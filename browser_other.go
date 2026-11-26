//go:build !windows

package main

import (
	"os/exec"
	"runtime"
)

func openBrowser(rawURL string) error {
	command := "xdg-open"
	if runtime.GOOS == "darwin" {
		command = "open"
	}
	return exec.Command(command, rawURL).Run()
}
