package tunmode

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestStateSaveLoadAndCorruption(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tun-state.json")
	state := State{PID: 42, ProcessStartTime: 123, CreatedAt: time.Now(), Gateway: "192.0.2.1", GatewayIf: 3, TunIf: 8}
	if err := SaveState(path, state); err != nil {
		t.Fatalf("SaveState() error = %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat state file: %v", err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Errorf("state permissions = %o, want 600", info.Mode().Perm())
	}
	got, err := LoadState(path)
	if err != nil {
		t.Fatalf("LoadState() error = %v", err)
	}
	if got.PID != state.PID || got.Gateway != state.Gateway || got.TunIf != state.TunIf {
		t.Errorf("loaded state = %+v, want %+v", got, state)
	}
	if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
		t.Fatalf("write corrupt state: %v", err)
	}
	if _, err := LoadState(path); err == nil {
		t.Fatal("LoadState() error = nil for corrupt state")
	}
}

func TestStateAllowsRecoveryBeforeTUNAdapterAppears(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tun-state.json")
	state := State{PID: 42, ProcessStartTime: 123, Gateway: "192.0.2.1", GatewayIf: 3}
	if err := SaveState(path, state); err != nil {
		t.Fatalf("SaveState() error = %v", err)
	}

	got, err := LoadState(path)
	if err != nil {
		t.Fatalf("LoadState() should accept pre-adapter recovery state: %v", err)
	}
	if got.TunIf != 0 {
		t.Errorf("TunIf = %d, want 0 before adapter creation", got.TunIf)
	}
}

func TestProcessIdentityRequiresMatchingPIDAndStartTime(t *testing.T) {
	if !processIdentityMatches(17, 17, 0x123456789, 0x123456789) {
		t.Fatal("matching PID and start time were not considered the same process")
	}

	if processIdentityMatches(17, 17, 0x123456789, 0x987654321) {
		t.Fatal("same PID with different start time was considered the same process")
	}
}

func TestStateFromEarlierTUNBuildRemainsRecoverable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy-state.json")
	legacyState := `{"pid":42,"gateway":"192.0.2.1","gateway_interface":3,"tun_interface":8,"added_routes":[],"protect_routes":[]}`
	if err := os.WriteFile(path, []byte(legacyState), 0o600); err != nil {
		t.Fatalf("write legacy recovery state: %v", err)
	}
	state, err := LoadState(path)
	if err != nil {
		t.Fatalf("LoadState() legacy state error = %v", err)
	}
	if state.ProcessStartTime != 0 || processIdentityMatches(state.PID, state.PID, state.ProcessStartTime, 100) {
		t.Fatalf("legacy process identity should be treated as orphaned: %+v", state)
	}
}
