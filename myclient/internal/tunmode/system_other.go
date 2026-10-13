//go:build !windows

package tunmode

import (
	"context"
	"errors"
	"fmt"
	"net"
)

type unsupportedRuntime struct{}

func NewSystemRuntime() (Runtime, error) {
	return unsupportedRuntime{}, nil
}

func IsAdmin() bool { return false }

func CheckWintunDLL() error {
	return fmt.Errorf("check Wintun DLL: %w", errors.New("full-tunnel mode is only supported on Windows"))
}

func (unsupportedRuntime) IsAdmin() bool { return false }

func (unsupportedRuntime) WintunAvailable() error { return CheckWintunDLL() }

func (unsupportedRuntime) DefaultGateway() (net.IP, uint32, error) {
	return nil, 0, unsupportedError()
}

func (unsupportedRuntime) ResolveIPv4(context.Context, string) ([]net.IP, error) {
	return nil, unsupportedError()
}

func (unsupportedRuntime) AdapterIndex(string) (uint32, error) { return 0, unsupportedError() }

func (unsupportedRuntime) DNS(uint32) ([]string, error) { return nil, unsupportedError() }

func (unsupportedRuntime) SetDNS(uint32, []string) error { return unsupportedError() }

func (unsupportedRuntime) PhysicalIPv6Adapters() ([]IPv6AdapterState, error) {
	return nil, unsupportedError()
}

func (unsupportedRuntime) SetIPv6Enabled(uint32, bool) error { return unsupportedError() }

func (unsupportedRuntime) AddRoute(Route) error { return unsupportedError() }

func (unsupportedRuntime) DeleteRoute(Route) error { return unsupportedError() }

func (unsupportedRuntime) BestRoute(net.IP) (uint32, error) { return 0, unsupportedError() }

func (unsupportedRuntime) ProcessStartTime(int) (uint64, error) { return 0, unsupportedError() }

func (unsupportedRuntime) ProcessMatches(int, uint64) bool { return false }

func unsupportedError() error {
	return fmt.Errorf("TUN system operation: %w", errors.New("full-tunnel mode is only supported on Windows"))
}
