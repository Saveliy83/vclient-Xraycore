package logger

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
)

var errUnsupportedLevel = errors.New("unsupported log level")

type closerFunc func() error

func (closeFn closerFunc) Close() error {
	return closeFn()
}

func ParseLevel(level string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("parse log level %q: %w", level, errUnsupportedLevel)
	}
}

func New(level, file string) (*slog.Logger, io.Closer, error) {
	minLevel, err := ParseLevel(level)
	if err != nil {
		return nil, nil, fmt.Errorf("create logger: %w", err)
	}

	writers := []io.Writer{os.Stdout}
	closeFn := closerFunc(func() error { return nil })
	if file != "" {
		logFile, err := os.OpenFile(file, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600) // #nosec G304 -- path is explicitly supplied by the local user.
		if err != nil {
			return nil, nil, fmt.Errorf("open log file %q: %w", file, err)
		}
		writers = append(writers, logFile)
		closeFn = logFile.Close
	}

	handler := slog.NewTextHandler(io.MultiWriter(writers...), &slog.HandlerOptions{Level: minLevel})
	return slog.New(handler), closeFn, nil
}
