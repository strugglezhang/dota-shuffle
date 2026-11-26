//go:build windows

package main

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"unsafe"

	"github.com/jchv/go-webview2"
)

const desktopTitle = "DOTA 内战局"

var (
	user32               = syscall.NewLazyDLL("user32.dll")
	setProcessDPIContext = user32.NewProc("SetProcessDpiAwarenessContext")
	setProcessDPIAware   = user32.NewProc("SetProcessDPIAware")
	getDPIForSystem      = user32.NewProc("GetDpiForSystem")
	systemParametersInfo = user32.NewProc("SystemParametersInfoW")
	setWindowPos         = user32.NewProc("SetWindowPos")
	messageBox           = user32.NewProc("MessageBoxW")
	shellExecute         = syscall.NewLazyDLL("shell32.dll").NewProc("ShellExecuteW")
	comctl32             = syscall.NewLazyDLL("comctl32.dll")
	setWindowSubclass    = comctl32.NewProc("SetWindowSubclass")
	defSubclassProc      = comctl32.NewProc("DefSubclassProc")
	removeWindowSubclass = comctl32.NewProc("RemoveWindowSubclass")
	windowSubclass       uintptr
)

func init() {
	windowSubclass = syscall.NewCallback(desktopWindowProc)
}

type screenRect struct {
	left, top, right, bottom int32
}

// runDesktop 使用一个仅本机可访问的监听器为桌面窗口提供界面和 API。
func runDesktop() error {
	path, err := windowsDesktopDBPath()
	if err != nil {
		return fmt.Errorf("无法定位战史目录: %w", err)
	}
	store, err := OpenStore(path)
	if err != nil {
		return fmt.Errorf("初始化数据库失败: %w", err)
	}
	defer store.Close()

	mux, err := newMux(&apiServer{store: store})
	if err != nil {
		return err
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("申请本地端口失败: %w", err)
	}
	defer ln.Close()
	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(ln) }()
	defer srv.Close()

	rawURL := "http://" + ln.Addr().String() + "/"
	return runWebviewWindow(rawURL)
}

func windowsDesktopDBPath() (string, error) {
	// 继续使用旧版已存在的便携战史。新安装把数据写入用户目录。
	if exe, err := os.Executable(); err == nil {
		legacy := filepath.Join(filepath.Dir(exe), "dota-shuffle.db")
		if _, err := os.Stat(legacy); err == nil && dirWritable(filepath.Dir(legacy)) {
			return legacy, nil
		}
	}
	return userDBPath()
}

func runWebviewWindow(rawURL string) (err error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("桌面窗口初始化失败: %v", r)
		}
	}()

	enableDesktopDPI()
	if err := prepareWebViewRuntime(); err != nil {
		return fmt.Errorf("准备 WebView2 运行时失败: %w", err)
	}
	data, err := os.UserCacheDir()
	if err != nil {
		return fmt.Errorf("无法定位 WebView2 用户缓存目录: %w", err)
	}

	bounds := initialDesktopBounds()
	w := webview2.NewWithOptions(webview2.WebViewOptions{
		AutoFocus: true,
		DataPath:  filepath.Join(data, "dota-shuffle", "webview-profile"),
		WindowOptions: webview2.WindowOptions{
			Title:  desktopTitle,
			Width:  uint(bounds.right - bounds.left),
			Height: uint(bounds.bottom - bounds.top),
		},
	})
	if w == nil {
		return fmt.Errorf("无法创建 WebView2 窗口")
	}
	defer w.Destroy()
	hwnd := uintptr(w.Window())
	if setWindowSubclass.Find() == nil && defSubclassProc.Find() == nil && removeWindowSubclass.Find() == nil {
		setWindowSubclass.Call(hwnd, windowSubclass, 1, 0)
	}
	moveDesktopWindow(hwnd, bounds)
	w.Navigate(rawURL)
	w.Run()
	return nil
}

func enableDesktopDPI() {
	if setProcessDPIContext.Find() == nil {
		// DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2 = -4.
		if ok, _, _ := setProcessDPIContext.Call(^uintptr(3)); ok != 0 {
			return
		}
	}
	setProcessDPIAware.Call()
}

func initialDesktopBounds() screenRect {
	const spiGetWorkArea = 0x0030
	var work screenRect
	if ok, _, _ := systemParametersInfo.Call(spiGetWorkArea, 0, uintptr(unsafe.Pointer(&work)), 0); ok == 0 {
		return screenRect{right: 1024, bottom: 720}
	}
	dpi := uintptr(96)
	if getDPIForSystem.Find() == nil {
		if value, _, _ := getDPIForSystem.Call(); value != 0 {
			dpi = value
		}
	}
	workWidth, workHeight := work.right-work.left, work.bottom-work.top
	width := min(int32(1440*dpi/96), workWidth*94/100)
	height := min(int32(900*dpi/96), workHeight*92/100)
	left := work.left + (workWidth-width)/2
	top := work.top + (workHeight-height)/2
	return screenRect{left, top, left + width, top + height}
}

func moveDesktopWindow(hwnd uintptr, bounds screenRect) {
	const swpNoZOrderOrActivate = 0x0004 | 0x0010
	setWindowPos.Call(hwnd, 0, uintptr(bounds.left), uintptr(bounds.top),
		uintptr(bounds.right-bounds.left), uintptr(bounds.bottom-bounds.top), swpNoZOrderOrActivate)
}

func desktopWindowProc(hwnd, msg, wParam, lParam, id, _ uintptr) uintptr {
	const (
		wmDPIChanged = 0x02e0
		wmNCDestroy  = 0x0082
	)
	switch msg {
	case wmDPIChanged:
		// Windows 提供新显示器 DPI 下建议的窗口位置和尺寸。
		moveDesktopWindow(hwnd, *(*screenRect)(unsafe.Pointer(lParam)))
		return 0
	case wmNCDestroy:
		removeWindowSubclass.Call(hwnd, windowSubclass, id)
	}
	result, _, _ := defSubclassProc.Call(hwnd, msg, wParam, lParam)
	return result
}

func reportError(err error, desktop bool) {
	fmt.Fprintln(os.Stderr, "摇人失败:", err)
	if desktop {
		showDesktopMessage(err.Error(), 0x10) // MB_ICONERROR
	}
}

func showDesktopMessage(message string, icon uintptr) {
	title, _ := syscall.UTF16PtrFromString(desktopTitle)
	body, _ := syscall.UTF16PtrFromString(message)
	messageBox.Call(0, uintptr(unsafe.Pointer(body)), uintptr(unsafe.Pointer(title)), icon)
}

func openBrowser(rawURL string) error {
	url, err := syscall.UTF16PtrFromString(rawURL)
	if err != nil {
		return err
	}
	// 直接调用 Windows Shell，避免启动 cmd.exe。
	const swShowNormal = 1
	result, _, _ := shellExecute.Call(0, 0, uintptr(unsafe.Pointer(url)), 0, 0, swShowNormal)
	if result <= 32 {
		return fmt.Errorf("打开默认浏览器失败（ShellExecuteW 返回 %d）", result)
	}
	return nil
}
