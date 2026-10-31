// Package server is the web side of file-presenter: the viewer and folder picker for this computer,
// the phone remote (over a WebSocket), and the files themselves.
package server

import (
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"file-presenter/internal/library"
)

// remoteGrace is how long a remote may be gone before the viewer shows the setup screen again.
const remoteGrace = 20 * time.Second

// App holds what lives outside a single folder: the chosen library (nil until a folder is picked),
// connected remotes, and shutdown.
type App struct {
	port int
	lib  atomic.Pointer[library.Library]

	mu             sync.Mutex
	remotes        int       // open remote connections
	remoteGen      int       // bumps on every new remote connection
	lastDisconnect time.Time // when the last remote went away

	closing   atomic.Bool
	closedAck chan struct{}
	ackOnce   sync.Once
}

func NewApp(port int) *App {
	return &App{port: port, closedAck: make(chan struct{})}
}

// Open scans root and makes it the presented folder.
func (a *App) Open(root string) error {
	log.Printf("scanning %s ...", root)
	lib := library.NewLibrary(root)
	if lib.Len() == 0 {
		return fmt.Errorf("no files found in %s", root)
	}
	lib.Start()
	log.Printf("%d files", lib.Len())
	a.lib.Store(lib)
	return nil
}

func (a *App) RemoteConnected() {
	a.mu.Lock()
	a.remotes++
	a.remoteGen++
	a.mu.Unlock()
}

func (a *App) RemoteDisconnected() {
	a.mu.Lock()
	a.remotes--
	a.lastDisconnect = time.Now()
	a.mu.Unlock()
}

// RemoteActive: a remote is connected, or one dropped less than remoteGrace ago.
func (a *App) RemoteActive() (active bool, gen int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.remotes > 0 || (a.remoteGen > 0 && time.Since(a.lastDisconnect) < remoteGrace), a.remoteGen
}

// Close asks open pages to close; the returned channel closes once the viewer has been told.
func (a *App) Close() <-chan struct{} {
	a.closing.Store(true)
	if lib := a.lib.Load(); lib != nil {
		lib.Notify() // wake remote connections so they hang up
	}
	return a.closedAck
}

func (a *App) viewerToldClosing() {
	a.ackOnce.Do(func() { close(a.closedAck) })
}
