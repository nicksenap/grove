package machine

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
)

func decode(t *testing.T, data []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, data)
	}
	return m
}

func TestSuccessEnvelopeShape(t *testing.T) {
	t.Cleanup(Reset)
	var buf bytes.Buffer
	EmitTo(&buf, map[string]string{"name": "feat-x"}, NextAction("inspect it", "gw status feat-x --format json"))

	got := decode(t, buf.Bytes())
	if got["ok"] != true {
		t.Errorf("ok = %v, want true", got["ok"])
	}
	if got["schemaVersion"] != float64(SchemaVersion) {
		t.Errorf("schemaVersion = %v, want %d", got["schemaVersion"], SchemaVersion)
	}
	result, ok := got["result"].(map[string]any)
	if !ok || result["name"] != "feat-x" {
		t.Errorf("result = %v", got["result"])
	}
	if _, ok := got["error"]; ok {
		t.Error("success envelope must not carry an error key")
	}
	actions, ok := got["next_actions"].([]any)
	if !ok || len(actions) != 1 {
		t.Fatalf("next_actions = %v", got["next_actions"])
	}
	first := actions[0].(map[string]any)
	if first["command"] != "gw status feat-x --format json" {
		t.Errorf("action command = %v", first["command"])
	}
}

// next_actions is always present so clients can iterate it unconditionally.
func TestNextActionsAlwaysPresent(t *testing.T) {
	t.Cleanup(Reset)
	var buf bytes.Buffer
	EmitTo(&buf, nil)

	got := decode(t, buf.Bytes())
	actions, ok := got["next_actions"].([]any)
	if !ok {
		t.Fatalf("next_actions missing or not an array: %v", got["next_actions"])
	}
	if len(actions) != 0 {
		t.Errorf("next_actions = %v, want []", actions)
	}
	// A nil result still serializes as an object, never as null.
	if _, ok := got["result"].(map[string]any); !ok {
		t.Errorf("result = %v, want {}", got["result"])
	}
}

func TestErrorEnvelopeShape(t *testing.T) {
	t.Cleanup(Reset)
	err := Errorf(CodeWorktreeDirty, "api has uncommitted changes").
		WithFix("Commit, stash, or explicitly force deletion").
		WithActions(NextAction("inspect changes", "gw status api --format json"))

	env := ErrorEnvelope(err)
	data, _ := json.Marshal(env)
	got := decode(t, data)

	if got["ok"] != false {
		t.Errorf("ok = %v, want false", got["ok"])
	}
	body, ok := got["error"].(map[string]any)
	if !ok {
		t.Fatalf("error body missing: %v", got)
	}
	if body["code"] != string(CodeWorktreeDirty) {
		t.Errorf("code = %v, want %s", body["code"], CodeWorktreeDirty)
	}
	if body["message"] != "api has uncommitted changes" {
		t.Errorf("message = %v", body["message"])
	}
	if got["fix"] != "Commit, stash, or explicitly force deletion" {
		t.Errorf("fix = %v", got["fix"])
	}
	if _, ok := got["result"]; ok {
		t.Error("error envelope must not carry a result key")
	}
}

// An unclassified error must still produce a valid envelope, flagged INTERNAL
// rather than silently mapped onto some unrelated code.
func TestUnclassifiedErrorBecomesInternal(t *testing.T) {
	t.Cleanup(Reset)
	env := ErrorEnvelope(errors.New("boom"))
	if env.Error.Code != CodeInternal {
		t.Errorf("code = %s, want %s", env.Error.Code, CodeInternal)
	}
	if ExitCodeFor(errors.New("boom")) != ExitFailure {
		t.Errorf("exit code = %d, want %d", ExitCodeFor(errors.New("boom")), ExitFailure)
	}
}

func TestAsErrorFindsWrappedClassification(t *testing.T) {
	sentinel := errors.New("git exploded")
	classified := Wrap(CodeGitFailed, sentinel, "cloning failed")
	wrapped := errors.Join(errors.New("context"), classified)

	if got := CodeFor(wrapped); got != CodeGitFailed {
		t.Errorf("code = %s, want %s", got, CodeGitFailed)
	}
	if !errors.Is(wrapped, sentinel) {
		t.Error("wrapping must preserve errors.Is on the original error")
	}
}

