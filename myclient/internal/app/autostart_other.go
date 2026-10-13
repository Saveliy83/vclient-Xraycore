//go:build !windows

package app

import (
	"errors"
	"fmt"
)

func SetAutoStartEnabled(bool) error {
	return fmt.Errorf("configure auto-start: %w", errors.New("only supported on Windows"))
}
