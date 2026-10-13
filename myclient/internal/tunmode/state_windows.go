//go:build windows

package tunmode

import (
	"fmt"
	"os/exec"
	"strings"

	"golang.org/x/sys/windows"
)

func secureStateFile(path string) error {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return fmt.Errorf("read current Windows user SID: %w", err)
	}
	sid := user.User.Sid.String()
	output, err := exec.Command("icacls.exe", path, "/inheritance:r", "/grant:r", "*"+sid+":F").CombinedOutput()
	if err != nil {
		return fmt.Errorf("restrict TUN state ACL: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}
