//go:build cgo

package main

/*
#cgo LDFLAGS: -framework AppKit -framework WebKit
#include <stdlib.h>
#include "desktop_darwin.h"
*/
import "C"

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strings"
	"unsafe"
)

var macAppHandler http.Handler

func init() {
	// AppKit 必须在进程的主线程上运行。
	runtime.LockOSThread()
}

func runDesktop() error {
	path, err := userDBPath()
	if err != nil {
		return fmt.Errorf("无法定位战史目录: %w", err)
	}
	store, err := OpenStore(path)
	if err != nil {
		return fmt.Errorf("初始化战史数据库失败: %w", err)
	}
	defer store.Close()

	macAppHandler, err = newMux(&apiServer{store: store})
	if err != nil {
		return err
	}
	C.DotaRunApp()
	return nil
}

//export DotaHandleRequest
func DotaHandleRequest(method, path, body *C.char) C.DotaResponse {
	request, err := http.NewRequest(C.GoString(method), C.GoString(path), strings.NewReader(C.GoString(body)))
	if err != nil || request.URL.IsAbs() || request.Host != "" || request.URL.Path == "" {
		return C.DotaResponse{status: http.StatusBadRequest}
	}
	response := httptest.NewRecorder()
	macAppHandler.ServeHTTP(response, request)
	data := response.Body.Bytes()
	return C.DotaResponse{
		status:       C.int(response.Code),
		content_type: C.CString(response.Header().Get("Content-Type")),
		body:         C.CBytes(data),
		size:         C.size_t(len(data)),
	}
}

func reportError(err error, desktop bool) {
	fmt.Fprintln(os.Stderr, "摇人失败:", err)
	if desktop {
		message := C.CString(err.Error())
		defer C.free(unsafe.Pointer(message))
		C.DotaShowError(message)
	}
}
