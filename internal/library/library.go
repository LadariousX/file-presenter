// Package library is a presented folder: its files in Explorer order, which one is showing, what's been
// seen (saved between runs), favorites, and thumbnails for the remote.
package library

import (
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type Library struct {
	root string

	mu                       sync.Mutex // guards everything below, including files/rels/dirs (flagging moves files)
	files, rels, dirs, kinds []string
	index                    int
	version                  int // bumps when the current file changes
	cmd                      int // bumps on play/pause from the remote
	seek                     int // running total of seconds skipped from the remote (viewer applies the change)
	seekTo                   int // bumps when the remote's timeline is dragged to seekAt
	seekAt                   float64
	playback                 playback // where the viewer's video is, for the remote's timeline
	autoplay                 bool
	autoTimer                *time.Timer
	seen                     map[int]bool
	skipDupes                bool
	sizes                    map[int]int64 // file sizes, read on demand for duplicate checks
	updates                  chan struct{} // closed and replaced whenever state changes
	progressFile             string
}

type playback struct {
	pos, dur float64
	paused   bool
}

type progress struct {
	Last      string   `json:"last"`
	Seen      []string `json:"seen"`
	SkipDupes bool     `json:"skipDupes"`
}

// progressPath keeps progress outside the presented folder, one file per root.
func progressPath(root string) string {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = os.TempDir()
	}
	dir = filepath.Join(dir, "file-presenter")
	os.MkdirAll(dir, 0o755)
	return filepath.Join(dir, fmt.Sprintf("progress-%x.json", sha1.Sum([]byte(root))))
}

func NewLibrary(root string) *Library {
	lib := &Library{root: root, files: scan(root), seen: map[int]bool{}, sizes: map[int]int64{},
		progressFile: progressPath(root), updates: make(chan struct{})}
	pos := map[string]int{}
	for i, f := range lib.files {
		rel, dir := lib.relDir(f)
		lib.rels = append(lib.rels, rel)
		lib.dirs = append(lib.dirs, dir)
		lib.kinds = append(lib.kinds, kindOf(f))
		pos[rel] = i
	}
	var saved progress
	if data, err := os.ReadFile(lib.progressFile); err == nil && json.Unmarshal(data, &saved) == nil {
		for _, r := range saved.Seen {
			if i, ok := pos[r]; ok {
				lib.seen[i] = true
			}
		}
		if i, ok := pos[saved.Last]; ok {
			lib.index = i
		}
		lib.skipDupes = saved.SkipDupes
	}
	return lib
}

// relDir returns path relative to the root, and its folder ("" = top level), always with "/" separators.
func (lib *Library) relDir(path string) (rel, dir string) {
	rel, _ = filepath.Rel(lib.root, path)
	rel = filepath.ToSlash(rel)
	dir = filepath.ToSlash(filepath.Dir(rel))
	if dir == "." {
		dir = ""
	}
	return rel, dir
}

// File returns the current path and kind of file i.
func (lib *Library) File(i int) (string, string) {
	lib.mu.Lock()
	defer lib.mu.Unlock()
	return lib.files[i], lib.kinds[i]
}

func (lib *Library) Len() int { return len(lib.kinds) }

// saveProgress must be called with mu held.
func (lib *Library) saveProgress() {
	seen := make([]int, 0, len(lib.seen))
	for i := range lib.seen {
		seen = append(seen, i)
	}
	sort.Ints(seen)
	data := progress{Last: lib.rels[lib.index], Seen: make([]string, len(seen)), SkipDupes: lib.skipDupes}
	for k, i := range seen {
		data.Seen[k] = lib.rels[i]
	}
	b, _ := json.Marshal(data)
	tmp := lib.progressFile + ".tmp"
	if os.WriteFile(tmp, b, 0o644) == nil {
		os.Rename(tmp, lib.progressFile)
	}
}

// Start shows the file the last session ended on (or the first one).
func (lib *Library) Start() {
	lib.Go(lib.index)
}

func (lib *Library) Go(to int) {
	lib.mu.Lock()
	lib.goLocked(to)
	lib.mu.Unlock()
}

func (lib *Library) goLocked(to int) {
	n := len(lib.files)
	lib.index = ((to % n) + n) % n
	lib.version++
	lib.playback = playback{}
	lib.seen[lib.index] = true
	lib.saveProgress()
	lib.scheduleLocked()
	lib.changed()
}

// Delta steps forward/back; with skipDupes on, files matching (same name and size) one already viewed
// elsewhere are passed over.
func (lib *Library) Delta(d int) {
	lib.mu.Lock()
	n := len(lib.files)
	target := lib.index + d
	if lib.skipDupes && d != 0 {
		step := 1
		if d < 0 {
			step = -1
		}
		seenKeys := map[dupeKey]int{}
		for i := range lib.seen {
			seenKeys[lib.dupeKeyLocked(i)]++
		}
		isDupe := func(j int) bool {
			c := seenKeys[lib.dupeKeyLocked(j)]
			if lib.seen[j] {
				c-- // its own view doesn't make it a duplicate
			}
			return c > 0
		}
		for tries := 0; tries < n && isDupe(((target%n)+n)%n); tries++ {
			target += step
		}
	}
	lib.goLocked(target)
	lib.mu.Unlock()
}

type dupeKey struct {
	name string
	size int64
}

