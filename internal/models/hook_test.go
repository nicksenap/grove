package models

import (
	"bytes"
	"testing"

	"github.com/BurntSushi/toml"
)

// TestHookUnmarshalStringForm verifies a bare string hook parses into Command
// with no metadata (backward compatibility).
func TestHookUnmarshalStringForm(t *testing.T) {
	in := `[hooks]
pre_delete = "gw harvest {path}"
`
	var cfg Config
	if err := toml.Unmarshal([]byte(in), &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	h, ok := cfg.Hooks["pre_delete"]
	if !ok {
		t.Fatal("pre_delete hook not parsed")
	}
	if h.Command != "gw harvest {path}" {
		t.Errorf("Command = %q, want %q", h.Command, "gw harvest {path}")
	}
	if h.Stream || h.Description != "" || h.Timeout != "" || h.OnFailure != "" {
		t.Errorf("string-form hook should carry no metadata, got %+v", h)
	}
}

// TestHookUnmarshalTableForm verifies the table form parses command + metadata.
func TestHookUnmarshalTableForm(t *testing.T) {
	in := `[hooks.post_create]
command = "npm install"
description = "install deps"
stream = true
timeout = "5m"
on_failure = "abort"
`
	var cfg Config
	if err := toml.Unmarshal([]byte(in), &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	h := cfg.Hooks["post_create"]
	want := Hook{
		Command:     "npm install",
		Description: "install deps",
		Stream:      true,
		Timeout:     "5m",
		OnFailure:   "abort",
	}
	if h != want {
		t.Errorf("parsed hook = %+v, want %+v", h, want)
	}
}

// TestHookMarshalKeepsBareString verifies a metadata-free hook is written back
// as a bare string, so config.Save() never bloats a plain hook into a table.
func TestHookMarshalKeepsBareString(t *testing.T) {
	cfg := Config{Hooks: map[string]Hook{
		"pre_delete": {Command: "gw harvest {path}"},
	}}
	var b bytes.Buffer
	if err := toml.NewEncoder(&b).Encode(cfg); err != nil {
		t.Fatalf("encode: %v", err)
	}
	got := b.String()
	if !bytes.Contains(b.Bytes(), []byte(`pre_delete = "gw harvest {path}"`)) {
		t.Errorf("bare hook should encode as a string, got:\n%s", got)
	}
	if bytes.Contains(b.Bytes(), []byte("command =")) {
		t.Errorf("bare hook should NOT encode as a table, got:\n%s", got)
	}
}

// TestHookRoundTrip verifies both forms survive an encode/decode cycle, and a
// hook with metadata is preserved as a table.
func TestHookRoundTrip(t *testing.T) {
	orig := Config{Hooks: map[string]Hook{
		"pre_delete":  {Command: "gw harvest {path}"},
		"post_create": {Command: "npm install", Stream: true, Description: "deps", OnFailure: "abort"},
	}}
	var b bytes.Buffer
	if err := toml.NewEncoder(&b).Encode(orig); err != nil {
		t.Fatalf("encode: %v", err)
	}
	var back Config
	if err := toml.Unmarshal(b.Bytes(), &back); err != nil {
		t.Fatalf("decode: %v\nencoded:\n%s", err, b.String())
	}
	for name, want := range orig.Hooks {
		if got := back.Hooks[name]; got != want {
			t.Errorf("hook %q round-trip: got %+v, want %+v\nencoded:\n%s", name, got, want, b.String())
		}
	}
}
