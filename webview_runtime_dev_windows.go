//go:build !standalone

package main

import (
	"errors"

	"github.com/jchv/go-webview2/webviewloader"
)

// 源码调试构建可以使用本机 WebView2。发行版由 build-windows.sh 打包固定版本。
func prepareWebViewRuntime() error {
	version, err := webviewloader.GetInstalledVersion()
	if err != nil {
		return err
	}
	if version == "" {
		return errors.New("缺少 WebView2 Runtime，请使用带内置运行时的发行版")
	}
	return nil
}
