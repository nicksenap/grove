package console

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/nicksenap/grove/internal/machine"
)

// ANSI color codes
const (
	reset      = "\033[0m"
	dim        = "\033[2m"
	boldRed    = "\033[1;31m"
	boldGreen  = "\033[1;32m"
	boldYellow = "\033[1;33m"
)

// NoColor strips ANSI escapes from every helper in this package. Machine mode
// sets it so stderr diagnostics stay plain text for log scrapers, and it is also
// the hook a future NO_COLOR/non-TTY check would use.
var NoColor bool

// paint returns code unless colors are disabled.
func paint(code string) string {
	if NoColor {
		return ""
	}
	return code
}

// Error prints an error message to stderr.
func Error(msg string) {
	fmt.Fprintf(os.Stderr, "%serror:%s %s\n", paint(boldRed), paint(reset), msg)
}

// Errorf prints a formatted error message to stderr.
func Errorf(format string, args ...any) {
	Error(fmt.Sprintf(format, args...))
}

// Success prints a success message to stderr.
func Success(msg string) {
	fmt.Fprintf(os.Stderr, "%sok:%s %s\n", paint(boldGreen), paint(reset), msg)
}

// Successf prints a formatted success message to stderr.
func Successf(format string, args ...any) {
	Success(fmt.Sprintf(format, args...))
}

// Info prints an info message to stderr.
func Info(msg string) {
	fmt.Fprintf(os.Stderr, "%s%s%s\n", paint(dim), msg, paint(reset))
}

// Infof prints a formatted info message to stderr.
func Infof(format string, args ...any) {
	Info(fmt.Sprintf(format, args...))
}

// Warning prints a warning message to stderr and records it for the machine
// envelope, so a degraded-but-successful run is visible to agents too.
func Warning(msg string) {
	machine.Warn(msg)
	fmt.Fprintf(os.Stderr, "%swarn:%s %s\n", paint(boldYellow), paint(reset), msg)
}

// Warningf prints a formatted warning message to stderr.
func Warningf(format string, args ...any) {
	Warning(fmt.Sprintf(format, args...))
}

// Confirm asks the user a yes/no question. Returns true for yes.
// Defaults to defaultYes if the user just presses enter.
//
// In machine mode it never reads stdin — a command promised to be
// non-interactive must not hang waiting for a human. It answers with the
// default, which is the conservative choice for destructive prompts. Commands
// that need a real decision should require an explicit flag instead of relying
// on this fallback.
func Confirm(prompt string, defaultYes bool) bool {
	if machine.Enabled() {
		machine.Warn(fmt.Sprintf("skipped prompt %q in machine mode, assuming %v", prompt, defaultYes))
		return defaultYes
	}

	hint := "[y/N]"
	if defaultYes {
		hint = "[Y/n]"
	}
	fmt.Fprintf(os.Stderr, "%s %s ", prompt, hint)

	reader := bufio.NewReader(os.Stdin)
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(strings.ToLower(input))

	if input == "" {
		return defaultYes
	}
	return input == "y" || input == "yes"
}

// Prompt asks the user for text input.
func Prompt(label string) string {
	return PromptDefault(label, "")
}

// PromptDefault asks the user for text input, showing defaultValue in brackets
// when non-empty. Empty input (just Enter) returns defaultValue.
func PromptDefault(label, defaultValue string) string {
	if machine.Enabled() {
		machine.Warn(fmt.Sprintf("skipped prompt %q in machine mode, using %q", label, defaultValue))
		return defaultValue
	}
	if defaultValue != "" {
		fmt.Fprintf(os.Stderr, "%s [%s]: ", label, defaultValue)
	} else {
		fmt.Fprintf(os.Stderr, "%s: ", label)
	}
	reader := bufio.NewReader(os.Stdin)
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)
	if input == "" {
		return defaultValue
	}
	return input
}

// IsTerminal returns true if the given file is a terminal.
func IsTerminal(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}
