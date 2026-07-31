package cmd

import (
	"os"

	"github.com/nicksenap/grove/internal/console"
	"github.com/nicksenap/grove/internal/picker"
)

// Prompter is the seam for every interaction that needs a human: menus,
// confirmations, free-text input, and the question of whether a human is there at
// all.
//
// Commands go through this instead of calling picker/console directly for two
// reasons. It makes interactive flows testable — a scripted Prompter can answer
// "pick the second preset, then decline the save" without a terminal, which is the
// only way to cover the branches that a non-interactive test suite otherwise
// cannot reach. And it puts every "asks a human" call behind one interface, so the
// rule that machine mode never prompts has one place to hold rather than being
// re-derived at each call site.
//
// Machine-mode enforcement still lives in picker and console, so a plugin or
// future caller that bypasses this seam cannot accidentally block on input.
type Prompter interface {
	// Interactive reports whether there is a human to ask.
	Interactive() bool
	// PickOne shows a single-select menu.
	PickOne(prompt string, choices []string) (string, error)
	// PickMany shows a multi-select menu.
	PickMany(prompt string, choices []string) ([]string, error)
	// Confirm asks a yes/no question, returning defaultYes on empty input.
	Confirm(prompt string, defaultYes bool) bool
	// Prompt asks for text, returning defaultValue on empty input. Pass "" for no
	// default.
	Prompt(label, defaultValue string) string
}

// prompter is the active Prompter. Tests replace it; production never does.
var prompter Prompter = terminalPrompter{}

// terminalPrompter is the production implementation: a thin delegation to the
// picker and console packages, holding no logic of its own so that swapping it out
// in a test cannot change what the code under test does.
type terminalPrompter struct{}

func (terminalPrompter) Interactive() bool {
	return console.IsTerminal(os.Stdin)
}

func (terminalPrompter) PickOne(prompt string, choices []string) (string, error) {
	return picker.PickOne(prompt, choices)
}

func (terminalPrompter) PickMany(prompt string, choices []string) ([]string, error) {
	return picker.PickMany(prompt, choices)
}

func (terminalPrompter) Confirm(prompt string, defaultYes bool) bool {
	return console.Confirm(prompt, defaultYes)
}

func (terminalPrompter) Prompt(label, defaultValue string) string {
	return console.PromptDefault(label, defaultValue)
}
