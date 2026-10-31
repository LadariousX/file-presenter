package library

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
)

// skipDirs are system folders that are never presented (or offered in the folder picker).
var skipDirs = map[string]bool{"System Volume Information": true, "$RECYCLE.BIN": true, ".Trashes": true,
	".Spotlight-V100": true, ".fseventsd": true}
var skipFiles = map[string]bool{"thumbs.db": true, "desktop.ini": true, ".ds_store": true}

// kindExts decides how the viewer shows a file. The browser gets the file as is; if it can't open it
// (e.g. WMV in Chrome, HEIC outside Safari), the viewer says so.
var kindExts = map[string][]string{
	"image": {"jpg", "jpeg", "png", "gif", "bmp", "webp", "svg", "avif", "tif", "tiff", "heic"},
	"video": {"mp4", "m4v", "mov", "webm", "ogv", "wmv", "mts", "m2ts", "mpg", "mpeg", "mkv", "avi", "3gp", "flv", "vob"},
	"audio": {"mp3", "m4a", "wav", "aac", "ogg", "oga", "flac", "opus", "wma"},
	"pdf":   {"pdf"},
	"text":  {"txt", "csv", "md", "log", "json", "xml"},
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

// NaturalLess compares names so "8" < "8a" < "9" < "10", ignoring case.
func NaturalLess(a, b string) bool {
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
	sort.Slice(entries, func(i, j int) bool { return NaturalLess(entries[i].Name(), entries[j].Name()) })
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

// SkipDir reports system folders (recycle bin, Spotlight index, …) that shouldn't be shown.
func SkipDir(name string) bool {
	return skipDirs[name] || strings.EqualFold(name, "$RECYCLE.BIN")
}
