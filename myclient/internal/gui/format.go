package gui

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"example.com/vclient/internal/app"
)

var (
	uuidPattern   = regexp.MustCompile(`(?i)\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b`)
	secretPattern = regexp.MustCompile(`(?i)\b(pbk|sid|public_key|short_id)\s*[:=]\s*([^\s,;]+)`)
)

func MaskUUID(value string) string {
	return uuidPattern.ReplaceAllStringFunc(value, func(uuid string) string {
		compact := strings.ReplaceAll(uuid, "-", "")
		return compact[:4] + "****" + compact[len(compact)-4:]
	})
}

func MaskSensitiveLog(value string) string {
	value = uuidPattern.ReplaceAllStringFunc(value, func(uuid string) string {
		compact := strings.ReplaceAll(uuid, "-", "")
		return compact[:4] + "****" + compact[len(compact)-4:]
	})
	return secretPattern.ReplaceAllString(value, "$1=****")
}

func FormatUptime(startedAt, now time.Time) string {
	if startedAt.IsZero() || now.Before(startedAt) {
		return "00:00:00"
	}
	elapsed := now.Sub(startedAt).Truncate(time.Second)
	hours := int(elapsed.Hours())
	minutes := int(elapsed.Minutes()) % 60
	seconds := int(elapsed.Seconds()) % 60
	return fmt.Sprintf("%02d:%02d:%02d", hours, minutes, seconds)
}

func FilterLogs(entries []app.LogEntry, minimumLevel, search string) []app.LogEntry {
	minimum := logLevelValue(minimumLevel)
	needle := strings.ToLower(strings.TrimSpace(search))
	filtered := make([]app.LogEntry, 0, len(entries))
	for _, entry := range entries {
		if logLevelValue(entry.Level) < minimum {
			continue
		}
		if needle != "" && !strings.Contains(strings.ToLower(entry.Message), needle) {
			continue
		}
		entry.Message = MaskSensitiveLog(entry.Message)
		filtered = append(filtered, entry)
	}
	return filtered
}

func logLevelValue(level string) int {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		return 0
	case "info":
		return 1
	case "warn", "warning":
		return 2
	case "error":
		return 3
	default:
		return -1
	}
}
