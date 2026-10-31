package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"html"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

//go:embed web
var webFS embed.FS

func newServer(lib *Library) http.Handler {
	mux := http.NewServeMux()
	page := func(name string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			b, _ := webFS.ReadFile("web/" + name)
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Cache-Control", "no-store")
			w.Write(b)
		}
	}
	mux.HandleFunc("GET /{$}", page("viewer.html"))
	mux.HandleFunc("GET /remote", page("remote.html"))

	mux.HandleFunc("GET /api/state", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, lib.State())
	})
	mux.HandleFunc("GET /api/list", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, lib.Listing(r.URL.Query().Get("dir")))
	})
	mux.HandleFunc("POST /api/go", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		switch {
		case q.Has("to"):
			to, err := strconv.Atoi(q.Get("to"))
			if err != nil {
				http.Error(w, "bad request", http.StatusBadRequest)
				return
			}
			lib.Go(to)
		case q.Has("unseen"):
			lib.NextUnseen()
		default:
			d, err := strconv.Atoi(q.Get("delta"))
			if err != nil {
				d = 1
			}
			lib.Delta(d)
		}
		writeJSON(w, lib.State())
	})
	mux.HandleFunc("POST /api/playpause", func(w http.ResponseWriter, r *http.Request) {
		lib.PlayPause()
		writeJSON(w, lib.State())
	})

	mux.HandleFunc("GET /thumb/{i}", func(w http.ResponseWriter, r *http.Request) {
		i, ok := fileIndex(w, r, lib)
		if !ok {
			return
		}
		if t, err := makeThumb(lib.files[i], lib.kinds[i]); err == nil {
			serveFile(w, r, t, "image/jpeg")
			return
		}
		ext := strings.ToUpper(strings.TrimPrefix(filepath.Ext(lib.files[i]), "."))
		if ext == "" {
			ext = "FILE"
		}
		w.Header().Set("Content-Type", "image/svg+xml")
		fmt.Fprintf(w, `<svg xmlns="http://www.w3.org/2000/svg" width="400" height="300"><rect width="100%%" `+
			`height="100%%" fill="#333"/><text x="50%%" y="55%%" font-size="64" fill="#aaa" `+
			`text-anchor="middle" font-family="sans-serif">%s</text></svg>`, html.EscapeString(ext))
	})

	mux.HandleFunc("GET /file/{i}", func(w http.ResponseWriter, r *http.Request) {
		i, ok := fileIndex(w, r, lib)
		if !ok {
			return
		}
		path := lib.files[i]
		var out, ctype string
		var err error
		switch lib.kinds[i] {
		case "image_conv":
			ctype = "image/jpeg"
			out, err = convertImage(path)
		case "doc":
			ctype = "text/html; charset=utf-8"
			out, err = convertDoc(path)
		case "video_conv":
			j := lib.videoJob(i)
			<-j.done
			out, err, ctype = j.out, j.err, "video/mp4"
		case "text":
			out, ctype = path, "text/plain; charset=utf-8"
		default:
			out = path
		}
		if err != nil {
			log.Printf("error %s: %v", r.URL.Path, err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		serveFile(w, r, out, ctype)
	})
	return mux
}

func fileIndex(w http.ResponseWriter, r *http.Request, lib *Library) (int, bool) {
	i, err := strconv.Atoi(r.PathValue("i"))
	if err != nil || i < 0 || i >= len(lib.files) {
		http.Error(w, "bad request", http.StatusBadRequest)
		return 0, false
	}
	return i, true
}

// serveFile sends a file with Range support (needed for video seeking).
func serveFile(w http.ResponseWriter, r *http.Request, path, ctype string) {
	f, err := os.Open(path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if ctype != "" {
		w.Header().Set("Content-Type", ctype)
	}
	w.Header().Set("Cache-Control", "max-age=3600")
	http.ServeContent(w, r, filepath.Base(path), st.ModTime(), f)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(v)
}
