//go:build darwin && cgo

package desktop

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Cocoa
#include <stdlib.h>
#include "window_darwin.h"
*/
import "C"

import (
	_ "embed"
	"fmt"
	"os"
	"runtime"
	"time"
	"unsafe"
)

//go:embed icon.png
var iconPNG []byte // Dock icon when run with `go run`; the .app uses its own icon

// Cocoa only works on the main thread, and main() runs on it as long as it's locked there from init.
func init() { runtime.LockOSThread() }

// Run must be called from main().
func Run(url string) {
	go func() {
		time.Sleep(300 * time.Millisecond) // after the window is up, so the browser opens in front of it
		OpenBrowser(url)
	}()
	curl := C.CString(url)
	defer C.free(unsafe.Pointer(curl))
	C.fpRun(curl, unsafe.Pointer(&iconPNG[0]), C.int(len(iconPNG)))
}

func Close() { C.fpClose() }

// Fatal must be called from main().
func Fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	msg := C.CString(err.Error())
	defer C.free(unsafe.Pointer(msg))
	C.fpAlert(msg)
	os.Exit(1)
}
