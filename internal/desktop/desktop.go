// Package desktop is file-presenter's presence on the computer: a small native window that stays open
// while it runs (closing it quits), and opening the browser.
//
//	Run(url)   shows the window, opens url in the browser, and blocks until the window is closed.
//	Close()    takes the window down from any goroutine, making Run return.
//	Fatal(err) shows an error (there may be no console to print it to) and exits.
//
// Mac uses Cocoa (cgo), Windows calls Win32 directly; elsewhere there's no window and Run just waits
// for Close.
package desktop

import (
	"os/exec"
	"runtime"
)

// OpenBrowser opens url in the default browser.
func OpenBrowser(url string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", url).Start()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	default:
		return exec.Command("xdg-open", url).Start()
	}
}
