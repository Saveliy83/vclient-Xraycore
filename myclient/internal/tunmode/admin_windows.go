//go:build windows

package tunmode

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

func IsAdmin() bool {
	return windows.GetCurrentProcessToken().IsElevated()
}

func CheckWintunDLL() error {
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable path: %w", err)
	}
	path := filepath.Join(filepath.Dir(executable), "wintun.dll")
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("check Wintun DLL %q: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("check Wintun DLL %q: %w", path, errors.New("path is not a regular file"))
	}
	return nil
}
