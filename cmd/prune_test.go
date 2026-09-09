package cmd

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/nicksenap/grove/internal/models"
)

func TestPruneCommandDefaults(t *testing.T) {
	if pruneCmd.Name() != "prune" {
		t.Fatalf("command name = %q", pruneCmd.Name())
	}
	flag := pruneCmd.Flags().Lookup("min-age")
	if flag == nil {
		t.Fatal("missing --min-age flag")
	}
	if flag.DefValue != "7" {
		t.Fatalf("--min-age default = %q, want 7", flag.DefValue)
	}
	yes := pruneCmd.Flags().Lookup("yes")
	if yes == nil {
		t.Fatal("missing --yes flag")
	}
	if yes.DefValue != "false" {
		t.Fatalf("--yes default = %q, want false", yes.DefValue)
	}
}

func TestPruneCandidatesReportsAgeDays(t *testing.T) {
	now := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	ws := []models.Workspace{
		{Name: "old", Branch: "feat/old", CreatedAt: now.Add(-10 * 24 * time.Hour).Format("2006-01-02T15:04:05.000000")},
		{Name: "fresh", Branch: "feat/fresh", CreatedAt: now.Add(-2 * 24 * time.Hour).Format("2006-01-02T15:04:05.000000")},
	}

	got := pruneCandidates(ws, 7, now)
	if len(got) != 1 {
		t.Fatalf("got %+v, want only old", got)
	}
	if got[0].Name != "old" || got[0].Branch != "feat/old" || got[0].AgeDays != 10 {
		t.Fatalf("candidate = %+v", got[0])
	}
}

func TestWritePrunePreviewJSON(t *testing.T) {
	var stdout bytes.Buffer
	candidates := []pruneCandidate{{
		Name:      "old",
		Branch:    "feat/old",
		CreatedAt: "2026-04-01T12:00:00.000000",
		AgeDays:   9,
	}}
	if err := writePrunePreview(candidates, 7, true, false, &stdout); err != nil {
		t.Fatal(err)
	}
	var decoded []pruneCandidate
	if err := json.Unmarshal(stdout.Bytes(), &decoded); err != nil {
		t.Fatalf("json: %v\n%s", err, stdout.String())
	}
	if len(decoded) != 1 || decoded[0].Name != "old" || decoded[0].AgeDays != 9 {
		t.Fatalf("decoded = %+v", decoded)
	}
}

func TestWritePrunePreviewEmptyJSON(t *testing.T) {
	var stdout bytes.Buffer
	if err := writePrunePreview(nil, 7, true, false, &stdout); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(stdout.String()) != "[]" {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestValidatePruneMinAgeRejectsNegative(t *testing.T) {
	if err := validatePruneMinAge(-1); err == nil {
		t.Fatal("expected error for negative --min-age")
	}
}
