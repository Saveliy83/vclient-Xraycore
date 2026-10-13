package tunmode

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"example.com/vclient/internal/config"
	"example.com/vclient/internal/core"
)

type fakeRuntime struct {
	mu           sync.Mutex
	events       []string
	admin        bool
	failAdd      bool
	failDelete   bool
	dns          []string
	dnsSet       bool
	ipv6Adapters []IPv6AdapterState
	failIPv6If   uint32
	startTime    uint64
	processPID   int
}

func (runtime *fakeRuntime) record(event string) {
	runtime.mu.Lock()
	runtime.events = append(runtime.events, event)
	runtime.mu.Unlock()
}

func (runtime *fakeRuntime) IsAdmin() bool          { runtime.record("admin"); return runtime.admin }
func (runtime *fakeRuntime) WintunAvailable() error { runtime.record("wintun"); return nil }
func (runtime *fakeRuntime) DefaultGateway() (net.IP, uint32, error) {
	runtime.record("gateway")
	return net.ParseIP("192.0.2.1"), 7, nil
}
func (runtime *fakeRuntime) ResolveIPv4(context.Context, string) ([]net.IP, error) {
	runtime.record("resolve")
	return []net.IP{net.ParseIP("203.0.113.10")}, nil
}
func (runtime *fakeRuntime) AdapterIndex(string) (uint32, error) {
	runtime.record("adapter")
	return 21, nil
}
func (runtime *fakeRuntime) DNS(uint32) ([]string, error) {
	runtime.record("read-dns")
	return append([]string(nil), runtime.dns...), nil
}
func (runtime *fakeRuntime) SetDNS(_ uint32, servers []string) error {
	if runtime.dnsSet || slices.Equal(servers, runtime.dns) {
		runtime.record("restore-dns")
	} else {
		runtime.record("set-dns")
		runtime.dnsSet = true
	}
	return nil
}
func (runtime *fakeRuntime) PhysicalIPv6Adapters() ([]IPv6AdapterState, error) {
	runtime.record("read-physical-ipv6")
	return append([]IPv6AdapterState(nil), runtime.ipv6Adapters...), nil
}
func (runtime *fakeRuntime) SetIPv6Enabled(ifIndex uint32, enabled bool) error {
	runtime.record(fmt.Sprintf("set-ipv6:%d:%t", ifIndex, enabled))
	if runtime.failIPv6If == ifIndex {
		runtime.failIPv6If = 0
		return errors.New("injected IPv6 binding failure")
	}
	for i := range runtime.ipv6Adapters {
		if runtime.ipv6Adapters[i].InterfaceIndex == ifIndex {
			runtime.ipv6Adapters[i].Enabled = enabled
		}
	}
	return nil
}
func (runtime *fakeRuntime) AddRoute(route Route) error {
	runtime.record("add:" + route.Destination.String())
	if runtime.failAdd {
		runtime.failAdd = false
		return errors.New("injected route failure")
	}
	return nil
}
func (runtime *fakeRuntime) DeleteRoute(route Route) error {
	runtime.record("delete:" + route.Destination.String())
	if runtime.failDelete {
		return errors.New("injected delete failure")
	}
	return nil
}
func (runtime *fakeRuntime) BestRoute(net.IP) (uint32, error) {
	runtime.record("best-route")
	return 21, nil
}
func (runtime *fakeRuntime) ProcessStartTime(int) (uint64, error) {
	if runtime.startTime != 0 {
		return runtime.startTime, nil
	}
	return 1234, nil
}
func (runtime *fakeRuntime) ProcessMatches(pid int, startTime uint64) bool {
	currentPID := runtime.processPID
	if currentPID == 0 {
		currentPID = pid + 1
	}
	currentStart := runtime.startTime
	if currentStart == 0 {
		currentStart = 1234
	}
	return processIdentityMatches(pid, currentPID, startTime, currentStart)
}

type fakeRunner struct {
	runtime  *fakeRuntime
	startErr error
	stopErr  error
}

func (runner *fakeRunner) Start(context.Context) error {
	runner.runtime.record("start-runner")
	return runner.startErr
}
func (runner *fakeRunner) Stop() error {
	runner.runtime.record("stop-runner")
	return runner.stopErr
}

var _ core.Runner = (*fakeRunner)(nil)

func testProfile() config.Profile {
	return config.Profile{
		Name: "test", Protocol: "vless", Address: "203.0.113.10", Port: 443,
		UUID: "123e4567-e89b-12d3-a456-426614174000", Network: "tcp", Security: "tls",
	}
}

func testManager(t *testing.T, system *fakeRuntime) *Manager {
	t.Helper()
	path := filepath.Join(t.TempDir(), "tun-state.json")
	manager, err := NewManager(Options{
		Runtime:   system,
		StatePath: path,
		RunnerFactory: func([]byte) (core.Runner, error) {
			return &fakeRunner{runtime: system}, nil
		},
	})
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	return manager
}

