package cmd

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// skillDir holds the user-facing agent skill. Every `gw ...` invocation in
// its code spans and fenced blocks must name real commands and flags, so the
// skill fails `just check` instead of silently drifting from the CLI.
const skillDir = "../skills/grove"

var (
	fencedBlock = regexp.MustCompile("(?s)```[a-z]*\n(.*?)```")
	inlineCode  = regexp.MustCompile("`([^`\n]+)`")
	gwCall      = regexp.MustCompile(`(?:^|[^\w./-])gw\s+(.*)`)
	flagToken   = regexp.MustCompile(`^--?[A-Za-z][\w-]*`)
)

func TestSkillDocsMatchCLI(t *testing.T) {
	files, err := filepath.Glob(filepath.Join(skillDir, "*.md"))
	if err != nil {
		t.Fatal(err)
	}
	refs, _ := filepath.Glob(filepath.Join(skillDir, "references", "*.md"))
	files = append(files, refs...)
	if len(files) == 0 {
		t.Fatalf("no skill docs found under %s", skillDir)
	}

	checked := 0
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, snippet := range codeSnippets(string(data)) {
			for _, call := range gwCalls(snippet) {
				checked++
				if msg := checkInvocation(call); msg != "" {
					t.Errorf("%s: `gw %s`: %s", filepath.Base(file), call, msg)
				}
			}
		}
	}
	// Guards against the extraction regexes silently matching nothing.
	if checked < 30 {
		t.Fatalf("only %d gw invocations found in skill docs; extraction is broken", checked)
	}
}

// codeSnippets returns fenced-block lines and inline code spans, with
// markdown-table pipe escapes undone.
func codeSnippets(doc string) []string {
	var out []string
	for _, m := range fencedBlock.FindAllStringSubmatch(doc, -1) {
		out = append(out, strings.Split(m[1], "\n")...)
	}
	prose := fencedBlock.ReplaceAllString(doc, "")
	for _, m := range inlineCode.FindAllStringSubmatch(prose, -1) {
		out = append(out, m[1])
	}
	for i := range out {
		out[i] = strings.ReplaceAll(out[i], `\|`, "|")
	}
	return out
}

// gwCalls splits a snippet into the argument text of each gw invocation,
// ending at a shell boundary (`)`, `;`, `&&`, comment, or a pipe into another
// command). A pipe followed by a flag stays inside, as in `(-r a | -p P)`.
func gwCalls(snippet string) []string {
	var calls []string
	rest := snippet
	for {
		m := gwCall.FindStringSubmatchIndex(rest)
		if m == nil {
			return calls
		}
		args := rest[m[2]:m[3]]
		end := len(args)
		for i := 0; i < len(args); i++ {
			switch {
			case args[i] == ')' || args[i] == ';' || args[i] == '"',
				strings.HasPrefix(args[i:], "&&"),
				strings.HasPrefix(args[i:], " #"):
				end = i
			case args[i] == '|':
				if !strings.HasPrefix(strings.TrimLeft(args[i+1:], " "), "-") {
					end = i
				}
			}
			if end != len(args) {
				break
			}
		}
		calls = append(calls, strings.TrimSpace(args[:end]))
		rest = rest[m[2]+end:]
	}
}

// checkInvocation resolves subcommands, then checks every flag token against
// the resolved command. It returns a problem description, or "".
func checkInvocation(call string) string {
	tokens := strings.Fields(call)
	c := rootCmd
	i := 0
	for ; i < len(tokens); i++ {
		next := findChild(c, tokens[i])
		if next == nil {
			break
		}
		c = next
	}
	if c == rootCmd && i < len(tokens) && !strings.HasPrefix(tokens[i], "-") {
		if strings.HasPrefix(tokens[i], "<") || strings.HasPrefix(tokens[i], "[") {
			return "" // placeholder such as `gw <command> --help`
		}
		return "unknown command " + tokens[i]
	}
	c.InitDefaultHelpFlag()
	c.InitDefaultVersionFlag()
	for _, tok := range tokens[i:] {
		tok = strings.TrimLeft(tok, "([")
		flag := flagToken.FindString(tok)
		if flag == "" {
			continue
		}
		if !hasFlag(c, flag) {
			return "unknown flag " + flag + " for " + c.CommandPath()
		}
	}
	return ""
}

func findChild(c *cobra.Command, name string) *cobra.Command {
	for _, sub := range c.Commands() {
		if sub.Name() == name || sub.HasAlias(name) {
			return sub
		}
	}
	return nil
}

func hasFlag(c *cobra.Command, flag string) bool {
	if long, ok := strings.CutPrefix(flag, "--"); ok {
		return c.Flags().Lookup(long) != nil || c.InheritedFlags().Lookup(long) != nil
	}
	short := strings.TrimPrefix(flag, "-")
	if len(short) != 1 {
		return false
	}
	return c.Flags().ShorthandLookup(short) != nil || c.InheritedFlags().ShorthandLookup(short) != nil
}
