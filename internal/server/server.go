package server

import (
	"embed"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"file-presenter/internal/library"
	"github.com/skip2/go-qrcode"
)

//go:embed web
var webFS embed.FS

// viewState is what the viewer (and folder picker) polls: the library's state plus app-level bits.
type viewState struct {
	*library.State      // nil until a folder is chosen
	Remote         bool `json:"remote"`    // a remote is connected (or dropped < 20 s ago)
	RemoteGen      int  `json:"remoteGen"` // bumps on each new remote connection
	Closing        bool `json:"closing"`
}

// Handler serves the viewer, the remote and their APIs.
func (a *App) Handler() http.Handler {
	mux := http.NewServeMux()
	page := func(name string) []byte {
		b, _ := webFS.ReadFile("web/" + name)
		return b
	}
	sendPage := func(w http.ResponseWriter, b []byte) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Write(b)
	}
	// withLib wraps handlers that need a chosen folder.
	withLib := func(h func(http.ResponseWriter, *http.Request, *library.Library)) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			lib := a.lib.Load()
			if lib == nil {
				http.Error(w, "no folder chosen yet", http.StatusServiceUnavailable)
				return
			}
			h(w, r, lib)
		}
	}
	localOnly := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if !isLocal(r) {
				http.Error(w, "only available on this computer", http.StatusForbidden)
				return
			}
			h(w, r)
		}
	}

	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		switch {
		case a.lib.Load() != nil:
			sendPage(w, page("viewer.html"))
		case isLocal(r): // the viewer page shows the folder picker until a folder is chosen
			sendPage(w, page("viewer.html"))
		default: // a phone before a folder is chosen
			http.Redirect(w, r, "/remote", http.StatusFound)
		}
	})
	mux.HandleFunc("GET /remote", func(w http.ResponseWriter, r *http.Request) { sendPage(w, page("remote.html")) })
	mux.HandleFunc("GET /ws", a.serveRemote)
	mux.HandleFunc("GET /icon.svg", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/svg+xml")
		w.Write(page("icon.svg"))
	})

	mux.HandleFunc("GET /api/state", func(w http.ResponseWriter, r *http.Request) {
		var vs viewState
		if lib := a.lib.Load(); lib != nil {
			if q := r.URL.Query(); q.Has("pos") { // the viewer reports its video's position with each poll
				v, _ := strconv.Atoi(q.Get("v"))
				pos, _ := strconv.ParseFloat(q.Get("pos"), 64)
				dur, _ := strconv.ParseFloat(q.Get("dur"), 64)
				lib.SetPlayback(v, pos, dur, q.Get("paused") == "1")
			}
			s := lib.State()
			vs.State = &s
		}
		vs.Remote, vs.RemoteGen = a.RemoteActive()
		if vs.State != nil && vs.State.Autoplay {
			vs.Remote = true // the phone may sleep while autoplay runs; don't bring the setup screen down over it
		}
		vs.Closing = a.closing.Load()
		writeJSON(w, vs)
		if vs.Closing {
			a.viewerToldClosing() // this response carries the news
		}
	})

	// Folder picker (this computer only).
	mux.HandleFunc("GET /api/fs", localOnly(func(w http.ResponseWriter, r *http.Request) {
		l, err := listFolder(r.URL.Query().Get("path"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		writeJSON(w, l)
	}))
	mux.HandleFunc("POST /api/open", localOnly(func(w http.ResponseWriter, r *http.Request) {
		if a.lib.Load() != nil {
			http.Error(w, "a folder is already open", http.StatusConflict)
			return
		}
		if err := a.Open(r.URL.Query().Get("path")); err != nil {
			http.Error(w, err.Error(), http.StatusUnprocessableEntity)
			return
		}
		writeJSON(w, map[string]bool{"ok": true})
	}))

	// Connect screen: the remote's address on the local network ("" when there's no network) and its QR code.
	remoteURL := func() string { return RemoteURL(a.port) }
	mux.HandleFunc("GET /api/connect", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]string{"url": remoteURL()})
	})
	mux.HandleFunc("GET /qr.png", func(w http.ResponseWriter, r *http.Request) {
		url := remoteURL()
		if url == "" {
			http.Error(w, "no network", http.StatusNotFound)
			return
		}
		png, err := qrcode.Encode(url, qrcode.Medium, 512)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "no-store")
		w.Write(png)
	})

	mux.HandleFunc("GET /api/list", withLib(func(w http.ResponseWriter, r *http.Request, lib *library.Library) {
		writeJSON(w, lib.Listing(r.URL.Query().Get("dir")))
	}))
	// ← / → on the computer's keyboard; the phone sends its commands over the WebSocket.
	mux.HandleFunc("POST /api/go", withLib(func(w http.ResponseWriter, r *http.Request, lib *library.Library) {
		d, err := strconv.Atoi(r.URL.Query().Get("delta"))
		if err != nil {
			d = 1
		}
		lib.Delta(d)
		writeJSON(w, lib.State())
	}))

	mux.HandleFunc("GET /thumb/{i}", withLib(func(w http.ResponseWriter, r *http.Request, lib *library.Library) {
		i, ok := fileIndex(w, r, lib)
		if !ok {
			return
		}
		path, kind := lib.File(i)
		if b, err := library.Thumb(path, kind); err == nil {
			w.Header().Set("Content-Type", "image/jpeg")
			w.Header().Set("Cache-Control", "max-age=600")
			w.Write(b)
			return
		}
		ext := strings.ToUpper(strings.TrimPrefix(filepath.Ext(path), "."))
		if ext == "" {
			ext = "FILE"
		}
		w.Header().Set("Content-Type", "image/svg+xml")
		fmt.Fprintf(w, `<svg xmlns="http://www.w3.org/2000/svg" width="400" height="300"><rect width="100%%" `+
			`height="100%%" fill="#333"/><text x="50%%" y="55%%" font-size="64" fill="#aaa" `+
			`text-anchor="middle" font-family="sans-serif">%s</text></svg>`, html.EscapeString(ext))
	}))

	mux.HandleFunc("GET /file/{i}", withLib(func(w http.ResponseWriter, r *http.Request, lib *library.Library) {
		i, ok := fileIndex(w, r, lib)
		if !ok {
			return
		}
		// Sent as is; if the browser can't open it, the viewer shows a "can't open" message.
		path, kind := lib.File(i)
		ctype := ""
		if kind == "text" {
			ctype = "text/plain; charset=utf-8"
		}
		serveFile(w, r, path, ctype)
	}))
	return mux
}

// RemoteURL is the phone's address for the remote: by name (Laydens-MacBook-Pro.local) when that
// works on this network, else by IP; "" when there's no network.
func RemoteURL(port int) string {
	ip := lanIP()
	if ip == "" {
		return ""
	}
	host := ip
	if name := localName(ip); name != "" {
		host = name
	}
	return fmt.Sprintf("http://%s:%d/remote", host, port)
}

func fileIndex(w http.ResponseWriter, r *http.Request, lib *library.Library) (int, bool) {
	i, err := strconv.Atoi(r.PathValue("i"))
	if err != nil || i < 0 || i >= lib.Len() {
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
