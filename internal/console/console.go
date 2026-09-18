package console

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"
)

// ANSI color codes
const (
	reset      = "\033[0m"
	dim        = "\033[2m"
	boldRed    = "\033[1;31m"
	boldGreen  = "\033[1;32m"
	boldYellow = "\033[1;33m"
)

func colorEnabled(terminal bool) bool {
	return terminal && os.Getenv("NO_COLOR") == ""
}

// ColorEnabled reports whether ANSI styling should be emitted to f.
func ColorEnabled(f *os.File) bool {
	return colorEnabled(IsTerminal(f))
}

func styled(code, text string, f *os.File) string {
	if !ColorEnabled(f) {
		return text
	}
	return code + text + reset
}

// Error prints an error message to stderr.
func Error(msg string) {
	fmt.Fprintf(os.Stderr, "%s %s\n", styled(boldRed, "error:", os.Stderr), msg)
}

// Errorf prints a formatted error message to stderr.
func Errorf(format string, args ...any) {
	Error(fmt.Sprintf(format, args...))
}

// Success prints a success message to stderr.
func Success(msg string) {
	fmt.Fprintf(os.Stderr, "%s %s\n", styled(boldGreen, "ok:", os.Stderr), msg)
}

// Successf prints a formatted success message to stderr.
func Successf(format string, args ...any) {
	Success(fmt.Sprintf(format, args...))
}

// Info prints an info message to stderr.
func Info(msg string) {
	fmt.Fprintln(os.Stderr, styled(dim, msg, os.Stderr))
}

// Infof prints a formatted info message to stderr.
func Infof(format string, args ...any) {
	Info(fmt.Sprintf(format, args...))
}

// Warning prints a warning message to stderr.
func Warning(msg string) {
	fmt.Fprintf(os.Stderr, "%s %s\n", styled(boldYellow, "warn:", os.Stderr), msg)
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
	return PromptDefault(label, "")
}

// PromptDefault asks the user for text input, showing defaultValue in brackets
// when non-empty. Empty input (just Enter) returns defaultValue.
func PromptDefault(label, defaultValue string) string {
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
	return term.IsTerminal(int(f.Fd()))
}
