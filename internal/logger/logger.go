// Package logger initialises the process-wide slog logger; all output goes
// through it (fmt/log printing is forbidden by lint).
package logger

import (
	"log/slog"
	"os"
)

var _ slog.Level = slog.LevelDebug

// Init initialises the global slog logger
func Init(lvl slog.Level) {
	opts := &slog.HandlerOptions{
		AddSource: true,
		Level:     lvl,
	}

	h := slog.NewJSONHandler(os.Stdout, opts)
	slog.SetDefault(slog.New(h))
}
