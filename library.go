package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"unicode"
)

var skipDirs = map[string]bool{"System Volume Information": true, "$RECYCLE.BIN": true, ".Trashes": true,
	".Spotlight-V100": true, ".fseventsd": true}
var skipFiles = map[string]bool{"thumbs.db": true, "desktop.ini": true, ".ds_store": true}

var kindExts = map[string][]string{
	"image":      {"jpg", "jpeg", "png", "gif", "bmp", "webp", "svg"},
	"image_conv": {"tif", "tiff", "heic"},
	"video":      {"mp4", "m4v", "mov", "webm"},
	"video_conv": {"wmv", "mts", "m2ts", "mpg", "mpeg", "mkv", "avi", "3gp", "flv", "vob"},
	"audio":      {"mp3", "m4a", "wav", "aac", "ogg", "flac"},
	"pdf":        {"pdf"},
	"text":       {"txt", "csv", "md", "log", "json", "xml"},
	"doc":        {"docx", "doc", "rtf", "odt"},
}

var extKind = func() map[string]string {
	m := map[string]string{}
	for k, exts := range kindExts {
		for _, e := range exts {
			m[e] = k
		}
	}
	return m
}()

func kindOf(path string) string {
	if k, ok := extKind[strings.ToLower(strings.TrimPrefix(filepath.Ext(path), "."))]; ok {
		return k
	}
	return "other"
}

// naturalLess compares names so "8" < "8a" < "9" < "10", ignoring case.
func naturalLess(a, b string) bool {
	ca, cb := chunks(a), chunks(b)
	for i := 0; i < len(ca) && i < len(cb); i++ {
		x, y := ca[i], cb[i]
		if i%2 == 1 { // numeric chunk
			x, y = strings.TrimLeft(x, "0"), strings.TrimLeft(y, "0")
			if len(x) != len(y) {
				return len(x) < len(y)
			}
		} else {
			x, y = strings.ToLower(x), strings.ToLower(y)
		}
		if x != y {
			return x < y
		}
	}
	return len(ca) < len(cb)
}

// chunks splits s into alternating text/number pieces, always starting with text.
func chunks(s string) []string {
	out := []string{""}
	for _, r := range s {
		isNum := unicode.IsDigit(r)
		if isNum != (len(out)%2 == 0) {
			out = append(out, "")
		}
		out[len(out)-1] += string(r)
	}
	return out
}

// scan lists all files in Windows Explorer order: subfolders first, then loose files.
func scan(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	sort.Slice(entries, func(i, j int) bool { return naturalLess(entries[i].Name(), entries[j].Name()) })
	var files []string
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") && !skipDirs[e.Name()] {
			files = append(files, scan(filepath.Join(dir, e.Name()))...)
		}
	}
	for _, e := range entries {
		n := strings.ToLower(e.Name())
		if e.Type().IsRegular() && !strings.HasPrefix(n, ".") && !skipFiles[n] && !strings.HasSuffix(n, ".modd") {
			files = append(files, filepath.Join(dir, e.Name()))
		}
	}
	return files
}

type videoJob struct {
	done chan struct{}
	out  string
	err  error
}

type Library struct {
	files, rels, dirs, kinds []string

	mu           sync.Mutex
	index        int
	version      int // bumps when the current file changes
	cmd          int // bumps on play/pause from the remote
	seen         map[int]bool
	progressFile string
	jobs         map[int]*videoJob
	ffmpegSlot   chan struct{} // one ffmpeg conversion at a time
}

func NewLibrary(root string) *Library {
	lib := &Library{files: scan(root), seen: map[int]bool{}, jobs: map[int]*videoJob{},
		progressFile: filepath.Join(cacheDir, "progress.json"), ffmpegSlot: make(chan struct{}, 1)}
	pos := map[string]int{}
	for i, f := range lib.files {
		rel, _ := filepath.Rel(root, f)
		dir := filepath.Dir(rel)
		if dir == "." {
			dir = "" // top level
		}
		lib.rels = append(lib.rels, rel)
		lib.dirs = append(lib.dirs, dir)
		lib.kinds = append(lib.kinds, kindOf(f))
		pos[rel] = i
	}
	var saved struct {
		Last string   `json:"last"`
		Seen []string `json:"seen"`
	}
	if data, err := os.ReadFile(lib.progressFile); err == nil && json.Unmarshal(data, &saved) == nil {
		for _, r := range saved.Seen {
			if i, ok := pos[r]; ok {
				lib.seen[i] = true
			}
		}
		if i, ok := pos[saved.Last]; ok {
			lib.index = i
		}
	}
	return lib
}