func TestManagerEnableDisable(t *testing.T) {
	system := &fakeRuntime{admin: true, dns: []string{"192.0.2.53"}, ipv6Adapters: []IPv6AdapterState{
		{InterfaceIndex: 7, Name: "Ethernet", Enabled: true},
	}}
	manager := testManager(t, system)
	if err := manager.Enable(context.Background(), testProfile(), "127.0.0.1:1080", "127.0.0.1:1081", false); err != nil {
		t.Fatalf("Enable() error = %v", err)
	}
	if containsEvent(system.events, "add:::/1") || containsEvent(system.events, "add:8000::/1") {
		t.Errorf("IPv6 routes were installed while IPv6 is configured to bypass the VPN: %v", system.events)
	}
	if containsEvent(system.events, "read-physical-ipv6") || strings.Contains(strings.Join(system.events, ","), "set-ipv6:") {
		t.Errorf("IPv6 adapters were changed while BlockIPv6 is false: %v", system.events)
	}
	if !system.ipv6Adapters[0].Enabled {
		t.Errorf("physical IPv6 was changed while BlockIPv6 is false: %+v", system.ipv6Adapters[0])
	}
	if _, err := os.Stat(manager.statePath); err != nil {
		t.Fatalf("state file was not saved: %v", err)
	}
	if err := manager.Disable(); err != nil {
		t.Fatalf("Disable() error = %v", err)
	}
	if _, err := os.Stat(manager.statePath); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("state file still exists after disable: %v", err)
	}
	assertEventOrder(t, system.events, []string{"admin", "wintun", "resolve", "gateway", "start-runner", "adapter", "read-dns", "set-dns", "best-route", "restore-dns", "stop-runner"})
}

func TestManagerBlockIPv6AndRestorePhysicalAdapterBindings(t *testing.T) {
	system := &fakeRuntime{admin: true, dns: []string{"192.0.2.53"}, ipv6Adapters: []IPv6AdapterState{
		{InterfaceIndex: 7, Name: "Ethernet", Enabled: true},
		{InterfaceIndex: 8, Name: "Wi-Fi", Enabled: false},
	}}
	manager := testManager(t, system)
	if err := manager.Enable(context.Background(), testProfile(), "127.0.0.1:1080", "127.0.0.1:1081", true); err != nil {
		t.Fatalf("Enable() error = %v", err)
	}
	if system.ipv6Adapters[0].Enabled || !containsEvent(system.events, "set-ipv6:7:false") {
		t.Fatalf("IPv6 was not disabled on the enabled physical adapter: %+v", system.ipv6Adapters)
	}
	if containsEvent(system.events, "add:::/1") || containsEvent(system.events, "add:8000::/1") {
		t.Fatalf("IPv6 default routes were installed while IPv6 blocking is enabled: %v", system.events)
	}
	state, err := LoadState(manager.statePath)
	if err != nil {
		t.Fatalf("LoadState() error = %v", err)
	}
	if len(state.IPv6Adapters) != 2 || !state.IPv6Adapters[0].Enabled || state.IPv6Adapters[1].Enabled {
		t.Fatalf("state did not save original physical adapter IPv6 bindings: %+v", state.IPv6Adapters)
	}
	if err := manager.Disable(); err != nil {
		t.Fatalf("Disable() error = %v", err)
	}
	if !system.ipv6Adapters[0].Enabled || system.ipv6Adapters[1].Enabled {
		t.Fatalf("physical adapter IPv6 state was not restored: %+v", system.ipv6Adapters)
	}
}

func TestManagerBlockIPv6RollsBackWhenSecondAdapterFails(t *testing.T) {
	system := &fakeRuntime{admin: true, failIPv6If: 8, ipv6Adapters: []IPv6AdapterState{
		{InterfaceIndex: 7, Name: "Ethernet", Enabled: true},
		{InterfaceIndex: 8, Name: "Wi-Fi", Enabled: true},
	}}
	manager := testManager(t, system)
	err := manager.Enable(context.Background(), testProfile(), "127.0.0.1:1080", "127.0.0.1:1081", true)
	if err == nil || !strings.Contains(err.Error(), "injected IPv6 binding failure") {
		t.Fatalf("Enable() error = %v, want injected second-adapter failure", err)
	}
	for _, adapter := range system.ipv6Adapters {
		if !adapter.Enabled {
			t.Errorf("adapter %q IPv6 state was not rolled back: %+v", adapter.Name, adapter)
		}
	}
	if !containsEvent(system.events, "set-ipv6:7:false") ||
		!containsEvent(system.events, "set-ipv6:7:true") ||
		!containsEvent(system.events, "set-ipv6:8:true") {
		t.Errorf("second-adapter failure did not restore every saved adapter state: %v", system.events)
	}
	if containsEvent(system.events, "restore-dns") {
		t.Errorf("rollback changed DNS although original TUN DNS was not captured: %v", system.events)
	}
	if _, statErr := os.Stat(manager.statePath); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("state file remains after successful physical IPv6 rollback: %v", statErr)
	}
}

