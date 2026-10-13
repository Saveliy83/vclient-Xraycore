package tunmode

import (
	"errors"
	"testing"
)

func TestHealthFailureThresholdAndSuccessReset(t *testing.T) {
	var tracker healthFailureTracker
	failure := errors.New("health check failed")
	if tracker.record(failure) {
		t.Fatal("one failure disabled tunnel")
	}
	if tracker.record(failure) {
		t.Fatal("two consecutive failures disabled tunnel")
	}
	if tracker.record(nil) {
		t.Fatal("successful health check disabled tunnel")
	}
	if tracker.failures != 0 {
		t.Fatalf("failure count after success = %d, want 0", tracker.failures)
	}
	if tracker.record(failure) || tracker.record(failure) {
		t.Fatal("two failures after success disabled tunnel")
	}
	if !tracker.record(failure) {
		t.Fatal("third consecutive failure did not disable tunnel")
	}
}
