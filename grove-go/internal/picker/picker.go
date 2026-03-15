// Package picker provides interactive selection menus using huh.
package picker

import (
	"fmt"
	"sort"

	"github.com/charmbracelet/huh"
)

// PickOne shows a single-select menu and returns the chosen value.
func PickOne(title string, options []string) (string, error) {
	if len(options) == 0 {
		return "", fmt.Errorf("no options available")
	}

	opts := make([]huh.Option[string], len(options))
	for i, o := range options {
		opts[i] = huh.NewOption(o, o)
	}

	var selected string
	err := huh.NewSelect[string]().
		Title(title).
		Options(opts...).
		Value(&selected).
		Run()

	return selected, err
}

// PickMany shows a multi-select menu and returns chosen values.
func PickMany(title string, options []string) ([]string, error) {
	if len(options) == 0 {
		return nil, fmt.Errorf("no options available")
	}

	sort.Strings(options)
	opts := make([]huh.Option[string], len(options))
	for i, o := range options {
		opts[i] = huh.NewOption(o, o)
	}

	var selected []string
	err := huh.NewMultiSelect[string]().
		Title(title).
		Options(opts...).
		Value(&selected).
		Run()

	return selected, err
}

// PickManyWithPresets shows a multi-select with named preset groupings.
func PickManyWithPresets(title string, options []string, presets map[string][]string) ([]string, error) {
	if len(presets) > 0 {
		// Add preset options at the top
		presetNames := make([]string, 0, len(presets))
		for name := range presets {
			presetNames = append(presetNames, name)
		}
		sort.Strings(presetNames)

		presetOptions := make([]string, len(presetNames))
		for i, name := range presetNames {
			presetOptions[i] = fmt.Sprintf("[preset] %s", name)
		}

		allOptions := append(presetOptions, options...)
		selected, err := PickMany(title, allOptions)
		if err != nil {
			return nil, err
		}

		// Expand presets
		var result []string
		seen := make(map[string]bool)
		for _, s := range selected {
			if len(s) > 9 && s[:9] == "[preset] " {
				presetName := s[9:]
				if repos, ok := presets[presetName]; ok {
					for _, r := range repos {
						if !seen[r] {
							result = append(result, r)
							seen[r] = true
						}
					}
				}
			} else if !seen[s] {
				result = append(result, s)
				seen[s] = true
			}
		}
		return result, nil
	}

	return PickMany(title, options)
}

// Confirm shows a yes/no confirmation prompt.
func Confirm(title string) (bool, error) {
	var confirmed bool
	err := huh.NewConfirm().
		Title(title).
		Value(&confirmed).
		Run()
	return confirmed, err
}

// Input shows a text input prompt.
func Input(title string) (string, error) {
	var value string
	err := huh.NewInput().
		Title(title).
		Value(&value).
		Run()
	return value, err
}