func TestExitCodeClasses(t *testing.T) {
	cases := map[Code]int{
		CodeUsage:             ExitUsage,
		CodeWorkspaceNotFound: ExitNotFound,
		CodeWorkspaceExists:   ExitConflict,
		CodeWorktreeDirty:     ExitConflict,
		CodeStateChanged:      ExitConflict,
		CodeNotInitialized:    ExitPrecondition,
		CodePermission:        ExitPermission,
		CodeTransient:         ExitTransient,
		CodeCancelled:         ExitCancelled,
	}
	for code, want := range cases {
		if got := ExitCodeFor(Errorf(code, "x")); got != want {
			t.Errorf("%s → exit %d, want %d", code, got, want)
		}
	}
	if ExitCodeFor(nil) != ExitOK {
		t.Error("nil error must exit 0")
	}
}

// Every declared code needs an exit class, or an agent gets a generic 1 for a
// failure Grove actually models.
func TestEveryCodeHasExitCode(t *testing.T) {
	for _, code := range AllCodes() {
		if _, ok := exitCodes[code]; !ok {
			t.Errorf("code %s has no exit code mapping", code)
		}
	}
}

func TestSetFormat(t *testing.T) {
	t.Cleanup(Reset)
	if Enabled() {
		t.Error("text mode must be the default")
	}
	if err := SetFormat("json"); err != nil {
		t.Fatalf("SetFormat(json): %v", err)
	}
	if !Enabled() {
		t.Error("json format should enable machine mode")
	}
	if err := SetFormat("yaml"); err == nil {
		t.Error("unknown format should be rejected")
	}
	if !Enabled() {
		t.Error("a rejected format must not change the active mode")
	}
}

func TestDetectEarly(t *testing.T) {
	for _, args := range [][]string{
		{"list", "--format", "json"},
		{"list", "--format=json"},
		{"list", "-o", "json"},
		{"create", "x", "-o=json"},
	} {
		Reset()
		DetectEarly(args)
		if !Enabled() {
			t.Errorf("DetectEarly(%v) did not enable machine mode", args)
		}
	}

	Reset()
	DetectEarly([]string{"create", "--branch", "feature/format"})
	if Enabled() {
		t.Error("DetectEarly must not be fooled by unrelated values")
	}
	t.Cleanup(Reset)
}

// Emit is a no-op in text mode: human commands print their own tables and must
// not have JSON interleaved into them.
func TestEmitSilentInTextMode(t *testing.T) {
	t.Cleanup(Reset)
	Reset()

	r, w, _ := os.Pipe()
	stdout := os.Stdout
	os.Stdout = w
	Emit(map[string]string{"a": "b"})
	w.Close()
	os.Stdout = stdout

	var buf bytes.Buffer
	buf.ReadFrom(r)
	if buf.Len() != 0 {
		t.Errorf("text mode wrote to stdout: %q", buf.String())
	}
	if Emitted() {
		t.Error("nothing was emitted, Emitted() should be false")
	}
}

func TestWarningsAttachToEnvelopeOnce(t *testing.T) {
	t.Cleanup(Reset)
	Reset()
	SetFormat("json")

	Warn("api: fetch failed, using local state")
	var buf bytes.Buffer
	EmitTo(&buf, nil)

	got := decode(t, buf.Bytes())
	warns, ok := got["warnings"].([]any)
	if !ok || len(warns) != 1 || !strings.Contains(warns[0].(string), "fetch failed") {
		t.Fatalf("warnings = %v", got["warnings"])
	}

	// Drained, so a second envelope does not repeat them.
	buf.Reset()
	EmitTo(&buf, nil)
	if _, ok := decode(t, buf.Bytes())["warnings"]; ok {
		t.Error("warnings should be drained after being emitted")
	}
}

func TestWarnIgnoredInTextMode(t *testing.T) {
	t.Cleanup(Reset)
	Reset()
	Warn("noise")
	if len(takeWarnings()) != 0 {
		t.Error("text mode should not accumulate warnings")
	}
}

// Envelope field order is part of the contract's readability, not its
// correctness — but the key set is contractual.
func TestEnvelopeKeySet(t *testing.T) {
	t.Cleanup(Reset)
	fields := map[string]bool{}
	typ := reflect.TypeOf(Envelope{})
	for i := 0; i < typ.NumField(); i++ {
		tag := typ.Field(i).Tag.Get("json")
		fields[strings.Split(tag, ",")[0]] = true
	}
	for _, want := range []string{"ok", "schemaVersion", "result", "error", "fix", "warnings", "next_actions"} {
		if !fields[want] {
			t.Errorf("envelope is missing the %q field", want)
		}
	}
}
