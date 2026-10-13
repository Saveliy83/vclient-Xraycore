package reload

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

type runnerMock struct {
	restarts int
	stops    int
}

func (*runnerMock) Start(context.Context) error { return nil }

func (runner *runnerMock) Stop() error {
	runner.stops++
	return nil
}

func (runner *runnerMock) Restart(context.Context, []byte) error {
	runner.restarts++
	return nil
}

func TestHTTPReloadRequiresToken(t *testing.T) {
	calls := 0
	handler := NewHTTPHandler("secret", func(context.Context) error {
		calls++
		return nil
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	request, err := http.NewRequest(http.MethodPost, server.URL+"/reload", nil)
	if err != nil {
		t.Fatalf("create request without token: %v", err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("send request without token: %v", err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("without token status = %d, want %d", response.StatusCode, http.StatusUnauthorized)
	}

	request, err = http.NewRequest(http.MethodPost, server.URL+"/reload", nil)
	if err != nil {
		t.Fatalf("create request with token: %v", err)
	}
	request.Header.Set("X-Reload-Token", "secret")
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("send request with token: %v", err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("with token status = %d, want %d", response.StatusCode, http.StatusOK)
	}
	if calls != 1 {
		t.Fatalf("reload calls = %d, want 1", calls)
	}
}

func TestInvalidReloadDoesNotRestartRunner(t *testing.T) {
	runner := &runnerMock{}
	controller, err := NewController(
		runner,
		func(context.Context) ([]byte, error) { return []byte("invalid"), nil },
		func([]byte) error { return errors.New("invalid config") },
	)
	if err != nil {
		t.Fatalf("NewController() error = %v", err)
	}
	if err := controller.Reload(context.Background()); err == nil {
		t.Fatal("Reload() error = nil, want invalid config error")
	}
	if runner.restarts != 0 {
		t.Fatalf("runner restarts = %d, want 0", runner.restarts)
	}
	if runner.stops != 0 {
		t.Fatalf("runner stops = %d, want 0", runner.stops)
	}
}