// dupeKeyLocked identifies copies of a file: same name (ignoring case) and same size. Sizes are read
// once and cached. Call with mu held.
func (lib *Library) dupeKeyLocked(i int) dupeKey {
	size, ok := lib.sizes[i]
	if !ok {
		size = -1
		if st, err := os.Stat(lib.files[i]); err == nil {
			size = st.Size()
		}
		lib.sizes[i] = size
	}
	return dupeKey{strings.ToLower(filepath.Base(lib.files[i])), size}
}

// Seek skips the playing video by secs (negative = back).
func (lib *Library) Seek(secs int) {
	lib.mu.Lock()
	lib.seek += secs
	lib.changed()
	lib.mu.Unlock()
}

// SeekTo jumps the playing video to secs.
func (lib *Library) SeekTo(secs float64) {
	lib.mu.Lock()
	lib.seekTo++
	lib.seekAt = secs
	lib.changed()
	lib.mu.Unlock()
}

// SetPlayback records where the viewer's video is, for the remote's timeline. Reports for an older
// version (the viewer hasn't caught up with a file change yet) are ignored.
func (lib *Library) SetPlayback(version int, pos, dur float64, paused bool) {
	lib.mu.Lock()
	defer lib.mu.Unlock()
	p := playback{pos, dur, paused}
	if version != lib.version || p == lib.playback {
		return
	}
	lib.playback = p
	lib.mediaEndedLocked()
	lib.changed()
}

func (lib *Library) PlayPause() {
	lib.mu.Lock()
	lib.cmd++
	lib.changed()
	lib.mu.Unlock()
}

// changed wakes everyone waiting on Updates; call with mu held.
func (lib *Library) changed() {
	close(lib.updates)
	lib.updates = make(chan struct{})
}

// Notify wakes everyone waiting on Updates without changing anything (e.g. so remotes see a shutdown).
func (lib *Library) Notify() {
	lib.mu.Lock()
	lib.changed()
	lib.mu.Unlock()
}

// Updates returns a channel that is closed at the next state change.
func (lib *Library) Updates() <-chan struct{} {
	lib.mu.Lock()
	defer lib.mu.Unlock()
	return lib.updates
}

func (lib *Library) SetSkipDupes(on bool) {
	lib.mu.Lock()
	lib.skipDupes = on
	lib.saveProgress()
	lib.changed()
	lib.mu.Unlock()
}

func (lib *Library) ClearProgress() {
	lib.mu.Lock()
	lib.seen = map[int]bool{lib.index: true} // the open file is being viewed right now
	lib.saveProgress()
	lib.changed()
	lib.mu.Unlock()
}

type State struct {
	Index     int     `json:"index"`
	Total     int     `json:"total"`
	Version   int     `json:"version"`
	Cmd       int     `json:"cmd"`
	Seek      int     `json:"seek"`
	SeekTo    int     `json:"seekTo"`
	SeekAt    float64 `json:"seekAt"`
	Pos       float64 `json:"pos"` // playing video: seconds in
	Dur       float64 `json:"dur"` // and its length (0 until the viewer knows)
	Paused    bool    `json:"paused"`
	Name      string  `json:"name"`
	Folder    string  `json:"folder"`
	Kind      string  `json:"kind"`
	Seen      int     `json:"seen"`
	Size      int64   `json:"size"`
	Flagged   bool    `json:"flagged"`
	SkipDupes bool    `json:"skipDupes"`
	Autoplay  bool    `json:"autoplay"`
}

func (lib *Library) State() State {
	lib.mu.Lock()
	i := lib.index
	s := State{Index: i, Total: len(lib.files), Version: lib.version, Cmd: lib.cmd, Seek: lib.seek,
		SeekTo: lib.seekTo, SeekAt: lib.seekAt, Pos: lib.playback.pos, Dur: lib.playback.dur, Paused: lib.playback.paused,
		Name: filepath.Base(lib.files[i]), Folder: lib.dirs[i], Kind: lib.kinds[i], Seen: len(lib.seen),
		Flagged: lib.dirs[i] == favoritesDir, SkipDupes: lib.skipDupes, Autoplay: lib.autoplay}
	path := lib.files[i]
	lib.mu.Unlock()
	if st, err := os.Stat(path); err == nil {
		s.Size = st.Size()
	}
	return s
}

type DirEntry struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	First int    `json:"first"`
	Total int    `json:"total"`
	Seen  int    `json:"seen"`
}

type FileEntry struct {
	Index int    `json:"index"`
	Name  string `json:"name"`
	Seen  bool   `json:"seen"`
}

type Listing struct {
	Dir   string      `json:"dir"`
	Dirs  []*DirEntry `json:"dirs"`
	Files []FileEntry `json:"files"`
}

// Listing returns subfolders (with seen counts) and files directly inside folder d ("" = top level).
func (lib *Library) Listing(d string) Listing {
	lib.mu.Lock()
	defer lib.mu.Unlock()
	prefix := ""
	if d != "" {
		prefix = d + "/"
	}
	out := Listing{Dir: d, Dirs: []*DirEntry{}, Files: []FileEntry{}}
	byName := map[string]*DirEntry{}
	for i, fd := range lib.dirs {
		switch {
		case fd == d:
			out.Files = append(out.Files, FileEntry{Index: i, Name: filepath.Base(lib.files[i]), Seen: lib.seen[i]})
		case strings.HasPrefix(fd, prefix):
			name, _, _ := strings.Cut(fd[len(prefix):], "/")
			e, ok := byName[name]
			if !ok {
				e = &DirEntry{Name: name, Path: prefix + name, First: i}
				byName[name] = e
				out.Dirs = append(out.Dirs, e)
			}
			e.Total++
			if lib.seen[i] {
				e.Seen++
			}
		}
	}
	return out
}
