package tunmode

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type State struct {
	PID              int                `json:"pid"`
	ProcessStartTime uint64             `json:"process_start_time"`
	CreatedAt        time.Time          `json:"created_at"`
	Gateway          string             `json:"gateway"`
	GatewayIf        uint32             `json:"gateway_interface"`
	TunIf            uint32             `json:"tun_interface"`
	IPv6Adapters     []IPv6AdapterState `json:"ipv6_adapters,omitempty"`
	IPv6WasEnabled   *bool              `json:"ipv6_was_enabled,omitempty"`
	DNS              []string           `json:"dns,omitempty"`
	DNSCaptured      bool               `json:"dns_captured,omitempty"`
	AddedRoutes      []Route            `json:"added_routes"`
	ProtectRoutes    []Route            `json:"protect_routes"`
}

type IPv6AdapterState struct {
	InterfaceIndex uint32 `json:"interface_index"`
	Name           string `json:"name"`
	Enabled        bool   `json:"enabled"`
}

func processIdentityMatches(savedPID, currentPID int, savedStartTime, currentStartTime uint64) bool {
	return savedPID > 0 && savedPID == currentPID && savedStartTime != 0 && savedStartTime == currentStartTime
}

func SaveState(path string, state State) error {
	if path == "" {
		return fmt.Errorf("save TUN state: %w", errors.New("path must not be empty"))
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode TUN state: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create TUN state directory: %w", err)
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".tun-state-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary TUN state: %w", err)
	}
	tempPath := temp.Name()
	renamed := false
	defer func() {
		if !renamed {
			_ = os.Remove(tempPath)
		}
	}()
	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return fmt.Errorf("set TUN state permissions: %w", err)
	}
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return fmt.Errorf("write TUN state: %w", err)
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return fmt.Errorf("sync TUN state: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close TUN state: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("replace TUN state: %w", err)
	}
	renamed = true
	if err := secureStateFile(path); err != nil {
		removeErr := os.Remove(path)
		if removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			err = errors.Join(err, removeErr)
		}
		return fmt.Errorf("secure TUN state file: %w", err)
	}
	return nil
}

func LoadState(path string) (State, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- path is the application-owned TUN state file.
	if err != nil {
		return State{}, fmt.Errorf("read TUN state: %w", err)
	}
	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return State{}, fmt.Errorf("decode TUN state: %w", err)
	}
	if state.PID < 1 || state.GatewayIf == 0 {
		return State{}, fmt.Errorf("validate TUN state: %w", errors.New("required process or gateway interface data is missing"))
	}
	return state, nil
}
