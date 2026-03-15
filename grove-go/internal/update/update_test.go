package update

import (
	"testing"
)

func TestIsNewer(t *testing.T) {
	tests := []struct {
		latest  string
		current string
		want    bool
	}{
		{"1.0.0", "0.9.0", true},
		{"1.1.0", "1.0.0", true},
		{"1.0.1", "1.0.0", true},
		{"2.0.0", "1.9.9", true},
		{"1.0.0", "1.0.0", false},
		{"0.9.0", "1.0.0", false},
		{"1.0.0", "1.0.1", false},
		// With v prefix
		{"v1.1.0", "v1.0.0", true},
		{"v1.0.0", "v1.0.0", false},
	}
	for _, tt := range tests {
		got := isNewer(tt.latest, tt.current)
		if got != tt.want {
			t.Errorf("isNewer(%q, %q) = %v, want %v", tt.latest, tt.current, got, tt.want)
		}
	}
}

func TestParseVersion(t *testing.T) {
	tests := []struct {
		input string
		want  []int
	}{
		{"1.2.3", []int{1, 2, 3}},
		{"v0.12.4", []int{0, 12, 4}},
		{"10.0.0", []int{10, 0, 0}},
	}
	for _, tt := range tests {
		got := parseVersion(tt.input)
		if len(got) != len(tt.want) {
			t.Errorf("parseVersion(%q) length = %d, want %d", tt.input, len(got), len(tt.want))
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("parseVersion(%q)[%d] = %d, want %d", tt.input, i, got[i], tt.want[i])
			}
		}
	}
}

func TestGetNewerVersionNoCache(t *testing.T) {
	// With no cache, should return empty string (non-blocking)
	result := GetNewerVersion("999.0.0")
	if result != "" {
		t.Errorf("expected empty string, got %q", result)
	}
}
