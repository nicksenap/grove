package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

func TestRenderOvenScheduleExamples(t *testing.T) {
	executable := "/opt/homebrew/bin/gw"
	recipePath := "/Users/example/My Recipes/stack.yaml"

	t.Run("launchd", func(t *testing.T) {
		output, err := renderOvenScheduleExample(scheduleExampleOptions{
			Format: "launchd", Every: 30 * time.Minute, Executable: executable, RecipePath: recipePath,
		})
		if err != nil {
			t.Fatal(err)
		}
		for _, expected := range []string{
			`<string>/opt/homebrew/bin/gw</string>`,
			`<string>oven</string>`,
			`<string>reconcile</string>`,
			`<string>/Users/example/My Recipes/stack.yaml</string>`,
			`<integer>1800</integer>`,
			`<key>RunAtLoad</key>`,
			`<string>com.grove.oven.`,
		} {
			if !strings.Contains(output, expected) {
				t.Fatalf("launchd output missing %q:\n%s", expected, output)
			}
		}
	})

	t.Run("cron", func(t *testing.T) {
		output, err := renderOvenScheduleExample(scheduleExampleOptions{
			Format: "cron", Every: 30 * time.Minute, Executable: executable, RecipePath: recipePath,
		})
		if err != nil {
			t.Fatal(err)
		}
		expected := `*/30 * * * * '/opt/homebrew/bin/gw' oven reconcile '/Users/example/My Recipes/stack.yaml'`
		if !strings.Contains(output, expected) {
			t.Fatalf("cron output missing command:\n%s", output)
		}
	})

	t.Run("systemd", func(t *testing.T) {
		output, err := renderOvenScheduleExample(scheduleExampleOptions{
			Format: "systemd", Every: 30 * time.Minute, Executable: executable, RecipePath: recipePath,
		})
		if err != nil {
			t.Fatal(err)
		}
		for _, expected := range []string{
			`grove-oven-`,
			`ExecStart="/opt/homebrew/bin/gw" oven reconcile "/Users/example/My Recipes/stack.yaml"`,
			`OnUnitActiveSec=30m`,
			`Persistent=true`,
		} {
			if !strings.Contains(output, expected) {
				t.Fatalf("systemd output missing %q:\n%s", expected, output)
			}
		}
	})
}

func TestRenderOvenScheduleExamplesEscapeSchedulerSyntax(t *testing.T) {
	options := scheduleExampleOptions{
		Every: 30 * time.Minute, Executable: "/opt/gw%tool", RecipePath: "/tmp/recipe's $name%.yaml",
	}
	options.Format = "cron"
	cron, err := renderOvenScheduleExample(options)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(cron, `gw\%tool`) || !strings.Contains(cron, `recipe'\''s $name\%.yaml`) {
		t.Fatalf("cron escaping = %q", cron)
	}
	options.Format = "systemd"
	systemd, err := renderOvenScheduleExample(options)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(systemd, `gw%%tool`) || !strings.Contains(systemd, `$$name%%.yaml`) {
		t.Fatalf("systemd escaping = %q", systemd)
	}
	options.Format = "launchd"
	options.RecipePath = "/tmp/recipe&name.yaml"
	launchd, err := renderOvenScheduleExample(options)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(launchd, `recipe&amp;name.yaml`) {
		t.Fatalf("launchd escaping = %q", launchd)
	}
}

func TestRenderOvenScheduleExampleRejectsControlCharacters(t *testing.T) {
	for _, format := range []string{"launchd", "cron", "systemd"} {
		_, err := renderOvenScheduleExample(scheduleExampleOptions{
			Format: format, Every: 30 * time.Minute, Executable: "/bin/gw", RecipePath: "/tmp/recipe\n* * * * * injected.yaml",
		})
		if err == nil {
			t.Fatalf("%s accepted newline path", format)
		}
	}
}

