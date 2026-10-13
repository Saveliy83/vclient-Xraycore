//go:build windows

package gui

import (
	"fmt"
	"os/exec"
)

func openDirectory(path string) error {
	// #nosec G204 -- the path is passed as one process argument, not interpreted by a shell.
	if err := exec.Command("explorer.exe", path).Start(); err != nil {
		return fmt.Errorf("open data directory: %w", err)
	}
	return nil
}
