package main

import (
	"log/slog"
	"os"

	"github.com/kstaniek/go-ampio-server/internal/logging"
)

func setupLogger(format, level string) *slog.Logger {
	var lvl slog.Level
	switch level {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	l := logging.New(format, lvl, os.Stderr).With("app", "can-server")
	logging.Set(l)
	return l
}
