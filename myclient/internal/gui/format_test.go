package gui

import (
	"strings"
	"testing"
	"time"

	"example.com/vclient/internal/app"
)

func TestMaskUUID(t *testing.T) {
	got := MaskUUID("uuid=01234567-89ab-cdef-0123-456789abcdef")
	if got != "uuid=0123****cdef" {
		t.Fatalf("MaskUUID() = %q, want uuid=0123****cdef", got)
	}
}

func TestMaskSensitiveLog(t *testing.T) {
	got := MaskSensitiveLog("uuid=01234567-89ab-cdef-0123-456789abcdef pbk=ABCD123 sid=deadbeef")
	for _, secret := range []string{"01234567-89ab-cdef-0123-456789abcdef", "ABCD123", "deadbeef"} {
		if strings.Contains(got, secret) {
			t.Errorf("MaskSensitiveLog() leaked %q in %q", secret, got)
		}
	}
}

func TestFormatUptime(t *testing.T) {
	now := time.Date(2026, time.January, 1, 12, 34, 56, 0, time.UTC)
	tests := []struct {
		name      string
		startedAt time.Time
		want      string
	}{
		{name: "duration", startedAt: now.Add(-time.Hour - 2*time.Minute - 3*time.Second), want: "01:02:03"},
		{name: "zero", want: "00:00:00"},
		{name: "future", startedAt: now.Add(time.Second), want: "00:00:00"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := FormatUptime(test.startedAt, now); got != test.want {
				t.Errorf("FormatUptime() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestFilterLogsByLevelAndText(t *testing.T) {
	entries := []app.LogEntry{
		{Level: "DEBUG", Message: "debug connection"},
		{Level: "INFO", Message: "proxy connected"},
		{Level: "ERROR", Message: "proxy failed"},
	}
	got := FilterLogs(entries, "warn", "proxy")
	if len(got) != 1 || got[0].Message != "proxy failed" {
		t.Fatalf("FilterLogs() = %#v, want matching error", got)
	}
}
