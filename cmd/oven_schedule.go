package cmd

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

var (
	ovenScheduleEvery      time.Duration
	ovenScheduleFormat     string
	ovenScheduleExecutable string
)

type scheduleExampleOptions struct {
	Format     string
	Every      time.Duration
	Executable string
	RecipePath string
}

type ovenScheduleExampleOutput struct {
	Format     string `json:"format"`
	Every      string `json:"every"`
	Executable string `json:"executable"`
	Recipe     string `json:"recipe"`
	Example    string `json:"example"`
}

var ovenScheduleExampleCmd = &cobra.Command{
	Use:   "schedule-example RECIPE",
	Short: "Print an external scheduler example for Oven reconciliation",
	Long:  "Print an example for launchd, cron, or systemd. Grove does not install or manage the scheduler.",
	Args:  cobra.ExactArgs(1),
	Annotations: map[string]string{
		offlineCommandAnnotation: "true",
	},
	RunE: runOvenScheduleExample,
}

func init() {
	ovenScheduleExampleCmd.Flags().DurationVar(&ovenScheduleEvery, "every", 0, "Reconciliation interval (whole minutes)")
	ovenScheduleExampleCmd.Flags().StringVar(&ovenScheduleFormat, "format", defaultOvenScheduleFormat(), "Scheduler format: launchd, cron, or systemd")
	if err := ovenScheduleExampleCmd.MarkFlagRequired("every"); err != nil {
		panic(err)
	}
	ovenCmd.AddCommand(ovenScheduleExampleCmd)
}

func runOvenScheduleExample(cmd *cobra.Command, args []string) error {
	_, recipePath, err := loadOvenRecipe(args[0])
	if err != nil {
		return err
	}
	recipePath, err = canonicalSchedulePath(recipePath)
	if err != nil {
		return err
	}
	executable := ovenScheduleExecutable
	if executable == "" {
		executable, err = os.Executable()
		if err != nil {
			return fmt.Errorf("resolving gw executable: %w", err)
		}
	}
	executable, err = canonicalSchedulePath(executable)
	if err != nil {
		return err
	}
	example, err := renderOvenScheduleExample(scheduleExampleOptions{
		Format: ovenScheduleFormat, Every: ovenScheduleEvery, Executable: executable, RecipePath: recipePath,
	})
	if err != nil {
		return err
	}
	output := ovenScheduleExampleOutput{
		Format: ovenScheduleFormat, Every: ovenScheduleEvery.String(), Executable: executable, Recipe: recipePath, Example: example,
	}
	if ovenJSON {
		return json.NewEncoder(cmd.OutOrStdout()).Encode(output)
	}
	_, err = fmt.Fprint(cmd.OutOrStdout(), example)
	return err
}

func renderOvenScheduleExample(options scheduleExampleOptions) (string, error) {
	if options.Every < time.Minute || options.Every%time.Minute != 0 {
		return "", fmt.Errorf("--every must be a whole number of minutes")
	}
	if !filepath.IsAbs(options.Executable) || !filepath.IsAbs(options.RecipePath) {
		return "", fmt.Errorf("scheduler examples require absolute executable and Recipe paths")
	}
	if err := validateSchedulerPath(options.Executable); err != nil {
		return "", fmt.Errorf("executable path: %w", err)
	}
	if err := validateSchedulerPath(options.RecipePath); err != nil {
		return "", fmt.Errorf("recipe path: %w", err)
	}
	switch options.Format {
	case "launchd":
		return renderLaunchdExample(options), nil
	case "cron":
		return renderCronExample(options)
	case "systemd":
		return renderSystemdExample(options), nil
	default:
		return "", fmt.Errorf("unsupported scheduler format %q (use launchd, cron, or systemd)", options.Format)
	}
}

