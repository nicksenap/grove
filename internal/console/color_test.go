package console

import (
	"os"
	"strings"
	"testing"
)

func TestColorEnabledRequiresTTYAndNoColorUnset(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	if !colorEnabled(true) {
		t.Fatal("expected color for a TTY when NO_COLOR is unset")
	}
	if colorEnabled(false) {
		t.Fatal("expected no color for a non-TTY")
	}

	t.Setenv("NO_COLOR", "1")
	if colorEnabled(true) {
		t.Fatal("expected NO_COLOR to disable color for a TTY")
	}
}

func TestIsTerminalRejectsNonTerminalCharacterDevice(t *testing.T) {
	devNull, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer devNull.Close()
	if IsTerminal(devNull) {
		t.Fatal("character devices such as /dev/null are not necessarily terminals")
	}
}

func TestWarningOmitsANSIWhenStderrIsPipe(t *testing.T) {
	stderr := withStdin(t, "", func() {
		Warning("careful")
	})
	if strings.ContainsRune(stderr, '\x1b') {
		t.Fatalf("unexpected ANSI in non-TTY output: %q", stderr)
	}
	if stderr != "warn: careful\n" {
		t.Fatalf("stderr = %q", stderr)
	}
}
