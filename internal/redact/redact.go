// Package redact removes common credentials and local identity from diagnostics.
package redact

import (
	"net/url"
	"regexp"
	"strings"
)

var (
	urlRE           = regexp.MustCompile(`(?i)(?:https?|ssh|git)://[^\s<>"']+`)
	authorizationRE = regexp.MustCompile(`(?i)(\bauthorization\s*[:=]\s*)(?:(?:bearer|basic)\s+)?(?:"[^"]*"|'[^']*'|[^\s,;]+)`)
	assignmentRE    = regexp.MustCompile(`(?i)(["']?(?:token|password|passwd|secret|api[-_]?key|access[-_]?token)["']?\s*[:=]\s*)(?:"[^"]*"|'[^']*'|[^\s,;}]+)`)
	flagRE          = regexp.MustCompile(`(?i)(--(?:token|password|passwd|secret|api[-_]?key|access[-_]?token)(?:=|\s+))(?:"[^"]*"|'[^']*'|[^\s,;]+)`)
	environmentRE   = regexp.MustCompile(`\b[A-Z][A-Z0-9_]{1,}=(?:"[^"]*"|'[^']*'|[^\s]+)`)
)

// Text redacts common secret formats, environment values, URL credentials,
// URL query/fragment data, and the user's home-directory prefix.
func Text(input, home string) string {
	if home != "" {
		input = strings.ReplaceAll(input, home, "~")
	}
	input = urlRE.ReplaceAllStringFunc(input, sanitizeURL)
	input = authorizationRE.ReplaceAllString(input, "$1[REDACTED]")
	input = assignmentRE.ReplaceAllString(input, "$1[REDACTED]")
	input = flagRE.ReplaceAllString(input, "$1[REDACTED]")
	input = environmentRE.ReplaceAllStringFunc(input, redactEnvironmentValue)
	return input
}

func redactEnvironmentValue(value string) string {
	name, _, _ := strings.Cut(value, "=")
	return name + "=[REDACTED]"
}

func sanitizeURL(raw string) string {
	trailing := ""
	for len(raw) > 0 && strings.ContainsRune(".,);]", rune(raw[len(raw)-1])) {
		trailing = raw[len(raw)-1:] + trailing
		raw = raw[:len(raw)-1]
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return "[REDACTED_URL]" + trailing
	}
	if parsed.User != nil {
		parsed.User = url.User("[REDACTED]")
	}
	query := parsed.Query()
	for key := range query {
		query.Set(key, "[REDACTED]")
	}
	parsed.RawQuery = query.Encode()
	if parsed.Fragment != "" {
		parsed.Fragment = "[REDACTED]"
	}
	return parsed.String() + trailing
}
