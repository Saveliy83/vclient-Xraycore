//go:build !windows

package tunmode

import (
	"strings"
	"testing"
)

func TestNonWindowsTUNIsUnsupported(t *testing.T) {
	if IsAdmin() {
		t.Fatal("IsAdmin() = true on non-Windows platform")
	}
	if err := CheckWintunDLL(); err == nil || !strings.Contains(err.Error(), "only supported on Windows") {
		t.Fatalf("CheckWintunDLL() error = %v, want unsupported-platform error", err)
	}
}
