//go:build standalone

package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	_ "embed"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

//go:embed .webview2-runtime.zip
var packedWebViewRuntime []byte

func prepareWebViewRuntime() error {
	cache, err := os.UserCacheDir()
	if err != nil {
		return err
	}
	root := filepath.Join(cache, "dota-shuffle", "webview2")
	hash := sha256.Sum256(packedWebViewRuntime)
	target := filepath.Join(root, fmt.Sprintf("%x", hash[:16]))
	executable := filepath.Join(target, "msedgewebview2.exe")
	if _, err := os.Stat(executable); os.IsNotExist(err) {
		if err := unpackWebViewRuntime(root, target); err != nil {
			// Another instance may have published the same runtime while we unpacked.
			if _, published := os.Stat(executable); published != nil {
				return err
			}
		}
	} else if err != nil {
		return err
	}
	return os.Setenv("WEBVIEW2_BROWSER_EXECUTABLE_FOLDER", target)
}

func unpackWebViewRuntime(root, target string) error {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}
	dir, err := os.MkdirTemp(root, "unpack-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)

	archive, err := zip.NewReader(bytes.NewReader(packedWebViewRuntime), int64(len(packedWebViewRuntime)))
	if err != nil {
		return err
	}
	for _, entry := range archive.File {
		relative := filepath.FromSlash(entry.Name)
		if !filepath.IsLocal(relative) {
			return fmt.Errorf("WebView2 包含无效路径 %q", entry.Name)
		}
		path := filepath.Join(dir, relative)
		if entry.FileInfo().IsDir() {
			if err := os.MkdirAll(path, 0o755); err != nil {
				return err
			}
			continue
		}
		if !entry.Mode().IsRegular() {
			return fmt.Errorf("WebView2 包含无效文件 %q", entry.Name)
		}
		if err := unpackRuntimeFile(entry, path); err != nil {
			return err
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "msedgewebview2.exe")); err != nil {
		return fmt.Errorf("WebView2 压缩包缺少主程序: %w", err)
	}

	// Windows 10 的 WebView2 沙箱需要读取内置运行时目录。
	icacls := filepath.Join(os.Getenv("SystemRoot"), "System32", "icacls.exe")
	cmd := exec.Command(icacls, dir, "/grant", "*S-1-15-2-1:(OI)(CI)(RX)", "*S-1-15-2-2:(OI)(CI)(RX)")
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000} // CREATE_NO_WINDOW
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("设置 WebView2 运行时读取权限失败: %w (%s)", err, output)
	}
	return os.Rename(dir, target)
}

func unpackRuntimeFile(entry *zip.File, path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	source, err := entry.Open()
	if err != nil {
		return err
	}
	defer source.Close()

	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(file, source)
	closeErr := file.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}
