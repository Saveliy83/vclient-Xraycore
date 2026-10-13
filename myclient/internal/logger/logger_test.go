package logger

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewFiltersLevelAndCreatesFile(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "client.log")
	logger, closer, err := New("warn", logPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	logger.Debug("debug message")
	logger.Info("info message")
	logger.Warn("warning message")
	if err := closer.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	data, err := os.ReadFile(logPath) // #nosec G304 -- path is generated under t.TempDir.
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}
	content := string(data)
	if strings.Contains(content, "debug message") || strings.Contains(content, "info message") {
		t.Fatalf("log level did not filter lower-priority messages: %s", content)
	}
	if !strings.Contains(content, "warning message") {
		t.Fatalf("warning message not found in log file: %s", content)
	}
}

func TestNewRejectsInvalidLevel(t *testing.T) {
	if _, _, err := New("verbose", ""); err == nil {
		t.Fatal("New() error = nil, want invalid log-level error")
	}
}
