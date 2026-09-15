package cmd

import (
	"strings"
	"testing"
)

func TestReadDeleteNamesReadsTrimmedUniqueLines(t *testing.T) {
	names, err := readDeleteNames(strings.NewReader("alpha\n\n beta \nalpha\n"))
	if err != nil {
		t.Fatalf("readDeleteNames: %v", err)
	}
	if got := strings.Join(names, ","); got != "alpha,beta" {
		t.Fatalf("names = %q", got)
	}
}

func TestResolveDeleteNamesAcceptsMultipleArguments(t *testing.T) {
	names, err := resolveDeleteNames([]string{"alpha", "beta"}, false, false, strings.NewReader(""))
	if err != nil {
		t.Fatalf("resolveDeleteNames: %v", err)
	}
	if got := strings.Join(names, ","); got != "alpha,beta" {
		t.Fatalf("names = %q", got)
	}
}

func TestResolveDeleteNamesRequiresYesForStdin(t *testing.T) {
	_, err := resolveDeleteNames(nil, true, false, strings.NewReader("alpha\n"))
	if err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("expected --yes error, got %v", err)
	}
}

func TestResolveDeleteNamesRejectsArgumentsWithStdin(t *testing.T) {
	_, err := resolveDeleteNames([]string{"alpha"}, true, true, strings.NewReader("beta\n"))
	if err == nil {
		t.Fatal("expected args plus --stdin to fail")
	}
}

func TestResolveDeleteNamesRejectsEmptyStdin(t *testing.T) {
	_, err := resolveDeleteNames(nil, true, true, strings.NewReader("\n"))
	if err == nil {
		t.Fatal("expected empty stdin to fail")
	}
}
