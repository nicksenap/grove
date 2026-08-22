package redact

import (
	"strings"
	"testing"
)

func TestTextRedactsCredentialForms(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		secret string
	}{
		{name: "double quoted environment", input: `PRIVATE_SETTING="first secret second"`, secret: "first secret second"},
		{name: "single quoted environment", input: `PRIVATE_SETTING='first secret second'`, secret: "first secret second"},
		{name: "authorization", input: `Authorization: Bearer bearer-secret`, secret: "bearer-secret"},
		{name: "JSON assignment", input: `{"token":"json-secret"}`, secret: "json-secret"},
		{name: "CLI flag", input: `--api-key cli-secret`, secret: "cli-secret"},
		{name: "URL", input: `HTTPS://user:pass@example.com/repo?token=query-secret#fragment-secret`, secret: "query-secret"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Text(tt.input, "")
			if strings.Contains(result, tt.secret) {
				t.Fatalf("Text(%q) leaked %q: %q", tt.input, tt.secret, result)
			}
			if !strings.Contains(result, "[REDACTED]") {
				t.Fatalf("Text(%q) = %q, want redaction marker", tt.input, result)
			}
		})
	}
}
