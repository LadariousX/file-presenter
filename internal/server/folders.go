package server

import (
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"file-presenter/internal/library"
)

// Folder browsing for the in-browser folder picker. Only answered for requests from this computer,
// so a phone on the network can't look around the disk.

type fsEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

type fsListing struct {
	Path   string    `json:"path"`   // "" = the drives view
	Parent string    `json:"parent"` // "" = up goes to the drives view
	Dirs   []fsEntry `json:"dirs"`
	Files  int       `json:"files"` // files directly inside, for "12 files here"
	Roots  []fsEntry `json:"roots"`
}

func isLocal(r *http.Request) bool {
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// roots lists drives (USB sticks, cards, disks) plus the home folder.
func roots() []fsEntry {
	var out []fsEntry
	addChildren := func(dir string) {
		entries, _ := os.ReadDir(dir)
		for _, e := range entries {
			if !strings.HasPrefix(e.Name(), ".") {
				out = append(out, fsEntry{e.Name(), filepath.Join(dir, e.Name())})
			}
		}
	}
	switch runtime.GOOS {
	case "darwin":
		addChildren("/Volumes")
	case "windows":
		for c := 'A'; c <= 'Z'; c++ {
			if p := string(c) + `:\`; library.FileExists(p) {
				out = append(out, fsEntry{string(c) + ":", p})
			}
		}
	default:
		user := os.Getenv("USER")
		addChildren(filepath.Join("/media", user))
		addChildren(filepath.Join("/run/media", user))
		addChildren("/mnt")
	}
	if home, err := os.UserHomeDir(); err == nil {
		out = append(out, fsEntry{"Home", home})
	}
	return out
}

func listFolder(path string) (fsListing, error) {
	l := fsListing{Path: path, Dirs: []fsEntry{}, Roots: roots()}
	if path == "" {
		return l, nil
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return l, err
	}
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") || library.SkipDir(name) {
			continue
		}
		if e.IsDir() {
			l.Dirs = append(l.Dirs, fsEntry{name, filepath.Join(path, name)})
		} else {
			l.Files++
		}
	}
	sort.Slice(l.Dirs, func(i, j int) bool { return library.NaturalLess(l.Dirs[i].Name, l.Dirs[j].Name) })
	if parent := filepath.Dir(path); parent != path {
		l.Parent = parent
	}
	return l, nil
}
