//go:build !windows

package gui

import (
	"fmt"
	"os/exec"
	"runtime"
)

func openDirectory(path string) error {
	command := "xdg-open"
	if runtime.GOOS == "darwin" {
		command = "open"
	}
	if err := exec.Command(command, path).Start(); err != nil {
		return fmt.Errorf("open data directory: %w", err)
	}
	return nil
}
