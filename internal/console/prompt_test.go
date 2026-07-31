package console

import (
	"bytes"
	"io"
	"os"
	"testing"
)

func withStdin(t *testing.T, input string, fn func()) (stderr string) {
	t.Helper()

	rIn, wIn, err := os.Pipe()
	if err != nil {
		t.Fatalf("stdin pipe: %v", err)
	}
	rErr, wErr, err := os.Pipe()
	if err != nil {
		t.Fatalf("stderr pipe: %v", err)
	}

	oldIn, oldErr := os.Stdin, os.Stderr
	os.Stdin, os.Stderr = rIn, wErr
	defer func() {
		os.Stdin, os.Stderr = oldIn, oldErr
		rIn.Close()
		rErr.Close()
	}()

	go func() {
		_, _ = io.WriteString(wIn, input)
		wIn.Close()
	}()

	fn()

	wErr.Close()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, rErr)
	return buf.String()
}

func TestPromptDefault_EmptyAcceptsDefault(t *testing.T) {
	var got string
	stderr := withStdin(t, "\n", func() {
		got = PromptDefault("Branch name", "linear-2026-07-30")
	})
	if got != "linear-2026-07-30" {
		t.Errorf("got %q, want default", got)
	}
	if want := "Branch name [linear-2026-07-30]: "; stderr != want {
		t.Errorf("prompt = %q, want %q", stderr, want)
	}
}

func TestPromptDefault_Override(t *testing.T) {
	var got string
	_ = withStdin(t, "feat/other\n", func() {
		got = PromptDefault("Branch name", "linear-2026-07-30")
	})
	if got != "feat/other" {
		t.Errorf("got %q, want override", got)
	}
}

func TestPromptDefault_NoDefault(t *testing.T) {
	var got string
	stderr := withStdin(t, "feat/login\n", func() {
		got = PromptDefault("Branch name", "")
	})
	if got != "feat/login" {
		t.Errorf("got %q, want typed value", got)
	}
	if want := "Branch name: "; stderr != want {
		t.Errorf("prompt = %q, want %q", stderr, want)
	}
}

func TestPromptDefault_EmptyNoDefault(t *testing.T) {
	var got string
	_ = withStdin(t, "\n", func() {
		got = PromptDefault("Branch name", "")
	})
	if got != "" {
		t.Errorf("got %q, want empty", got)
	}
}
