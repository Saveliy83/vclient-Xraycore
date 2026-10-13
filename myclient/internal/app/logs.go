package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
)

const maxLogEntries = 2000

type LogEntry struct {
	Time    time.Time
	Level   string
	Message string
}

type logBuffer struct {
	mu          sync.RWMutex
	entries     []LogEntry
	start       int
	subscribers map[uint64]func(LogEntry)
	nextID      uint64
}

func newLogBuffer() *logBuffer {
	return &logBuffer{subscribers: make(map[uint64]func(LogEntry))}
}

func (buffer *logBuffer) append(entry LogEntry) {
	buffer.mu.Lock()
	if len(buffer.entries) < maxLogEntries {
		buffer.entries = append(buffer.entries, entry)
	} else {
		buffer.entries[buffer.start] = entry
		buffer.start = (buffer.start + 1) % maxLogEntries
	}
	subscribers := make([]func(LogEntry), 0, len(buffer.subscribers))
	for _, subscriber := range buffer.subscribers {
		subscribers = append(subscribers, subscriber)
	}
	buffer.mu.Unlock()
	for _, subscriber := range subscribers {
		subscriber(entry)
	}
}

func (buffer *logBuffer) list(minLevel slog.Level) []LogEntry {
	buffer.mu.RLock()
	defer buffer.mu.RUnlock()
	result := make([]LogEntry, 0, len(buffer.entries))
	for offset := 0; offset < len(buffer.entries); offset++ {
		index := (buffer.start + offset) % len(buffer.entries)
		entry := buffer.entries[index]
		level, err := parseLogLevel(entry.Level)
		if err == nil && level >= minLevel {
			result = append(result, entry)
		}
	}
	return result
}

func (buffer *logBuffer) clear() {
	buffer.mu.Lock()
	buffer.entries = nil
	buffer.start = 0
	buffer.mu.Unlock()
}

func (buffer *logBuffer) subscribe(subscriber func(LogEntry)) func() {
	if subscriber == nil {
		return func() {}
	}
	buffer.mu.Lock()
	id := buffer.nextID
	buffer.nextID++
	buffer.subscribers[id] = subscriber
	buffer.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			buffer.mu.Lock()
			delete(buffer.subscribers, id)
			buffer.mu.Unlock()
		})
	}
}

type teeHandler struct {
	next   slog.Handler
	buffer *logBuffer
	attrs  []slog.Attr
	groups []string
}

func (handler *teeHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return true
}

func (handler *teeHandler) Handle(ctx context.Context, record slog.Record) error {
	var writeErr error
	if handler.next.Enabled(ctx, record.Level) {
		if err := handler.next.Handle(ctx, record); err != nil {
			writeErr = fmt.Errorf("write application log: %w", err)
		}
	}
	message := strings.TrimSpace(record.Message)
	if len(handler.groups) > 0 {
		message = strings.Join(handler.groups, ".") + ": " + message
	}
	handler.buffer.append(LogEntry{
		Time:    record.Time,
		Level:   record.Level.String(),
		Message: message,
	})
	return writeErr
}

func (handler *teeHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	cloned := *handler
	cloned.next = handler.next.WithAttrs(attrs)
	cloned.attrs = append(append([]slog.Attr(nil), handler.attrs...), attrs...)
	return &cloned
}

func (handler *teeHandler) WithGroup(name string) slog.Handler {
	cloned := *handler
	cloned.next = handler.next.WithGroup(name)
	cloned.groups = append(append([]string(nil), handler.groups...), name)
	return &cloned
}

func parseLogLevel(value string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("parse log level %q: %w", value, errors.New("unsupported log level"))
	}
}
