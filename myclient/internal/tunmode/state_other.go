//go:build !windows

package tunmode

import (
	"fmt"
	"os"
)

func secureStateFile(path string) error {
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("set private state-file permissions: %w", err)
	}
	return nil
}
