// Package logger builds the application's structured logger (zerolog).
// In local mode it prints human-friendly console output; otherwise JSON.
package logger

import (
	"os"
	"time"

	"github.com/rs/zerolog"
)

// New returns a configured zerolog.Logger.
// level: debug|info|warn|error. pretty: use console writer (local dev).
func New(level string, pretty bool) zerolog.Logger {
	lvl, err := zerolog.ParseLevel(level)
	if err != nil || level == "" {
		lvl = zerolog.InfoLevel
	}
	zerolog.SetGlobalLevel(lvl)
	zerolog.TimeFieldFormat = time.RFC3339

	if pretty {
		w := zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: "15:04:05"}
		return zerolog.New(w).With().Timestamp().Logger()
	}
	return zerolog.New(os.Stdout).With().Timestamp().Logger()
}
