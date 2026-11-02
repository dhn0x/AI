//go:build windows

package main

import (
	"errors"
	"image"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Win32 plumbing: find windows, capture them, send keys and clicks, the
// Ctrl+Alt+Q kill switch and the on-screen banner. Coordinates are physical
// pixels (the process is per-monitor DPI aware).

var (
	user32 = windows.NewLazySystemDLL("user32.dll")
	gdi32  = windows.NewLazySystemDLL("gdi32.dll")

	pEnumWindows                   = user32.NewProc("EnumWindows")
	pGetWindowTextW                = user32.NewProc("GetWindowTextW")
	pIsWindowVisible               = user32.NewProc("IsWindowVisible")
	pIsWindow                      = user32.NewProc("IsWindow")
	pIsIconic                      = user32.NewProc("IsIconic")
	pGetClientRect                 = user32.NewProc("GetClientRect")
	pClientToScreen                = user32.NewProc("ClientToScreen")
	pGetWindowThreadProcessId      = user32.NewProc("GetWindowThreadProcessId")
	pGetForegroundWindow           = user32.NewProc("GetForegroundWindow")
	pSetForegroundWindow           = user32.NewProc("SetForegroundWindow")
	pShowWindow                    = user32.NewProc("ShowWindow")
	pGetDC                         = user32.NewProc("GetDC")
	pReleaseDC                     = user32.NewProc("ReleaseDC")
	pPrintWindow                   = user32.NewProc("PrintWindow")
	pSendInput                     = user32.NewProc("SendInput")
	pSetCursorPos                  = user32.NewProc("SetCursorPos")
	pGetCursorPos                  = user32.NewProc("GetCursorPos")
	pVkKeyScanW                    = user32.NewProc("VkKeyScanW")
	pMapVirtualKeyW                = user32.NewProc("MapVirtualKeyW")
	pRegisterHotKey                = user32.NewProc("RegisterHotKey")
	pGetMessageW                   = user32.NewProc("GetMessageW")
	pPostThreadMessageW            = user32.NewProc("PostThreadMessageW")
	pCreateWindowExW               = user32.NewProc("CreateWindowExW")
	pSetWindowTextW                = user32.NewProc("SetWindowTextW")
	pSetWindowPos                  = user32.NewProc("SetWindowPos")
	pGetSystemMetrics              = user32.NewProc("GetSystemMetrics")
	pMessageBoxW                   = user32.NewProc("MessageBoxW")
	pSetProcessDpiAwarenessContext = user32.NewProc("SetProcessDpiAwarenessContext")
	pGetCurrentThreadId            = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetCurrentThreadId")

	pCreateCompatibleDC     = gdi32.NewProc("CreateCompatibleDC")
	pCreateCompatibleBitmap = gdi32.NewProc("CreateCompatibleBitmap")
	pSelectObject           = gdi32.NewProc("SelectObject")
	pBitBlt                 = gdi32.NewProc("BitBlt")
	pGetDIBits              = gdi32.NewProc("GetDIBits")
	pDeleteObject           = gdi32.NewProc("DeleteObject")
	pDeleteDC               = gdi32.NewProc("DeleteDC")
)

type rect struct{ Left, Top, Right, Bottom int32 }
type point struct{ X, Y int32 }

func init() {
	// Per-monitor DPI aware v2, so window sizes and the cursor use real pixels.
	if pSetProcessDpiAwarenessContext.Find() == nil {
		pSetProcessDpiAwarenessContext.Call(^uintptr(3)) // DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2 (-4)
	}
}

type winInfo struct {
	HWND    uintptr `json:"hwnd"`
	Title   string  `json:"title"`
	Process string  `json:"process"`
	Width   int     `json:"width"`
	Height  int     `json:"height"`
}

func windowTitle(h uintptr) string {
	buf := make([]uint16, 512)
	n, _, _ := pGetWindowTextW.Call(h, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return windows.UTF16ToString(buf[:n])
}

func processName(h uintptr) string {
	var pid uint32
	pGetWindowThreadProcessId.Call(h, uintptr(unsafe.Pointer(&pid)))
	ph, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(ph)
	buf := make([]uint16, 1024)
	n := uint32(len(buf))
	if windows.QueryFullProcessImageName(ph, 0, &buf[0], &n) != nil {
		return ""
	}
	p := windows.UTF16ToString(buf[:n])
	return p[strings.LastIndexAny(p, `\/`)+1:]
}

func clientSize(h uintptr) (int, int) {
	var r rect
	pGetClientRect.Call(h, uintptr(unsafe.Pointer(&r)))
	return int(r.Right - r.Left), int(r.Bottom - r.Top)
}

// listWindows returns visible, titled top-level windows of a useful size.
func listWindows() []winInfo {
	var out []winInfo
	cb := syscall.NewCallback(func(h, _ uintptr) uintptr {
		if v, _, _ := pIsWindowVisible.Call(h); v == 0 {
			return 1
		}
		t := windowTitle(h)
		w, ht := clientSize(h)
		if t == "" || w < 200 || ht < 150 {
			return 1
		}
		out = append(out, winInfo{HWND: h, Title: t, Process: processName(h), Width: w, Height: ht})
		return 1
	})
	pEnumWindows.Call(cb, 0)
	return out
}

func isWindow(h uintptr) bool { r, _, _ := pIsWindow.Call(h); return r != 0 }

func clientOrigin(h uintptr) point {
	var p point
	pClientToScreen.Call(h, uintptr(unsafe.Pointer(&p)))
	return p
}

// capture grabs the window's client area. PrintWindow(PW_RENDERFULLCONTENT)
// works even when other windows cover it; if that comes back blank (some apps)
// it falls back to copying the area from the screen (must then be visible).
func capture(h uintptr) (*image.RGBA, error) {
	if !isWindow(h) {
		return nil, errors.New("the attached window is gone: call attach again")
	}
	if ic, _, _ := pIsIconic.Call(h); ic != 0 {
		pShowWindow.Call(h, 9) // SW_RESTORE
		time.Sleep(300 * time.Millisecond)
	}
	w, ht := clientSize(h)
	if w <= 0 || ht <= 0 {
		return nil, errors.New("window has no visible area")
	}
	img, err := grab(h, w, ht, true)
	if err == nil && !blank(img) {
		return img, nil
	}
	return grab(h, w, ht, false)
}

func grab(h uintptr, w, ht int, print bool) (*image.RGBA, error) {
	screen, _, _ := pGetDC.Call(0)
	defer pReleaseDC.Call(0, screen)
	mem, _, _ := pCreateCompatibleDC.Call(screen)
	defer pDeleteDC.Call(mem)
	bmp, _, _ := pCreateCompatibleBitmap.Call(screen, uintptr(w), uintptr(ht))
	if bmp == 0 {
		return nil, errors.New("CreateCompatibleBitmap failed")
	}
	defer pDeleteObject.Call(bmp)
	old, _, _ := pSelectObject.Call(mem, bmp)
	if print {
		if r, _, _ := pPrintWindow.Call(h, mem, 3); r == 0 { // PW_CLIENTONLY | PW_RENDERFULLCONTENT
			pSelectObject.Call(mem, old)
			return nil, errors.New("PrintWindow failed")
		}
	} else {
		o := clientOrigin(h)
		pBitBlt.Call(mem, 0, 0, uintptr(w), uintptr(ht), screen, uintptr(o.X), uintptr(o.Y), 0x00CC0020|0x40000000) // SRCCOPY|CAPTUREBLT
	}
	pSelectObject.Call(mem, old)
	type bih struct {
		Size          uint32
		Width, Height int32
		Planes, Bits  uint16
		Compression   uint32
		SizeImage     uint32
		XPPM, YPPM    int32
		ClrUsed, Imp  uint32
	}
	hdr := bih{Width: int32(w), Height: -int32(ht), Planes: 1, Bits: 32}
	hdr.Size = uint32(unsafe.Sizeof(hdr))
	buf := make([]byte, w*ht*4)
	if r, _, _ := pGetDIBits.Call(mem, bmp, 0, uintptr(ht), uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&hdr)), 0); r == 0 {
		return nil, errors.New("GetDIBits failed")
	}
	img := image.NewRGBA(image.Rect(0, 0, w, ht))
	for i := 0; i < len(buf); i += 4 { // BGRA → RGBA
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = buf[i+2], buf[i+1], buf[i], 255
	}
	return img, nil
}

