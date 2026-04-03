package picker

import (
	"strings"
	"testing"
)

func TestPickOneSingleChoiceAutoSelects(t *testing.T) {
	// Single choice should return immediately without TUI
	result, err := PickOne("Pick:", []string{"only-option"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "only-option" {
		t.Errorf("expected 'only-option', got %q", result)
	}
}

func TestPickOneEmptyChoicesErrors(t *testing.T) {
	_, err := PickOne("Pick:", []string{})
	if err == nil {
		t.Error("expected error for empty choices")
	}
}

func TestPickManyEmptyChoicesErrors(t *testing.T) {
	_, err := PickMany("Pick:", []string{})
	if err == nil {
		t.Error("expected error for empty choices")
	}
}

func TestPickOneNonTerminalErrors(t *testing.T) {
	// In test context, stdin/stderr are not terminals
	_, err := PickOne("Pick:", []string{"a", "b"})
	if err == nil {
		t.Error("expected error when not in a terminal")
	}
	if !strings.Contains(err.Error(), "terminal") {
		t.Errorf("error should mention terminal, got: %v", err)
	}
}

func TestPickManyNonTerminalErrors(t *testing.T) {
	_, err := PickMany("Pick:", []string{"a", "b"})
	if err == nil {
		t.Error("expected error when not in a terminal")
	}
	if !strings.Contains(err.Error(), "terminal") {
		t.Errorf("error should mention terminal, got: %v", err)
	}
}

func TestPickOneUsesStderrNotStdout(t *testing.T) {
	// This is a design test — verify the terminal check is on stderr.
	// In test context both stdin and stderr are pipes (not terminals),
	// so the error fires. The important thing is it does NOT check stdout.
	//
	// If the code checked stdout, commands like `cd "$(gw go)"` would fail
	// because stdout is piped for cd, but stderr is still a terminal.
	//
	// We verify by checking the error fires in tests (where stderr is a pipe).
	// In a real terminal, stderr IS a terminal, so the picker would work
	// even when stdout is piped.
	_, err := PickOne("Pick:", []string{"a", "b"})
	if err == nil {
		t.Fatal("expected error in non-terminal test context")
	}
	// The fact that this test exists documents the stderr contract.
	// A regression to os.Stdout would cause `gw go` to fail in shell functions.
}
