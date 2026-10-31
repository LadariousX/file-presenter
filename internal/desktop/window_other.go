//go:build !windows && !(darwin && cgo)

package desktop

import (
	"log"
	"sync"
)

var (
	closed    = make(chan struct{})
	closeOnce sync.Once
)

// Run opens url in the browser and waits for Close; there's no window on this platform.
func Run(url string) {
	OpenBrowser(url)
	<-closed
}

func Close() { closeOnce.Do(func() { close(closed) }) }

func Fatal(err error) { log.Fatal(err) }
