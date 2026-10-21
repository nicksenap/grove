package cmd

import (
	"reflect"
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
