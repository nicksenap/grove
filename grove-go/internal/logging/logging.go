// Package logging provides file-based logging with rotation.
package logging

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"github.com/nicksenap/grove/internal/config"
	"gopkg.in/natefinch/lumberjack.v2"
)

var (
	once    sync.Once
	verbose bool
)

// Setup initializes the global logger with file rotation.
// Safe to call multiple times — only the first call takes effect.
func Setup(v bool) {
	once.Do(func() {
		verbose = v
		logPath := filepath.Join(config.GroveDir(), "grove.log")
		os.MkdirAll(filepath.Dir(logPath), 0o755)

		writer := &lumberjack.Logger{
			Filename:   logPath,
			MaxSize:    1, // 1 MB
			MaxBackups: 3,
		}

		level := slog.LevelInfo
		if verbose {
			level = slog.LevelDebug
		}

		var w io.Writer = writer
		if verbose {
			w = io.MultiWriter(writer, os.Stderr)
		}

		handler := slog.NewTextHandler(w, &slog.HandlerOptions{
			Level: level,
		})
		slog.SetDefault(slog.New(handler))
	})
}

// Logger returns a logger with the given name as a group.
func Logger(name string) *slog.Logger {
	return slog.Default().With("pkg", name)
}
