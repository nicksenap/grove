package output

import "testing"

func TestResolvePreservesLegacyJSONFlag(t *testing.T) {
	got, err := Resolve("", true, Table, JSON, JSONLines)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != JSON {
		t.Fatalf("format = %q, want %q", got, JSON)
	}
}

func TestResolveAcceptsExplicitFormat(t *testing.T) {
	got, err := Resolve("jsonl", false, Table, JSON, JSONLines)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != JSONLines {
		t.Fatalf("format = %q, want %q", got, JSONLines)
	}
}

func TestResolveRejectsConflictingLegacyJSONFlag(t *testing.T) {
	if _, err := Resolve("tsv", true, Table, JSON, TSV); err == nil {
		t.Fatal("expected conflicting --json and --output error")
	}
}

func TestResolveRejectsUnsupportedFormat(t *testing.T) {
	if _, err := Resolve("path", false, Table, JSON, Name); err == nil {
		t.Fatal("expected unsupported output format error")
	}
}

func TestResolveDefaultsToTable(t *testing.T) {
	got, err := Resolve("", false, Table, JSON)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != Table {
		t.Fatalf("format = %q, want %q", got, Table)
	}
}
