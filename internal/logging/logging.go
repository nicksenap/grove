// Package logging provides rotating file logging for Grove.
// Log file: ~/.grove/grove.log, 1MB max, 3 backups.
//
// The log file is always written (Info, Warn, Error) to act as a flight
// recorder for debugging.  Debug messages are only written when verbose
// mode is enabled via --verbose.
package logging

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	maxSize    = 1 * 1024 * 1024 // 1 MB
	maxBackups = 3
)

// LogDir is the directory for log files. Override in tests.
var LogDir string

var (
	mu       sync.Mutex
	logFile  *os.File
	verbose  bool
	initDone bool
)

func init() {
	home, _ := os.UserHomeDir()
	LogDir = filepath.Join(home, ".grove")
}

// Setup initializes logging. Call once at startup.
// The log file is always opened; verbose controls whether Debug messages are written.
func Setup(v bool) {
	mu.Lock()
	defer mu.Unlock()
	verbose = v
	initDone = true

	os.MkdirAll(LogDir, 0o755)
	path := filepath.Join(LogDir, "grove.log")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	logFile = f
}

// Debug logs a debug message (only when verbose).
func Debug(format string, args ...interface{}) {
	if !verbose || logFile == nil {
		return
	}
	write("DEBUG", format, args...)
}

// Info logs an info message.
func Info(format string, args ...interface{}) {
	if logFile == nil {
		return
	}
	write("INFO", format, args...)
}

// Warn logs a warning message.
func Warn(format string, args ...interface{}) {
	if logFile == nil {
		return
	}
	write("WARN", format, args...)
}

// Error logs an error message.
func Error(format string, args ...interface{}) {
	if logFile == nil {
		return
	}
	write("ERROR", format, args...)
}

func write(level, format string, args ...interface{}) {
	mu.Lock()
	defer mu.Unlock()

	if logFile == nil {
		return
	}

	msg := fmt.Sprintf(format, args...)
	ts := time.Now().Format("2006-01-02 15:04:05")
	line := fmt.Sprintf("%s %s - %s\n", ts, level, msg)

	logFile.WriteString(line)

	// Check rotation
	info, err := logFile.Stat()
	if err == nil && info.Size() > maxSize {
		rotate()
	}
}

func rotate() {
	if logFile != nil {
		logFile.Close()
		logFile = nil
	}

	base := filepath.Join(LogDir, "grove.log")

	// Shift backups: .3 -> delete, .2 -> .3, .1 -> .2, base -> .1
	for i := maxBackups; i >= 1; i-- {
		src := fmt.Sprintf("%s.%d", base, i)
		if i == maxBackups {
			os.Remove(src)
		} else {
			dst := fmt.Sprintf("%s.%d", base, i+1)
			os.Rename(src, dst)
		}
	}
	os.Rename(base, base+".1")

	f, err := os.OpenFile(base, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	logFile = f
}