func renderLaunchdExample(options scheduleExampleOptions) string {
	seconds := int64(options.Every / time.Second)
	identifier := scheduleIdentifier(options.RecipePath)
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!-- Save as ~/Library/LaunchAgents/com.grove.oven.%s.plist, then load with:
     launchctl bootstrap gui/$(id -u) ~/Library/LaunchAgents/com.grove.oven.%s.plist
     Grove does not install or manage this job. -->
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>com.grove.oven.%s</string>
  <key>ProgramArguments</key>
  <array>
    <string>%s</string>
    <string>oven</string>
    <string>reconcile</string>
    <string>%s</string>
  </array>
  <key>StartInterval</key>
  <integer>%d</integer>
  <key>RunAtLoad</key>
  <true/>
</dict>
</plist>
`, identifier, identifier, identifier, xmlEscape(options.Executable), xmlEscape(options.RecipePath), seconds)
}

func renderCronExample(options scheduleExampleOptions) (string, error) {
	minutes := int64(options.Every / time.Minute)
	var expression string
	switch {
	case minutes < 60 && 60%minutes == 0:
		expression = fmt.Sprintf("*/%d * * * *", minutes)
	case minutes%60 == 0 && minutes/60 < 24 && 24%(minutes/60) == 0:
		expression = fmt.Sprintf("0 */%d * * *", minutes/60)
	case minutes == 24*60:
		expression = "0 0 * * *"
	default:
		return "", fmt.Errorf("cron format supports intervals that evenly divide an hour or day")
	}
	return fmt.Sprintf("# Add with `crontab -e`. Grove does not install or manage this entry.\n%s %s oven reconcile %s\n",
		expression, shellQuote(escapeCronPercent(options.Executable)), shellQuote(escapeCronPercent(options.RecipePath))), nil
}

func renderSystemdExample(options scheduleExampleOptions) string {
	identifier := scheduleIdentifier(options.RecipePath)
	return fmt.Sprintf(`# Save these as ~/.config/systemd/user/grove-oven-%s.service and grove-oven-%s.timer.
# Enable with: systemctl --user enable --now grove-oven-%s.timer
# Grove does not install or manage these units.

# grove-oven.service
[Unit]
Description=Reconcile Grove Oven Recipe

[Service]
Type=oneshot
ExecStart=%s oven reconcile %s

# grove-oven.timer
[Unit]
Description=Periodically reconcile Grove Oven Recipe

[Timer]
OnBootSec=1m
OnUnitActiveSec=%s
Persistent=true

[Install]
WantedBy=timers.target
`, identifier, identifier, identifier, systemdQuote(options.Executable), systemdQuote(options.RecipePath), compactDuration(options.Every))
}

func defaultOvenScheduleFormat() string {
	switch runtime.GOOS {
	case "darwin":
		return "launchd"
	case "linux":
		return "systemd"
	default:
		return "cron"
	}
}

func canonicalSchedulePath(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", fmt.Errorf("resolving path %s: %w", absolute, err)
	}
	return filepath.Clean(resolved), nil
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

func systemdQuote(value string) string {
	value = strings.ReplaceAll(value, "%", "%%")
	value = strings.ReplaceAll(value, "$", "$$")
	return strconv.Quote(value)
}

func escapeCronPercent(value string) string {
	return strings.ReplaceAll(value, "%", `\%`)
}

func validateSchedulerPath(value string) error {
	for _, char := range value {
		if char < 0x20 || char == 0x7f {
			return fmt.Errorf("contains a control character")
		}
	}
	return nil
}

func scheduleIdentifier(recipePath string) string {
	sum := sha256.Sum256([]byte(recipePath))
	return hex.EncodeToString(sum[:6])
}

func xmlEscape(value string) string {
	replacer := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;")
	return replacer.Replace(value)
}

func compactDuration(duration time.Duration) string {
	minutes := int64(duration / time.Minute)
	if minutes%60 == 0 {
		return fmt.Sprintf("%dh", minutes/60)
	}
	return fmt.Sprintf("%dm", minutes)
}
