package console

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// ANSI color codes
const (
	reset      = "\033[0m"
	dim        = "\033[2m"
	boldRed    = "\033[1;31m"
	boldGreen  = "\033[1;32m"
	boldYellow = "\033[1;33m"
)

// Error prints an error message to stderr.
func Error(msg string) {
	fmt.Fprintf(os.Stderr, "%serror:%s %s\n", boldRed, reset, msg)
}

// Errorf prints a formatted error message to stderr.
func Errorf(format string, args ...any) {
	Error(fmt.Sprintf(format, args...))
}

// Success prints a success message to stderr.
func Success(msg string) {
	fmt.Fprintf(os.Stderr, "%sok:%s %s\n", boldGreen, reset, msg)
}

// Successf prints a formatted success message to stderr.
func Successf(format string, args ...any) {
	Success(fmt.Sprintf(format, args...))
}

// Info prints an info message to stderr.
func Info(msg string) {
	fmt.Fprintf(os.Stderr, "%s%s%s\n", dim, msg, reset)
}

// Infof prints a formatted info message to stderr.
func Infof(format string, args ...any) {
	Info(fmt.Sprintf(format, args...))
}

// Warning prints a warning message to stderr.
func Warning(msg string) {
	fmt.Fprintf(os.Stderr, "%swarn:%s %s\n", boldYellow, reset, msg)
}

// Warningf prints a formatted warning message to stderr.
func Warningf(format string, args ...any) {
	Warning(fmt.Sprintf(format, args...))
}

// Confirm asks the user a yes/no question. Returns true for yes.
// Defaults to defaultYes if the user just presses enter.
func Confirm(prompt string, defaultYes bool) bool {
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
	fmt.Fprintf(os.Stderr, "%s: ", label)
	reader := bufio.NewReader(os.Stdin)
	input, _ := reader.ReadString('\n')
	return strings.TrimSpace(input)
}

// IsTerminal returns true if the given file is a terminal.
func IsTerminal(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}
