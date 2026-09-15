// Package output defines the stable output formats shared by Grove commands.
package output

import (
	"fmt"
	"strings"
)

// Format identifies a command's stdout representation.
type Format string

const (
	Table     Format = "table"
	JSON      Format = "json"
	JSONLines Format = "jsonl"
	TSV       Format = "tsv"
	Name      Format = "name"
	Path      Format = "path"
)

// Resolve validates an explicit --output value and maps the legacy --json flag
// to the json format. Commands pass only the formats they support.
func Resolve(value string, legacyJSON bool, allowed ...Format) (Format, error) {
	format := Table
	if value != "" {
		format = Format(strings.ToLower(value))
	}

	if legacyJSON {
		if value != "" && format != JSON {
			return "", fmt.Errorf("--json cannot be combined with --output %s", value)
		}
		format = JSON
	}

	for _, candidate := range allowed {
		if format == candidate {
			return format, nil
		}
	}

	values := make([]string, len(allowed))
	for i, candidate := range allowed {
		values[i] = string(candidate)
	}
	return "", fmt.Errorf("unsupported output format %q (choose from: %s)", format, strings.Join(values, ", "))
}
