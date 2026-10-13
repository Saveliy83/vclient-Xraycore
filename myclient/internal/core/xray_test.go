package core

import (
	"context"
	"errors"
	"testing"
)

func TestNewRunnerRejectsEmptyConfig(t *testing.T) {
	if _, err := NewRunner(nil); !errors.Is(err, ErrEmptyConfig) {
		t.Fatalf("NewRunner() error = %v, want ErrEmptyConfig", err)
	}
}

func TestStartRejectsInvalidConfig(t *testing.T) {
	runner, err := NewRunner([]byte("{"))
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	if err := runner.Start(context.Background()); err == nil {
		t.Fatal("Start() error = nil, want JSON config error")
	}
}

func TestSetConfigRequiresStoppedRunner(t *testing.T) {
	runner, err := NewRunner([]byte(`{}`))
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	if err := runner.SetConfig([]byte("new config")); err != nil {
		t.Fatalf("SetConfig() error = %v", err)
	}
	if err := runner.SetConfig(nil); !errors.Is(err, ErrEmptyConfig) {
		t.Fatalf("SetConfig() error = %v, want ErrEmptyConfig", err)
	}
}
