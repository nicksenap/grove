package plugin

import (
	"strings"
	"testing"
)

func TestEmbeddedRegistryIsValid(t *testing.T) {
	entries, err := Registry()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Fatal("registry is empty")
	}
	for _, entry := range entries {
		if entry.Description == "" {
			t.Errorf("%s: missing description", entry.Name)
		}
		if !strings.HasSuffix(entry.Repo, "/gw-"+entry.Name) {
			t.Errorf("%s: repo %q should end with gw-%s", entry.Name, entry.Repo, entry.Name)
		}
	}
}

func TestParseRegistryRejectsDuplicatesAndBadRepos(t *testing.T) {
	if _, err := parseRegistry([]byte(`[{"name":"a","repo":"x/gw-a"},{"name":"a","repo":"y/gw-a"}]`)); err == nil {
		t.Fatal("expected duplicate error")
	}
	if _, err := parseRegistry([]byte(`[{"name":"a","repo":"gw-a"}]`)); err == nil {
		t.Fatal("expected repo format error")
	}
}

func TestSearchMatchesDescriptionCaseInsensitively(t *testing.T) {
	matches, err := Search("VS CODE")
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 || matches[0].Name != "code" {
		t.Fatalf("matches = %+v", matches)
	}
}

func TestResolveRepo(t *testing.T) {
	if got, _ := ResolveRepo("dispatch"); got != "nicksenap/gw-dispatch" {
		t.Fatalf("bare name resolved to %q", got)
	}
	if got, _ := ResolveRepo("someone/gw-thing"); got != "someone/gw-thing" {
		t.Fatalf("owner/repo changed to %q", got)
	}
	if _, err := ResolveRepo("nope"); err == nil {
		t.Fatal("expected unknown plugin error")
	}
}
