package desktop

import (
	"fmt"
	"os"
	"runtime"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	gdi32    = syscall.NewLazyDLL("gdi32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")

	adjustWindowRect    = user32.NewProc("AdjustWindowRect")
	createWindowEx      = user32.NewProc("CreateWindowExW")
	defWindowProc       = user32.NewProc("DefWindowProcW")
	destroyWindow       = user32.NewProc("DestroyWindow")
	dispatchMessage     = user32.NewProc("DispatchMessageW")
	getDC               = user32.NewProc("GetDC")
	getMessage          = user32.NewProc("GetMessageW")
	getSysColor         = user32.NewProc("GetSysColor")
	getSysColorBrush    = user32.NewProc("GetSysColorBrush")
	getSystemMetrics    = user32.NewProc("GetSystemMetrics")
	isDialogMessage     = user32.NewProc("IsDialogMessageW")
	loadCursor          = user32.NewProc("LoadCursorW")
	messageBox          = user32.NewProc("MessageBoxW")
	postMessage         = user32.NewProc("PostMessageW")
	postQuitMessage     = user32.NewProc("PostQuitMessage")
	privateExtractIcons = user32.NewProc("PrivateExtractIconsW")
	registerClassEx     = user32.NewProc("RegisterClassExW")
	releaseDC           = user32.NewProc("ReleaseDC")
	sendMessage         = user32.NewProc("SendMessageW")
	setCursor           = user32.NewProc("SetCursor")
	setForegroundWindow = user32.NewProc("SetForegroundWindow")
	setProcessDPIAware  = user32.NewProc("SetProcessDPIAware")
	showWindow          = user32.NewProc("ShowWindow")
	translateMessage    = user32.NewProc("TranslateMessage")
	createFont          = gdi32.NewProc("CreateFontW")
	getDeviceCaps       = gdi32.NewProc("GetDeviceCaps")
	setBkMode           = gdi32.NewProc("SetBkMode")
	setTextColor        = gdi32.NewProc("SetTextColor")
	getModuleHandle     = kernel32.NewProc("GetModuleHandleW")
)

const (
	wmDestroy          = 0x0002
	wmClose            = 0x0010
	wmSetCursor        = 0x0020
	wmSetFont          = 0x0030
	wmCommand          = 0x0111
	wmCtlColorStatic   = 0x0138
	wsOverlappedWindow = 0x00C00000 | 0x00080000 | 0x00020000 // caption, system menu, minimize box
	wsChild            = 0x40000000
	wsVisible          = 0x10000000
	ssIcon             = 0x0003
	ssNotify           = 0x0100
	stmSetIcon         = 0x0170
	colorWindow        = 5
	colorWindowText    = 8
	colorGrayText      = 17
	idcArrow           = 32512
	idcHand            = 32649
	logPixelsY         = 90
	linkID             = 1
)

type wndClassEx struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   uintptr
	Icon       uintptr
	Cursor     uintptr
	Background uintptr
	MenuName   *uint16
	ClassName  *uint16
	IconSm     uintptr
}

type msg struct {
	Hwnd     uintptr
	Message  uint32
	WParam   uintptr
	LParam   uintptr
	Time     uint32
	Pt       struct{ X, Y int32 }
	LPrivate uint32
}

type rect struct{ Left, Top, Right, Bottom int32 }

var (
	mu      sync.Mutex
	hwnd    uintptr // the window, once it exists
	closing bool    // Close was called (maybe before the window existed)
)

// str converts s for Win32. The result is kept referenced in strs so the GC never frees it while
// Windows still holds the address (e.g. the window class name).
func str(s string) uintptr {
	p, _ := syscall.UTF16PtrFromString(s)
	strs = append(strs, p)
	return uintptr(unsafe.Pointer(p))
}

var strs []*uint16

// exeIcon extracts our own icon (compiled in by build.sh) at size×size pixels; 0 if there's none.
func exeIcon(size int) uintptr {
	exe, err := os.Executable()
	if err != nil {
		return 0
	}
	var icon uintptr
	var id uint32
	n, _, _ := privateExtractIcons.Call(str(exe), 0, uintptr(size), uintptr(size),
		uintptr(unsafe.Pointer(&icon)), uintptr(unsafe.Pointer(&id)), 1, 0)
	if n != 1 {
		return 0
	}
	return icon
}

