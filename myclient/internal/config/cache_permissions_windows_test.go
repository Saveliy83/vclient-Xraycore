//go:build windows

package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func TestRestrictCacheFileGrantsOnlyCurrentUser(t *testing.T) {
	path := filepath.Join(t.TempDir(), "subscription.json")
	if err := os.WriteFile(path, []byte("[]"), 0o600); err != nil {
		t.Fatalf("create cache file: %v", err)
	}
	if err := restrictCacheFile(path); err != nil {
		t.Fatalf("restrictCacheFile() error = %v", err)
	}
	descriptor, err := windows.GetNamedSecurityInfo(
		path,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
	)
	if err != nil {
		t.Fatalf("read cache DACL: %v", err)
	}
	acl, _, err := descriptor.DACL()
	if err != nil {
		t.Fatalf("read cache ACL: %v", err)
	}
	if acl == nil || acl.AceCount != 1 {
		t.Fatalf("cache DACL ACE count = %v, want exactly one", acl)
	}
	currentUser, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatalf("get current user: %v", err)
	}
	if !strings.Contains(descriptor.String(), ";;;"+currentUser.User.Sid.String()+")") {
		t.Fatal("cache DACL grants access to an identity other than the current user")
	}
}
