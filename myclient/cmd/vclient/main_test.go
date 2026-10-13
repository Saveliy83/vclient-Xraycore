package main

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"reflect"
	"testing"
	"time"

	"example.com/vclient/internal/config"
)

type closeRecorder struct {
	called bool
}

func (recorder *closeRecorder) Close() error {
	recorder.called = true
	return nil
}

func TestParseFlags(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		want    options
		wantErr bool
	}{
		{
			name: "defaults",
			want: options{
				listenSOCKS: "127.0.0.1:1080",
				listenHTTP:  "127.0.0.1:1081",
				logLevel:    "info",
				subInterval: 6 * time.Hour,
			},
		},
		{
			name: "all options",
			args: []string{
				"-config", "client.json",
				"-listen-socks", "127.0.0.1:2080",
				"-listen-http", "127.0.0.1:2081",
				"-log-level", "debug",
				"-log-file", "client.log",
				"-rules", "rules.json",
				"-dns", "dns.json",
				"-reload-token", "local-secret",
				"-system-proxy",
				"-allow-experimental",
			},
			want: options{
				configPath:        "client.json",
				listenSOCKS:       "127.0.0.1:2080",
				listenHTTP:        "127.0.0.1:2081",
				logLevel:          "debug",
				logFile:           "client.log",
				rulesPath:         "rules.json",
				dnsPath:           "dns.json",
				subInterval:       6 * time.Hour,
				reloadToken:       "local-secret",
				systemProxy:       true,
				allowExperimental: true,
			},
		},
		{
			name: "link and version",
			args: []string{"-link", "vless://example", "-version"},
			want: options{
				link:        "vless://example",
				listenSOCKS: "127.0.0.1:1080",
				listenHTTP:  "127.0.0.1:1081",
				logLevel:    "info",
				subInterval: 6 * time.Hour,
				showVersion: true,
			},
		},
		{
			name: "subscription options",
			args: []string{
				"-sub-url", "https://subscriptions.example/list",
				"-sub-interval", "6h",
				"-profile", "travel",
			},
			want: options{
				listenSOCKS: "127.0.0.1:1080",
				listenHTTP:  "127.0.0.1:1081",
				logLevel:    "info",
				subURL:      "https://subscriptions.example/list",
				subInterval: 6 * time.Hour,
				profileName: "travel",
			},
		},
		{
			name:    "config and link conflict",
			args:    []string{"-config", "client.json", "-link", "vless://example"},
			wantErr: true,
		},
		{
			name:    "invalid log level",
			args:    []string{"-log-level", "verbose"},
			wantErr: true,
		},
		{
			name:    "unknown flag",
			args:    []string{"-unknown"},
			wantErr: true,
		},
		{
			name:    "profile without subscription",
			args:    []string{"-profile", "travel"},
			wantErr: true,
		},
		{
			name:    "non-positive subscription interval",
			args:    []string{"-sub-url", "https://subscriptions.example/list", "-sub-interval", "0s"},
			wantErr: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := parseFlags(test.args)
			if (err != nil) != test.wantErr {
				t.Fatalf("parseFlags() error = %v, wantErr %t", err, test.wantErr)
			}
			if err == nil && !reflect.DeepEqual(got, test.want) {
				t.Errorf("parseFlags() = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestWaitForShutdownClosesLogger(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	closer := &closeRecorder{}
	appLogger := slog.New(slog.NewTextHandler(io.Discard, nil))

	if err := waitForShutdown(ctx, appLogger, shutdownFunc(closer.Close)); err != nil {
		t.Fatalf("waitForShutdown() error = %v", err)
	}
	if !closer.called {
		t.Fatal("waitForShutdown() did not close the logger")
	}
}

func TestStartupLogDoesNotIncludeUUID(t *testing.T) {
	var output bytes.Buffer
	appLogger := slog.New(slog.NewTextHandler(&output, nil))
	profile := config.Profile{
		Network: "ws",
		Address: "proxy.example.com",
		Port:    443,
		UUID:    "123e4567-e89b-12d3-a456-426614174000",
	}

	logProfileStarted(appLogger, profile, options{
		listenSOCKS: "127.0.0.1:1080",
		listenHTTP:  "127.0.0.1:1081",
	})

	logged := output.String()
	if bytes.Contains(output.Bytes(), []byte(profile.UUID)) {
		t.Fatalf("startup log leaked UUID: %s", logged)
	}
	if !bytes.Contains(output.Bytes(), []byte("transport=ws")) ||
		!bytes.Contains(output.Bytes(), []byte("server=proxy.example.com:443")) {
		t.Errorf("startup log lacks transport or server address: %s", logged)
	}
}

func TestExperimentalKCPLogsWarningOnlyWhenEnabled(t *testing.T) {
	profile := config.Profile{Network: "kcp"}
	for _, test := range []struct {
		name              string
		allowExperimental bool
		wantWarning       bool
	}{
		{name: "enabled", allowExperimental: true, wantWarning: true},
		{name: "disabled"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			appLogger := slog.New(slog.NewTextHandler(&output, nil))
			logExperimentalTransport(appLogger, profile, options{allowExperimental: test.allowExperimental})
			if got := bytes.Contains(output.Bytes(), []byte("experimental transport enabled")); got != test.wantWarning {
				t.Errorf("warning present = %t, want %t; log = %s", got, test.wantWarning, output.String())
			}
		})
	}
}