// saveProgress must be called with mu held.
func (lib *Library) saveProgress() {
	seen := make([]int, 0, len(lib.seen))
	for i := range lib.seen {
		seen = append(seen, i)
	}
	sort.Ints(seen)
	data := struct {
		Last string   `json:"last"`
		Seen []string `json:"seen"`
	}{Last: lib.rels[lib.index], Seen: make([]string, len(seen))}
	for k, i := range seen {
		data.Seen[k] = lib.rels[i]
	}
	b, _ := json.Marshal(data)
	tmp := lib.progressFile + ".tmp"
	if os.WriteFile(tmp, b, 0o644) == nil {
		os.Rename(tmp, lib.progressFile)
	}
}

// videoJob starts (once) the MP4 conversion for a file browsers can't play; nil for other kinds.
func (lib *Library) videoJob(i int) *videoJob {
	if lib.kinds[i] != "video_conv" {
		return nil
	}
	lib.mu.Lock()
	defer lib.mu.Unlock()
	if j, ok := lib.jobs[i]; ok {
		return j
	}
	j := &videoJob{done: make(chan struct{})}
	lib.jobs[i] = j
	go func() {
		lib.ffmpegSlot <- struct{}{}
		j.out, j.err = convertVideo(lib.files[i])
		<-lib.ffmpegSlot
		close(j.done)
	}()
	return j
}

func (lib *Library) Go(to int) {
	n := len(lib.files)
	lib.mu.Lock()
	lib.index = ((to % n) + n) % n
	lib.version++
	i := lib.index
	lib.seen[i] = true
	lib.saveProgress()
	lib.mu.Unlock()
	lib.videoJob(i)
	lib.videoJob((i + 1) % n) // prefetch next
}

func (lib *Library) Delta(d int) {
	lib.mu.Lock()
	i := lib.index
	lib.mu.Unlock()
	lib.Go(i + d)
}

func (lib *Library) NextUnseen() {
	lib.mu.Lock()
	n, cur, next := len(lib.files), lib.index, lib.index+1
	for k := 1; k <= n; k++ {
		if !lib.seen[(cur+k)%n] {
			next = (cur + k) % n
			break
		}
	}
	lib.mu.Unlock()
	lib.Go(next)
}

func (lib *Library) PlayPause() {
	lib.mu.Lock()
	lib.cmd++
	lib.mu.Unlock()
}

type State struct {
	Index   int     `json:"index"`
	Total   int     `json:"total"`
	Version int     `json:"version"`
	Cmd     int     `json:"cmd"`
	Name    string  `json:"name"`
	Folder  string  `json:"folder"`
	Kind    string  `json:"kind"`
	Seen    int     `json:"seen"`
	Size    int64   `json:"size"`
	Ready   bool    `json:"ready"`
	Error   *string `json:"error"`
}

func (lib *Library) State() State {
	lib.mu.Lock()
	i := lib.index
	s := State{Index: i, Total: len(lib.files), Version: lib.version, Cmd: lib.cmd,
		Name: filepath.Base(lib.files[i]), Folder: lib.dirs[i], Kind: lib.kinds[i], Seen: len(lib.seen), Ready: true}
	lib.mu.Unlock()
	if st, err := os.Stat(lib.files[i]); err == nil {
		s.Size = st.Size()
	}
	if j := lib.videoJob(i); j != nil {
		select {
		case <-j.done:
			if j.err != nil {
				msg := j.err.Error()
				s.Error = &msg
			}
		default:
			s.Ready = false
		}
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
