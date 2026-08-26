package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPostCreateSourcePlaceholders(t *testing.T) {
	env := newEnv(t)
	env.createRepo("svc-auth")
	env.init()
	marker := filepath.Join(env.home, "src_hook_out")
	env.writeConfig(`[hooks]
post_create = "echo url={source_url} ref={source_ref} title={source_title} > ` + marker + `"
`)

	env.mustGW("create", "hooksrc-ws", "--repos", "svc-auth", "--branch", "feat/hooksrc",
		"--source-url", "https://example.com/x",
		"--source-provider", "notion",
		"--source-ref", "pageid123",
		"--source-title", "My Task")

	data, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("post_create source vars missing: %v", err)
	}
	got := string(data)
	for _, want := range []string{"url=https://example.com/x", "ref=pageid123", "title=My Task"} {
		if !strings.Contains(got, want) {
			t.Fatalf("post_create source vars wrong: %s", got)
		}
	}
}

func TestLifecycleHooks(t *testing.T) {
	env := newEnv(t)
	env.createRepo("svc-auth")
	env.init()
	post := filepath.Join(env.home, "post_create_fired")
	pre := filepath.Join(env.home, "pre_delete_fired")
	env.writeConfig(`[hooks]
post_create = "touch ` + post + `"
pre_delete = "touch ` + pre + `"
`)

	env.mustGW("create", "hook-ws", "--branch", "feat/hook-test", "--repos", "svc-auth")
	env.requireExists(post)
	env.mustGW("delete", "hook-ws")
	env.requireExists(pre)
}

func TestNoHooksFlag(t *testing.T) {
	env := newEnv(t)
	env.createRepo("svc-auth")
	env.init()
	post := filepath.Join(env.home, "post_create_fired")
	pre := filepath.Join(env.home, "pre_delete_fired")
	env.writeConfig(`[hooks]
post_create = "touch ` + post + `"
pre_delete = "touch ` + pre + `"
`)

	env.mustGW("--no-hooks", "create", "nohook-ws", "--branch", "feat/no-hooks", "--repos", "svc-auth")
	env.requireMissing(post)
	env.mustGW("-n", "delete", "nohook-ws")
	env.requireMissing(pre)
}

func TestHookMetadataStreamAndOnFailure(t *testing.T) {
	env := newEnv(t)
	env.createRepo("svc-auth")
	env.init()

	env.writeConfig(`[hooks.post_create]
command = "echo hello-from-stream"
stream = true
`)
	streamOut := env.mustGW("create", "stream-ws", "--branch", "feat/stream", "--repos", "svc-auth")
	env.requireContains(streamOut.combined(), "[post_create] hello-from-stream", "stream hook")
	env.mustGW("delete", "stream-ws")

	env.writeConfig(`[hooks.post_create]
command = "echo boom-details; exit 1"
on_failure = "abort"
`)
	env.gw("create", "abort-ws", "--branch", "feat/abort", "--repos", "svc-auth").mustFail(t)
	env.mustGW("delete", "abort-ws")

	env.writeConfig(`[hooks.post_create]
command = "echo boom-details; exit 1"
`)
	env.mustGW("create", "warn-ws", "--branch", "feat/warn", "--repos", "svc-auth")
	env.mustGW("delete", "warn-ws")
}
