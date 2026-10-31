// file-presenter browses every file on a drive in one browser window, controlled from a phone.
//
//	go run . ["/Volumes/NO NAME"] [-port 8000] [-no-open] [-cache .cache]
//
// Viewer: http://localhost:PORT/        (leave open, fullscreen)
// Remote: http://<lan-ip>:PORT/remote   (open on your phone)
package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
)

var chromium = []string{
	"/Applications/Chromium.app/Contents/MacOS/Chromium",
	"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
}

// openViewer opens the viewer in Chromium with autoplay allowed; falls back to the default browser.
func openViewer(url string) {
	for _, exe := range chromium {
		if _, err := os.Stat(exe); err != nil {
			continue
		}
		profile, _ := filepath.Abs(filepath.Join(cacheDir, "chromium"))
		// Own profile so the autoplay flag applies even if Chromium is already running.
		exec.Command(exe, "--user-data-dir="+profile, "--autoplay-policy=no-user-gesture-required",
			"--no-first-run", "--no-default-browser-check", "--start-fullscreen", "--app="+url).Start()
		return
	}
	log.Print("Chromium not found; using default browser (videos may need a click to start)")
	exec.Command("open", url).Start()
}

func lanIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return "localhost"
	}
	defer conn.Close()
	return conn.LocalAddr().(*net.UDPAddr).IP.String()
}

func main() {
	port := flag.Int("port", 8000, "port to listen on")
	noOpen := flag.Bool("no-open", false, "don't open the viewer window")
	flag.StringVar(&cacheDir, "cache", ".cache", "cache directory for thumbnails, conversions and progress")
	flag.Parse()
	root := "/Volumes/NO NAME"
	if flag.NArg() > 0 {
		root = flag.Arg(0)
	}

	if _, err := exec.LookPath("ffmpeg"); err != nil {
		log.Print("warning: ffmpeg not found; WMV/MTS/etc. videos and video thumbnails won't work")
	}
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		log.Fatal(err)
	}
	log.Printf("scanning %s ...", root)
	lib := NewLibrary(root)
	if len(lib.files) == 0 {
		log.Fatal("no files found")
	}
	lib.Go(lib.index)
	log.Printf("%d files", len(lib.files))

	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", *port))
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Viewer: http://localhost:%d/\n", *port)
	fmt.Printf("Remote: http://%s:%d/remote\n", lanIP(), *port)
	if !*noOpen {
		openViewer(fmt.Sprintf("http://localhost:%d/", *port))
	}
	log.Fatal(http.Serve(ln, newServer(lib)))
}