func TestManagerRouteFailureRollsBack(t *testing.T) {
	system := &fakeRuntime{admin: true, failAdd: true}
	manager := testManager(t, system)
	err := manager.Enable(context.Background(), testProfile(), "127.0.0.1:1080", "127.0.0.1:1081", false)
	if err == nil || !strings.Contains(err.Error(), "injected route failure") {
		t.Fatalf("Enable() error = %v, want injected route failure", err)
	}
	if !containsEvent(system.events, "stop-runner") || !containsEvent(system.events, "restore-dns") {
		t.Errorf("route failure did not roll back runner and DNS: %v", system.events)
	}
	if _, statErr := os.Stat(manager.statePath); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("state file remains after successful rollback: %v", statErr)
	}
}

func TestManagerDisableContinuesAfterCleanupFailure(t *testing.T) {
	system := &fakeRuntime{admin: true, failDelete: true}
	manager := testManager(t, system)
	if err := manager.Enable(context.Background(), testProfile(), "127.0.0.1:1080", "127.0.0.1:1081", false); err != nil {
		t.Fatalf("Enable() error = %v", err)
	}
	if err := manager.Disable(); err == nil {
		t.Fatal("Disable() error = nil, want route cleanup failure")
	} else if !strings.Contains(err.Error(), "route print") {
		t.Errorf("Disable() error lacks manual recovery instructions: %v", err)
	}
	if _, err := os.Stat(manager.statePath); err != nil {
		t.Errorf("state file was removed despite remaining routes: %v", err)
	}
	if !containsEvent(system.events, "restore-dns") || !containsEvent(system.events, "stop-runner") {
		t.Errorf("Disable() stopped cleanup after a route error: %v", system.events)
	}
	deleted := 0
	for _, event := range system.events {
		if strings.HasPrefix(event, "delete:") {
			deleted++
		}
	}
	if deleted != 2*(len(manager.state.AddedRoutes)+len(manager.state.ProtectRoutes)) {
		t.Errorf("DeleteRoute attempted %d times; want two attempts for each of %d remaining routes",
			deleted, len(manager.state.AddedRoutes)+len(manager.state.ProtectRoutes))
	}
}

func TestManagerRecoversStateSavedBeforeAdapterCreation(t *testing.T) {
	system := &fakeRuntime{admin: true}
	manager := testManager(t, system)
	if err := SaveState(manager.statePath, State{
		PID: os.Getpid() + 1, ProcessStartTime: 1, Gateway: "192.0.2.1", GatewayIf: 7,
	}); err != nil {
		t.Fatalf("SaveState() error = %v", err)
	}

	if err := manager.RecoverOrphan(); err != nil {
		t.Fatalf("RecoverOrphan() error = %v", err)
	}
	if _, err := os.Stat(manager.statePath); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("recovery state still exists: %v", err)
	}
}

func TestStateJSONSupportsIPv6Route(t *testing.T) {
	state := State{
		PID: 2, ProcessStartTime: 1, GatewayIf: 3, TunIf: 4,
		AddedRoutes: []Route{{Destination: netip.MustParsePrefix("::/1"), Interface: 4}},
	}
	path := filepath.Join(t.TempDir(), "state.json")
	if err := SaveState(path, state); err != nil {
		t.Fatal(err)
	}
	got, err := LoadState(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.AddedRoutes[0].Destination.String() != "::/1" {
		t.Errorf("IPv6 route = %s", got.AddedRoutes[0].Destination)
	}
}

func TestRecoverOrphanDoesNotTrustReusedPID(t *testing.T) {
	system := &fakeRuntime{admin: true, processPID: os.Getpid(), startTime: 200}
	manager := testManager(t, system)
	if err := SaveState(manager.statePath, State{
		PID: os.Getpid(), ProcessStartTime: 100, Gateway: "192.0.2.1", GatewayIf: 7,
	}); err != nil {
		t.Fatalf("SaveState() error = %v", err)
	}
	if err := manager.RecoverOrphan(); err != nil {
		t.Fatalf("RecoverOrphan() error = %v", err)
	}
	if _, err := os.Stat(manager.statePath); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("state with reused PID was not recovered: %v", err)
	}
}

func assertEventOrder(t *testing.T, events, expected []string) {
	t.Helper()
	position := 0
	for _, event := range events {
		if position < len(expected) && event == expected[position] {
			position++
		}
	}
	if position != len(expected) {
		t.Errorf("event order missing %q in %v", expected[position], events)
	}
}

func containsEvent(events []string, expected string) bool {
	for _, event := range events {
		if event == expected {
			return true
		}
	}
	return false
}