func blank(img *image.RGBA) bool {
	p := img.Pix
	first := [3]byte{p[0], p[1], p[2]}
	for i := 0; i < len(p); i += 4 * 97 {
		if p[i] != first[0] || p[i+1] != first[1] || p[i+2] != first[2] {
			return false
		}
	}
	return true
}

// ---- input ----

type input struct {
	typ  uint32
	_    uint32
	data [32]byte
}

type keyIn struct {
	vk, scan    uint16
	flags, time uint32
	extra       uintptr
}

type mouseIn struct {
	dx, dy            int32
	data, flags, time uint32
	extra             uintptr
}

const (
	keyUp      = 0x0002
	keyUnicode = 0x0004
	keyExt     = 0x0001
)

func sendKey(vk, scan uint16, flags uint32) {
	var in input
	in.typ = 1
	*(*keyIn)(unsafe.Pointer(&in.data[0])) = keyIn{vk: vk, scan: scan, flags: flags}
	pSendInput.Call(1, uintptr(unsafe.Pointer(&in)), unsafe.Sizeof(in))
}

func sendMouse(flags, data uint32) {
	var in input
	in.typ = 0
	*(*mouseIn)(unsafe.Pointer(&in.data[0])) = mouseIn{flags: flags, data: data}
	pSendInput.Call(1, uintptr(unsafe.Pointer(&in)), unsafe.Sizeof(in))
}

