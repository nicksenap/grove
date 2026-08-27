package e2e

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPlugins(t *testing.T) {
	env := newEnv(t)
	env.createRepo("svc-auth")
	env.init()

	empty := env.mustGW("plugin", "list")
	env.requireContains(empty.combined(), "No plugins", "plugin list empty")

	pluginsDir := filepath.Join(env.groveDir, "plugins")
	if err := os.MkdirAll(pluginsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	pluginPath := filepath.Join(pluginsDir, "gw-hello")
	env.writeFile(pluginPath, "#!/bin/sh\necho \"hello-from-plugin GROVE_DIR=${GROVE_DIR} args=$*\"\n")
	if err := os.Chmod(pluginPath, 0o755); err != nil {
		t.Fatal(err)
	}

	listed := env.mustGW("plugin", "list")
	env.requireContains(listed.combined(), "hello", "plugin list installed")

	pluginJSONOut := env.mustGW("plugin", "list", "--json")
	plugins := decodeJSON[[]pluginJSON](t, pluginJSONOut.stdout)
	if len(plugins) == 0 || plugins[0].Name != "hello" {
		t.Fatalf("plugin list --json failed: %s", pluginJSONOut.stdout)
	}
	if plugins[0].Path == "" {
		t.Fatal("plugin list --json missing path")
	}

	hello := env.mustGW("hello", "--test-flag")
	env.requireContains(hello.combined(), "hello-from-plugin", "plugin fallback")
	env.requireContains(hello.combined(), "GROVE_DIR=", "GROVE_DIR passed to plugin")
	env.requireContains(hello.combined(), "--test-flag", "args forwarded to plugin")

	env.mustGW("plugin", "remove", "hello")
	env.requireMissing(pluginPath)
	env.gw("plugin", "remove", "nonexistent").mustFail(t)
	env.gw("nonexistent-cmd").mustFail(t)

	missingRun := env.gw("run", "anything")
	missingRun.mustFail(t)
	env.requireContains(missingRun.combined(), `unknown command "run"`, "gw run without plugin")
}
