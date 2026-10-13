//go:build windows

package app

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/windows/registry"
)

const autoStartRegistryPath = `Software\Microsoft\Windows\CurrentVersion\Run`

func SetAutoStartEnabled(enabled bool) (result error) {
	if !enabled {
		key, err := registry.OpenKey(registry.CURRENT_USER, autoStartRegistryPath, registry.SET_VALUE)
		if err != nil {
			if errors.Is(err, registry.ErrNotExist) {
				return nil
			}
			return fmt.Errorf("open Windows auto-start registry key: %w", err)
		}
		defer func() {
			if err := key.Close(); err != nil {
				result = errors.Join(result, fmt.Errorf("close Windows auto-start registry key: %w", err))
			}
		}()
		if err := key.DeleteValue("vclient"); err != nil && !errors.Is(err, registry.ErrNotExist) {
			return fmt.Errorf("remove Windows auto-start entry: %w", err)
		}
		return nil
	}

	executablePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve application executable path: %w", err)
	}
	key, _, err := registry.CreateKey(registry.CURRENT_USER, autoStartRegistryPath, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("create Windows auto-start registry key: %w", err)
	}
	defer func() {
		if err := key.Close(); err != nil {
			result = errors.Join(result, fmt.Errorf("close Windows auto-start registry key: %w", err))
		}
	}()
	if err := key.SetStringValue("vclient", AutoStartCommand(executablePath)); err != nil {
		return fmt.Errorf("write Windows auto-start entry: %w", err)
	}
	return nil
}