func TestRenderOvenScheduleExampleUsesDistinctRecipeIdentifiers(t *testing.T) {
	first := renderLaunchdExample(scheduleExampleOptions{Every: time.Hour, Executable: "/bin/gw", RecipePath: "/tmp/a.yaml"})
	second := renderLaunchdExample(scheduleExampleOptions{Every: time.Hour, Executable: "/bin/gw", RecipePath: "/tmp/b.yaml"})
	if first == second || scheduleIdentifier("/tmp/a.yaml") == scheduleIdentifier("/tmp/b.yaml") {
		t.Fatal("different Recipes share a scheduler identifier")
	}
}

func TestCanonicalSchedulePathRejectsMissingPath(t *testing.T) {
	if _, err := canonicalSchedulePath(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("expected missing path error")
	}
}

func TestRenderOvenScheduleExampleRejectsInvalidIntervals(t *testing.T) {
	for _, test := range []struct {
		name     string
		format   string
		duration time.Duration
	}{
		{name: "less than minute", format: "launchd", duration: 30 * time.Second},
		{name: "fractional minute", format: "systemd", duration: 90 * time.Second},
		{name: "cron over hour not divisor", format: "cron", duration: 90 * time.Minute},
		{name: "unknown format", format: "other", duration: 30 * time.Minute},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := renderOvenScheduleExample(scheduleExampleOptions{
				Format: test.format, Every: test.duration, Executable: "/bin/gw", RecipePath: "/tmp/recipe.yaml",
			})
			if err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestRunOvenScheduleExampleCanonicalizesPathsAndSupportsJSON(t *testing.T) {
	directory := t.TempDir()
	recipePath := filepath.Join(directory, "recipe.yaml")
	if err := os.WriteFile(recipePath, []byte("version: 1\nrepositories:\n  api:\n    url: https://example.com/api.git\n    ref: main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	command, stdout, _ := ovenTestCommand()
	oldJSON := ovenJSON
	oldFormat := ovenScheduleFormat
	oldEvery := ovenScheduleEvery
	oldExecutable := ovenScheduleExecutable
	executablePath := filepath.Join(directory, "gw")
	if err := os.WriteFile(executablePath, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	ovenJSON = true
	ovenScheduleFormat = "cron"
	ovenScheduleEvery = 15 * time.Minute
	ovenScheduleExecutable = executablePath
	t.Cleanup(func() {
		ovenJSON = oldJSON
		ovenScheduleFormat = oldFormat
		ovenScheduleEvery = oldEvery
		ovenScheduleExecutable = oldExecutable
	})

	if err := runOvenScheduleExample(command, []string{recipePath}); err != nil {
		t.Fatal(err)
	}
	var output ovenScheduleExampleOutput
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatal(err)
	}
	expectedRecipe, err := canonicalSchedulePath(recipePath)
	if err != nil {
		t.Fatal(err)
	}
	expectedExecutable, err := canonicalSchedulePath(executablePath)
	if err != nil {
		t.Fatal(err)
	}
	if output.Format != "cron" || output.Every != "15m0s" || output.Recipe != expectedRecipe || output.Executable != expectedExecutable {
		t.Fatalf("schedule output = %+v", output)
	}
	if !strings.Contains(output.Example, "*/15 * * * *") {
		t.Fatalf("schedule example = %q", output.Example)
	}
}

func TestOvenScheduleExampleIsOfflineAndRequiresInterval(t *testing.T) {
	if ovenScheduleExampleCmd.Annotations[offlineCommandAnnotation] != "true" {
		t.Fatal("schedule-example must not run the update checker")
	}
	flag := ovenScheduleExampleCmd.Flags().Lookup("every")
	if flag == nil || flag.Annotations[cobra.BashCompOneRequiredFlag] == nil {
		t.Fatal("--every is not required")
	}
}

func TestDefaultOvenScheduleFormat(t *testing.T) {
	format := defaultOvenScheduleFormat()
	if runtime.GOOS == "darwin" && format != "launchd" {
		t.Fatalf("macOS default = %q", format)
	}
	if runtime.GOOS == "linux" && format != "systemd" {
		t.Fatalf("Linux default = %q", format)
	}
}