var extendedVK = map[uint16]bool{0x21: true, 0x22: true, 0x23: true, 0x24: true, 0x25: true, 0x26: true, 0x27: true, 0x28: true, 0x2D: true, 0x2E: true}

func vkDown(vk uint16) {
	sc, _, _ := pMapVirtualKeyW.Call(uintptr(vk), 0)
	f := uint32(0)
	if extendedVK[vk] {
		f |= keyExt
	}
	sendKey(vk, uint16(sc), f)
}

func vkUp(vk uint16) {
	sc, _, _ := pMapVirtualKeyW.Call(uintptr(vk), 0)
	f := uint32(keyUp)
	if extendedVK[vk] {
		f |= keyExt
	}
	sendKey(vk, uint16(sc), f)
}

// typeRune sends one character as real key presses (with Shift/Ctrl/Alt as the
// keyboard layout needs), which remote-control viewers such as Teams forward
// reliably; characters with no key fall back to a Unicode key event.
func typeRune(r rune) {
	if r == '\n' {
		tap(0x0D)
		return
	}
	if r == '\t' {
		tap(0x09)
		return
	}
	if r < 0x10000 {
		res, _, _ := pVkKeyScanW.Call(uintptr(r))
		if int16(res) != -1 {
			vk, mods := uint16(res&0xFF), res>>8&0xFF
			mk := []uint16{}
			if mods&1 != 0 {
				mk = append(mk, 0x10)
			}
			if mods&2 != 0 {
				mk = append(mk, 0x11)
			}
			if mods&4 != 0 {
				mk = append(mk, 0x12)
			}
			for _, m := range mk {
				vkDown(m)
			}
			tap(vk)
			for i := len(mk) - 1; i >= 0; i-- {
				vkUp(mk[i])
			}
			return
		}
	}
	for _, u := range windows.StringToUTF16(string(r)) {
		if u == 0 {
			continue
		}
		sendKey(0, u, keyUnicode)
		time.Sleep(keyHold)
		sendKey(0, u, keyUnicode|keyUp)
	}
}

// keyHold: how long each key is held down. Remote-control viewers (Teams)
// sample the keyboard and drop very short taps.
var keyHold = 20 * time.Millisecond

func tap(vk uint16) {
	vkDown(vk)
	time.Sleep(keyHold)
	vkUp(vk)
}

func foreground() uintptr { h, _, _ := pGetForegroundWindow.Call(); return h }

// focus brings the window to the front. Windows only lets the foreground app
// do that, so a lone Alt tap first unlocks SetForegroundWindow.
func focus(h uintptr) bool {
	if foreground() == h {
		return true
	}
	vkDown(0x12)
	vkUp(0x12)
	pSetForegroundWindow.Call(h)
	for i := 0; i < 20 && foreground() != h; i++ {
		time.Sleep(25 * time.Millisecond)
	}
	return foreground() == h
}

func cursorPos() point { var p point; pGetCursorPos.Call(uintptr(unsafe.Pointer(&p))); return p }