func Run(url string) {
	runtime.LockOSThread() // a window's messages arrive on the thread that made it
	defer runtime.UnlockOSThread()

	setProcessDPIAware.Call()
	dc, _, _ := getDC.Call(0)
	dpi, _, _ := getDeviceCaps.Call(dc, logPixelsY)
	releaseDC.Call(0, dc)
	px := func(n int) uintptr { return uintptr(n * int(dpi) / 96) } // layout is in 96-dpi pixels
	font := func(pt int, bold, underline bool) uintptr {
		weight := 400
		if bold {
			weight = 700
		}
		h := -(pt * int(dpi) / 72)
		f, _, _ := createFont.Call(uintptr(h), 0, 0, 0, uintptr(weight), 0, boolArg(underline), 0, 1, 0, 0,
			5 /* ClearType */, 0, str("Segoe UI"))
		return f
	}

	instance, _, _ := getModuleHandle.Call(0)
	cursor, _, _ := loadCursor.Call(0, idcArrow)
	hand, _, _ := loadCursor.Call(0, idcHand)
	bg, _, _ := getSysColorBrush.Call(colorWindow)
	var link, foot uintptr

	wndProc := syscall.NewCallback(func(w, m, wp, lp uintptr) uintptr {
		switch m {
		case wmCommand:
			if wp&0xFFFF == linkID {
				OpenBrowser(url)
			}
			return 0
		case wmSetCursor:
			if wp == link {
				setCursor.Call(hand)
				return 1
			}
		case wmCtlColorStatic: // colors for the labels: link blue, footnote gray
			color, _, _ := getSysColor.Call(colorWindowText)
			switch lp {
			case link:
				color = 0xCC6600 // RGB(0, 102, 204), stored as BGR
			case foot:
				color, _, _ = getSysColor.Call(colorGrayText)
			}
			setTextColor.Call(wp, color)
			setBkMode.Call(wp, 1 /* transparent */)
			return bg
		case wmDestroy:
			postQuitMessage.Call(0)
			return 0
		}
		r, _, _ := defWindowProc.Call(w, m, wp, lp)
		return r
	})

	className, _ := syscall.UTF16PtrFromString("FilePresenter")
	class := wndClassEx{WndProc: wndProc, Instance: instance, Cursor: cursor, Background: bg,
		ClassName: className, Icon: exeIcon(int(px(32))), IconSm: exeIcon(int(px(16)))}
	class.Size = uint32(unsafe.Sizeof(class))
	registerClassEx.Call(uintptr(unsafe.Pointer(&class)))
	runtime.KeepAlive(className)

	r := rect{0, 0, int32(px(470)), int32(px(150))}
	adjustWindowRect.Call(uintptr(unsafe.Pointer(&r)), wsOverlappedWindow, 0)
	ww, wh := uintptr(r.Right-r.Left), uintptr(r.Bottom-r.Top)
	sw, _, _ := getSystemMetrics.Call(0)
	sh, _, _ := getSystemMetrics.Call(1)
	win, _, _ := createWindowEx.Call(0, str("FilePresenter"), str("File Presenter"), wsOverlappedWindow,
		(sw-ww)/2, (sh-wh)/2, ww, wh, 0, 0, instance, 0)
	if win == 0 {
		OpenBrowser(url)
		waitForClose()
		return
	}

	child := func(class, text string, style uintptr, x, y, w, h int, id uintptr, f uintptr) uintptr {
		c, _, _ := createWindowEx.Call(0, str(class), str(text), wsChild|wsVisible|style,
			px(x), px(y), px(w), px(h), win, id, instance, 0)
		if f != 0 {
			sendMessage.Call(c, wmSetFont, f, 1)
		}
		return c
	}
	if icon := exeIcon(int(px(64))); icon != 0 {
		ic := child("STATIC", "", ssIcon, 20, 20, 64, 64, 0, 0)
		sendMessage.Call(ic, stmSetIcon, icon, 0)
	}
	child("STATIC", "File Presenter is running", 0, 100, 18, 350, 24, 0, font(12, true, false))
	child("STATIC", "A browser window should have opened. If it didn't, click:", 0, 100, 46, 350, 20, 0, font(9, false, false))
	link = child("STATIC", url, ssNotify, 100, 68, 350, 20, linkID, font(9, false, true))
	foot = child("STATIC", "Close this window to stop File Presenter.", 0, 100, 116, 350, 18, 0, font(8, false, false))

	mu.Lock()
	hwnd = win
	early := closing
	mu.Unlock()
	if early {
		destroyWindow.Call(win)
	} else {
		showWindow.Call(win, 1 /* SW_SHOWNORMAL */)
		setForegroundWindow.Call(win)
		go func() {
			time.Sleep(300 * time.Millisecond) // after the window is up, so the browser opens in front of it
			OpenBrowser(url)
		}()
	}

	var m msg
	for {
		r, _, _ := getMessage.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if r == 0 || int32(r) == -1 { // WM_QUIT or error
			break
		}
		if d, _, _ := isDialogMessage.Call(win, uintptr(unsafe.Pointer(&m))); d != 0 {
			continue
		}
		translateMessage.Call(uintptr(unsafe.Pointer(&m)))
		dispatchMessage.Call(uintptr(unsafe.Pointer(&m)))
	}
	mu.Lock()
	hwnd = 0
	mu.Unlock()
}

// Close closes the window (WM_CLOSE → destroy → Run returns).
func Close() {
	mu.Lock()
	defer mu.Unlock()
	closing = true
	if hwnd != 0 {
		postMessage.Call(hwnd, wmClose, 0, 0)
	}
	if waiting != nil {
		close(waiting)
		waiting = nil
	}
}

var waiting chan struct{}

// waitForClose is Run without a window (if creating it failed): wait for Close.
func waitForClose() {
	mu.Lock()
	if closing {
		mu.Unlock()
		return
	}
	ch := make(chan struct{})
	waiting = ch
	mu.Unlock()
	<-ch
}

func Fatal(err error) {
	messageBox.Call(0, str(err.Error()), str("File Presenter can't start"), 0x10 /* MB_ICONERROR */)
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}

func boolArg(b bool) uintptr {
	if b {
		return 1
	}
	return 0
}
