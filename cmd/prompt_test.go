package cmd

import (
	"fmt"
	"strings"
	"testing"
)

// scriptedPrompter is a Prompter that answers from a script instead of a terminal,
// so interactive flows can be tested without one.
//
// It is strict on purpose: an unscripted question fails the test rather than
// returning a zero value. A silent default would let a flow take a branch nobody
// wrote a case for and still pass.
type scriptedPrompter struct {
	t *testing.T

	interactive bool
	// Answers keyed by prompt substring, so a test says what it is answering
	// rather than depending on call order.
	picks    map[string]string
	multi    map[string][]string
	confirms map[string]bool
	inputs   map[string]string
	// errs forces a failure from a specific prompt (e.g. picker.ErrCancelled).
	errs map[string]error

	// asked records every prompt shown, in order, so a test can assert that a
	// question was or was not put to the user.
	asked []string
	// offered records the choices presented for each prompt, so a test can assert
	// what the user was actually given to choose from.
	offered map[string][]string
}

func newScriptedPrompter(t *testing.T) *scriptedPrompter {
	return &scriptedPrompter{
		t:           t,
		interactive: true,
		picks:       map[string]string{},
		multi:       map[string][]string{},
		confirms:    map[string]bool{},
		inputs:      map[string]string{},
		errs:        map[string]error{},
		offered:     map[string][]string{},
	}
}

// withPrompter installs a prompter for the duration of a test.
func withPrompter(t *testing.T, p Prompter) {
	t.Helper()
	original := prompter
	prompter = p
	t.Cleanup(func() { prompter = original })
}

func (s *scriptedPrompter) Interactive() bool { return s.interactive }

// match finds the scripted answer whose key is a substring of the prompt.
func match[T any](s *scriptedPrompter, kind, prompt string, table map[string]T) (T, bool) {
	s.asked = append(s.asked, prompt)
	for key, value := range table {
		if strings.Contains(prompt, key) {
			return value, true
		}
	}
	var zero T
	return zero, false
}

func (s *scriptedPrompter) PickOne(prompt string, choices []string) (string, error) {
	s.offered[prompt] = choices
	if err, ok := match(s, "err", prompt, s.errs); ok {
		return "", err
	}
	answer, ok := match(s, "pick", prompt, s.picks)
	if !ok {
		s.t.Fatalf("unscripted PickOne(%q) with choices %v", prompt, choices)
	}
	// The script names an answer; it must be one the user could actually choose.
	for _, c := range choices {
		if c == answer {
			return answer, nil
		}
	}
	s.t.Fatalf("scripted answer %q is not among the offered choices %v for %q", answer, choices, prompt)
	return "", nil
}

func (s *scriptedPrompter) PickMany(prompt string, choices []string) ([]string, error) {
	s.offered[prompt] = choices
	if err, ok := match(s, "err", prompt, s.errs); ok {
		return nil, err
	}
	answer, ok := match(s, "multi", prompt, s.multi)
	if !ok {
		s.t.Fatalf("unscripted PickMany(%q) with choices %v", prompt, choices)
	}
	for _, a := range answer {
		found := false
		for _, c := range choices {
			if c == a {
				found = true
				break
			}
		}
		if !found {
			s.t.Fatalf("scripted answer %q is not among the offered choices %v for %q", a, choices, prompt)
		}
	}
	return answer, nil
}

func (s *scriptedPrompter) Confirm(prompt string, defaultYes bool) bool {
	answer, ok := match(s, "confirm", prompt, s.confirms)
	if !ok {
		s.t.Fatalf("unscripted Confirm(%q)", prompt)
	}
	return answer
}

func (s *scriptedPrompter) Prompt(label, defaultValue string) string {
	answer, ok := match(s, "input", label, s.inputs)
	if !ok {
		s.t.Fatalf("unscripted Prompt(%q)", label)
	}
	return answer
}

// wasAsked reports whether any prompt contained the given substring.
func (s *scriptedPrompter) wasAsked(substr string) bool {
	for _, prompt := range s.asked {
		if strings.Contains(prompt, substr) {
			return true
		}
	}
	return false
}

// choicesFor returns the choices offered for the prompt containing substr.
func (s *scriptedPrompter) choicesFor(substr string) []string {
	for prompt, choices := range s.offered {
		if strings.Contains(prompt, substr) {
			return choices
		}
	}
	return nil
}

func (s *scriptedPrompter) askedList() string {
	return fmt.Sprintf("%v", s.asked)
}
