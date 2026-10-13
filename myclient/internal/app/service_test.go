package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"

	"example.com/vclient/internal/config"
	"example.com/vclient/internal/core"
)

func TestServiceConnectionStateTransitionsAndSnapshots(t *testing.T) {
	service, runners := newTestService(t, nil)
	var snapshots []Snapshot
	unsubscribe := service.Subscribe(func(snapshot Snapshot) {
		snapshots = append(snapshots, snapshot)
	})
	defer unsubscribe()

	if err := service.Connect(context.Background(), 0); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	active := service.Snapshot()
	if active.State != Connected || active.ActiveProfile != "service-test" || active.StartedAt.IsZero() {
		t.Fatalf("connected snapshot = %#v", active)
	}
	if err := service.Disconnect(); err != nil {
		t.Fatalf("Disconnect() error = %v", err)
	}
	if service.State() != Disconnected {
		t.Fatalf("State() = %s, want disconnected", service.State())
	}
	wantStates := []ConnState{Connecting, Connected, Disconnected}
	if len(snapshots) != len(wantStates) {
		t.Fatalf("snapshot count = %d, want %d: %#v", len(snapshots), len(wantStates), snapshots)
	}
	for index, want := range wantStates {
		if snapshots[index].State != want {
			t.Errorf("snapshot %d state = %s, want %s", index, snapshots[index].State, want)
		}
	}
	if len(*runners) != 1 || (*runners)[0].starts != 1 || (*runners)[0].stops != 1 {
		t.Fatalf("runner calls = %#v, want one start and stop", *runners)
	}
}

func TestServiceFailedStartPublishesFailedState(t *testing.T) {
	startErr := errors.New("start failed")
	service, runners := newTestService(t, startErr)
	var snapshots []Snapshot
	service.Subscribe(func(snapshot Snapshot) { snapshots = append(snapshots, snapshot) })

	err := service.Connect(context.Background(), 0)
	if !errors.Is(err, startErr) {
		t.Fatalf("Connect() error = %v, want start error", err)
	}
	snapshot := service.Snapshot()
	if snapshot.State != Failed || snapshot.LastError == "" {
		t.Fatalf("failed snapshot = %#v", snapshot)
	}
	if len(snapshots) != 2 || snapshots[0].State != Connecting || snapshots[1].State != Failed {
		t.Fatalf("snapshots = %#v, want connecting then failed", snapshots)
	}
	if len(*runners) != 1 || (*runners)[0].stops != 1 {
		t.Fatalf("failed runner cleanup = %#v, want Stop called", *runners)
	}
}

func TestConnectWhileConnectedRestartsRunner(t *testing.T) {
	service, runners := newTestService(t, nil)
	if err := service.Connect(context.Background(), 0); err != nil {
		t.Fatalf("first Connect() error = %v", err)
	}
	if err := service.Connect(context.Background(), 0); err != nil {
		t.Fatalf("second Connect() error = %v", err)
	}
	if len(*runners) != 2 {
		t.Fatalf("runner count = %d, want 2", len(*runners))
	}
	if (*runners)[0].starts != 1 || (*runners)[0].stops != 1 || (*runners)[1].starts != 1 || (*runners)[1].stops != 0 {
		t.Fatalf("runner calls = %#v, want first stopped and second running", *runners)
	}
	if err := service.Disconnect(); err != nil {
		t.Fatalf("Disconnect() error = %v", err)
	}
}

func TestServiceConnectAndDisconnectFullTunnel(t *testing.T) {
	profiles, err := NewProfileStore(filepath.Join(t.TempDir(), "profiles.json"))
	if err != nil {
		t.Fatalf("NewProfileStore() error = %v", err)
	}
	if err := profiles.Add(testProfile("full-tunnel")); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	tunnel := &testTunnelManager{}
	settings := DefaultSettings()
	settings.FullTunnel = true
	service, err := NewService(ServiceOptions{
		Profiles: profiles, Settings: settings, TunnelManager: tunnel,
		SettingsPath: filepath.Join(t.TempDir(), "settings.json"),
	})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	if err := service.Connect(context.Background(), 0); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	if service.State() != Connected || tunnel.enables != 1 || !tunnel.blockIPv6 {
		t.Fatalf("state=%s enables=%d, want connected and one enable", service.State(), tunnel.enables)
	}
	if err := service.Disconnect(); err != nil {
		t.Fatalf("Disconnect() error = %v", err)
	}
	if service.State() != Disconnected || tunnel.disables != 1 {
		t.Fatalf("state=%s disables=%d, want disconnected and one disable", service.State(), tunnel.disables)
	}
}

