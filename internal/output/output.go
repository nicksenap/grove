// Package output defines the stable output formats shared by Grove commands.
package output

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
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

// WriteJSON writes one indented JSON value.
func WriteJSON(w io.Writer, value any) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

// WriteJSONLine writes one compact JSON value followed by a newline.
func WriteJSONLine(w io.Writer, value any) error {
	return json.NewEncoder(w).Encode(value)
}

// WriteJSONLines writes each value as one compact JSON line.
func WriteJSONLines[T any](w io.Writer, values []T) error {
	encoder := json.NewEncoder(w)
	for _, value := range values {
		if err := encoder.Encode(value); err != nil {
			return err
		}
	}
	return nil
}

// WriteTSV writes rows using tab-separated CSV escaping rules.
func WriteTSV(w io.Writer, rows [][]string) error {
	writer := csv.NewWriter(w)
	writer.Comma = '\t'
	if err := writer.WriteAll(rows); err != nil {
		return err
	}
	return writer.Error()
}

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
