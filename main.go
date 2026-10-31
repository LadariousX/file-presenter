// file-presenter browses every file in a folder in one browser window, controlled from a phone.
//
//	go run . [folder] [-port 8000] [-no-open]
//
// With no folder, the browser opens a folder picker. A small window stays open while it runs;
// closing it stops the app.
// Viewer: http://localhost:PORT/              (opens in the default browser, closes itself on exit)
// Remote: http://<computer>.local:PORT/remote (open on your phone; the viewer shows a QR code)
//
// Code: internal/library (the folder being presented, conversions, thumbnails), internal/server
// (web pages, remote WebSocket), internal/desktop (the native window).
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"file-presenter/internal/desktop"
	"file-presenter/internal/library"
	"file-presenter/internal/server"
)

func main() {
	port := flag.Int("port", 8000, "port to listen on")
	noOpen := flag.Bool("no-open", false, "no window, and don't open the viewer in the browser")
	flag.Parse()
	viewer := fmt.Sprintf("http://localhost:%d/", *port)

	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", *port))
	if err != nil {
		if alreadyRunning(viewer) { // e.g. the app was opened twice: just show the one that's running
			if !*noOpen {
				desktop.OpenBrowser(viewer)
			}
			return
		}
		desktop.Fatal(fmt.Errorf("Port %d is in use by another program.\n\n%v", *port, err))
	}

	if !library.FileExists(library.Tool("ffmpeg")) {
		log.Print("warning: ffmpeg not found; some videos won't get thumbnails")
	}

	app := server.NewApp(*port)
	if root := flag.Arg(0); root != "" {
		if err := app.Open(root); err != nil {
			desktop.Fatal(err)
		}
	}

	srv := &http.Server{Handler: app.Handler()}
	go srv.Serve(ln)
	fmt.Printf("Viewer: %s\n", viewer)
	if url := server.RemoteURL(*port); url != "" {
		fmt.Printf("Remote: %s\n", url)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer stop()
	go func() {
		<-ctx.Done()
		desktop.Close()
	}()
	if *noOpen {
		<-ctx.Done()
	} else {
		desktop.Run(viewer) // until the window is closed, or Ctrl-C
	}

	log.Print("shutting down")
	// Tell the viewer to close its tab and remotes to hang up; give the viewer a moment to hear it.
	select {
	case <-app.Close():
	case <-time.After(2 * time.Second):
	}
	srv.Close()
}

// alreadyRunning reports whether file-presenter is already answering at viewer.
func alreadyRunning(viewer string) bool {
	c := http.Client{Timeout: time.Second}
	resp, err := c.Get(viewer + "api/state")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}