func TestServiceLogsRingBufferFilteringAndSubscription(t *testing.T) {
	base := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug}))
	service, _ := newTestServiceWithLogger(t, nil, base)
	var received int
	unsubscribe := service.SubscribeLogs(func(LogEntry) { received++ })
	for index := 0; index < maxLogEntries+5; index++ {
		service.Logger().Debug(fmt.Sprintf("entry-%d", index))
	}
	unsubscribe()
	service.Logger().Info("visible info")
	service.Logger().Error("visible error")

	entries := service.Logs("debug")
	if len(entries) != maxLogEntries {
		t.Fatalf("Logs(debug) entries = %d, want %d", len(entries), maxLogEntries)
	}
	if entries[0].Message != "entry-7" {
		t.Errorf("oldest retained message = %q, want entry-7", entries[0].Message)
	}
	debugEntries := service.Logs("error")
	if len(debugEntries) != 1 || debugEntries[0].Message != "visible error" {
		t.Fatalf("Logs(error) = %#v, want only error", debugEntries)
	}
	if received != maxLogEntries+5 {
		t.Errorf("log subscriber received %d entries, want %d", received, maxLogEntries+5)
	}
	if got := service.Logs("invalid"); len(got) != 0 {
		t.Errorf("Logs(invalid) = %#v, want empty", got)
	}
}

func TestServiceShutdownPersistsSettingsAndProfiles(t *testing.T) {
	profilePath := filepath.Join(t.TempDir(), "profiles.json")
	profiles, err := NewProfileStore(profilePath)
	if err != nil {
		t.Fatalf("NewProfileStore() error = %v", err)
	}
	if err := profiles.Add(testProfile("shutdown")); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	settingsPath := filepath.Join(t.TempDir(), "settings.json")
	service, err := NewService(ServiceOptions{
		Profiles: profiles, Settings: DefaultSettings(), SettingsPath: settingsPath,
	})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	if err := service.Shutdown(); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	if _, _, err := LoadSettingsFrom(settingsPath); err != nil {
		t.Fatalf("LoadSettingsFrom() after shutdown error = %v", err)
	}
	loaded, err := NewProfileStore(profilePath)
	if err != nil {
		t.Fatalf("NewProfileStore() after shutdown error = %v", err)
	}
	if got := loaded.List(); len(got) != 1 || got[0].Name != "shutdown" {
		t.Fatalf("persisted profiles = %#v", got)
	}
}

func TestServiceConcurrentObserversAndSettings(t *testing.T) {
	service, _ := newTestService(t, nil)
	var wait sync.WaitGroup
	for worker := 0; worker < 4; worker++ {
		wait.Add(1)
		go func(worker int) {
			defer wait.Done()
			for index := 0; index < 50; index++ {
				settings := DefaultSettings()
				if worker%2 == 0 {
					settings.LastProfile = "parallel"
				}
				if err := service.UpdateSettings(settings); err != nil {
					t.Errorf("UpdateSettings() error = %v", err)
				}
				_ = service.Settings()
				_ = service.State()
				_ = service.Logs("info")
				service.Logger().Info("parallel")
			}
		}(worker)
	}
	wait.Wait()
}

type testRunner struct {
	mu       sync.Mutex
	startErr error
	starts   int
	stops    int
}

type testTunnelManager struct {
	enables   int
	disables  int
	blockIPv6 bool
	onLost    func(error)
}

func (manager *testTunnelManager) Enable(_ context.Context, _ config.Profile, _, _ string, blockIPv6 bool) error {
	manager.enables++
	manager.blockIPv6 = blockIPv6
	return nil
}
func (manager *testTunnelManager) Disable() error {
	manager.disables++
	return nil
}
func (manager *testTunnelManager) SetOnTunnelLost(callback func(error)) {
	manager.onLost = callback
}

func (runner *testRunner) Start(context.Context) error {
	runner.mu.Lock()
	defer runner.mu.Unlock()
	runner.starts++
	return runner.startErr
}

func (runner *testRunner) Stop() error {
	runner.mu.Lock()
	defer runner.mu.Unlock()
	runner.stops++
	return nil
}

var _ core.Runner = (*testRunner)(nil)

func newTestService(t *testing.T, startErr error) (*Service, *[]*testRunner) {
	t.Helper()
	return newTestServiceWithLogger(t, startErr, slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug})))
}

func newTestServiceWithLogger(t *testing.T, startErr error, logger *slog.Logger) (*Service, *[]*testRunner) {
	t.Helper()
	profiles, err := NewProfileStore(filepath.Join(t.TempDir(), "profiles.json"))
	if err != nil {
		t.Fatalf("NewProfileStore() error = %v", err)
	}
	if err := profiles.Add(testProfile("service-test")); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	runners := make([]*testRunner, 0, 4)
	factory := func([]byte) (core.Runner, error) {
		runner := &testRunner{startErr: startErr}
		runners = append(runners, runner)
		return runner, nil
	}
	service, err := NewService(ServiceOptions{
		Profiles: profiles, Settings: DefaultSettings(), SettingsPath: filepath.Join(t.TempDir(), "settings.json"),
		RunnerFactory: factory, Logger: logger,
	})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	return service, &runners
}
