//go:build !windows && (!darwin || !cgo)

package main

import (
	"fmt"
	"os"
)

func reportError(err error, _ bool) {
	fmt.Fprintln(os.Stderr, "摇人失败:", err)
}

// runDesktop 在非 Windows 平台回退为「本地服务 + 默认浏览器」模式。
func runDesktop() error {
	store, err := OpenStore(DefaultDBPath())
	if err != nil {
		return err
	}
	defer store.Close()

	addr := ":8080"
	rawURL := browserURL(addr)
	go waitAndOpenBrowser(rawURL)
	return serve(addr, store)
}