func clickAt(x, y int, right, double bool) {
	pSetCursorPos.Call(uintptr(x), uintptr(y))
	time.Sleep(40 * time.Millisecond)
	down, up := uint32(0x0002), uint32(0x0004)
	if right {
		down, up = 0x0008, 0x0010
	}
	n := 1
	if double {
		n = 2
	}
	for i := 0; i < n; i++ {
		sendMouse(down, 0)
		time.Sleep(30 * time.Millisecond)
		sendMouse(up, 0)
		time.Sleep(60 * time.Millisecond)
	}
}

func wheelAt(x, y, clicks int) {
	pSetCursorPos.Call(uintptr(x), uintptr(y))
	time.Sleep(30 * time.Millisecond)
	sendMouse(0x0800, uint32(int32(clicks*120))) // MOUSEEVENTF_WHEEL; + up, - down
}

// confirm asks the engineer on this PC (top-most Yes/No box).
func confirm(title, text string) bool {
	t, _ := windows.UTF16PtrFromString(title)
	m, _ := windows.UTF16PtrFromString(text)
	r, _, _ := pMessageBoxW.Call(0, uintptr(unsafe.Pointer(m)), uintptr(unsafe.Pointer(t)), 0x4|0x30|0x40000|0x1000|0x100) // YESNO|ICONWARNING|TOPMOST|SYSTEMMODAL|DEFBUTTON2
	return r == 6
}

// ---- kill switch + banner (one UI thread) ----

type ui struct {
	tid    uint32
	banner uintptr
	texts  chan string
}

const wmUser = 0x0400

// startUI registers Ctrl+Alt+Q and shows a small always-on-top banner. Each
// press of Ctrl+Alt+Q toggles stop (the AI cannot un-stop itself).
func startUI(onToggle func() bool) *ui {
	u := &ui{texts: make(chan string, 8)}
	ready := make(chan struct{})
	go func() {
		runtime.LockOSThread()
		tid, _, _ := pGetCurrentThreadId.Call()
		u.tid = uint32(tid)
		pRegisterHotKey.Call(0, 1, 0x0001|0x0002|0x4000, 'Q') // MOD_ALT|MOD_CONTROL|MOD_NOREPEAT
		cls, _ := windows.UTF16PtrFromString("STATIC")
		txt, _ := windows.UTF16PtrFromString("")
		sw, _, _ := pGetSystemMetrics.Call(0)
		sh, _, _ := pGetSystemMetrics.Call(1)
		bw, bh := uintptr(560), uintptr(30)
		// WS_EX_TOPMOST|TOOLWINDOW|NOACTIVATE|TRANSPARENT|LAYERED-free; WS_POPUP|SS_CENTER|SS_CENTERIMAGE
		u.banner, _, _ = pCreateWindowExW.Call(0x8|0x80|0x08000000|0x20, uintptr(unsafe.Pointer(cls)), uintptr(unsafe.Pointer(txt)),
			0x80000000|0x1|0x200|0x00800000, (sw-bw)/2, sh-bh-60, bw, bh, 0, 0, 0, 0)
		close(ready)
		type msg struct {
			hwnd           uintptr
			message        uint32
			wParam, lParam uintptr
			time           uint32
			pt             point
			_              uint32
		}
		var m msg
		for {
			r, _, _ := pGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
			if int32(r) <= 0 {
				return
			}
			switch m.message {
			case 0x0312: // WM_HOTKEY
				onToggle()
			case wmUser: // new banner text
				for len(u.texts) > 0 {
					t := <-u.texts
					p, _ := windows.UTF16PtrFromString(t)
					pSetWindowTextW.Call(u.banner, uintptr(unsafe.Pointer(p)))
					show := uintptr(5) // SW_SHOW
					if t == "" {
						show = 0
					}
					pShowWindow.Call(u.banner, show)
					pSetWindowPos.Call(u.banner, ^uintptr(0), 0, 0, 0, 0, 0x1|0x2|0x10) // HWND_TOPMOST, NOSIZE|NOMOVE|NOACTIVATE
				}
			}
		}
	}()
	<-ready
	return u
}

func (u *ui) setBanner(t string) {
	select {
	case u.texts <- t:
	default:
	}
	pPostThreadMessageW.Call(uintptr(u.tid), wmUser, 0, 0)
}
