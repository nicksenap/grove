package cmd

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseDiffArgs(t *testing.T) {
	tests := []struct {
		args []string
		want diffOptions
	}{
		{nil, diffOptions{}},
		{[]string{"--staged", "-w", "-U10", "main...HEAD"},
			diffOptions{gitArgs: []string{"--staged", "-w", "-U10", "main...HEAD"}}},
		{[]string{"--workspace", "ws", "--no-pager", "--stat"},
			diffOptions{workspace: "ws", noPager: true, gitArgs: []string{"--stat"}}},
		{[]string{"--workspace=ws", "-h"}, diffOptions{workspace: "ws", help: true}},
		{[]string{"--verbose", "-n", "--no-hooks=false", "--stat"},
			diffOptions{rootFlags: []string{"--verbose", "-n", "--no-hooks=false"}, gitArgs: []string{"--stat"}}},
		{[]string{"HEAD", "--", "--no-pager"},
			diffOptions{gitArgs: []string{"HEAD", "--", "--no-pager"}}},
	}
	for _, tt := range tests {
		got, err := parseDiffArgs(tt.args)
		if err != nil {
			t.Fatalf("%v: %v", tt.args, err)
		}
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("parseDiffArgs(%v) = %+v, want %+v", tt.args, got, tt.want)
		}
	}
	if _, err := parseDiffArgs([]string{"--workspace"}); err == nil {
		t.Error("--workspace without a value should fail")
	}
}

func TestExitStatusOnly(t *testing.T) {
	for args, want := range map[string]bool{
		"":                    false,
		"--stat":              false,
		"--exit-code":         true,
		"HEAD --quiet":        true,
		"HEAD -- --exit-code": false,
	} {
		if got := exitStatusOnly(strings.Fields(args)); got != want {
			t.Errorf("exitStatusOnly(%q) = %v, want %v", args, got, want)
		}
	}
}
