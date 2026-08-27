package apimeta

import (
	"encoding/json"
	"fmt"
	"os"
)

// Help is the English help text model for generated Cloud API commands.
type Help struct {
	APIVersion string                 `json:"api_version"`
	Commands   map[string]CommandHelp `json:"commands"`
}

// CommandHelp is the human-authored help text for one generated command.
type CommandHelp struct {
	Short             string               `json:"short,omitempty"`
	Long              string               `json:"long,omitempty"`
	PreserveFlagOrder bool                 `json:"preserve_flag_order,omitempty"`
	Examples          []string             `json:"examples,omitempty"`
	Fields            map[string]FieldHelp `json:"fields,omitempty"`
}

// FieldHelp is the help text attached to one API request field.
type FieldHelp struct {
	Description string               `json:"description,omitempty"`
	Inputs      map[string]InputHelp `json:"inputs,omitempty"`
}

// InputHelp is the help text attached to one generated flag or argument.
type InputHelp struct {
	Usage    string   `json:"usage,omitempty"`
	Format   string   `json:"format,omitempty"`
	Fields   []string `json:"fields,omitempty"`
	Examples []string `json:"examples,omitempty"`
	Values   []string `json:"values,omitempty"`
}

// LoadHelp reads a help.json file from disk.
func LoadHelp(path string) (*Help, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read help: %w", err)
	}
	return ParseHelp(data)
}

// ParseHelp parses raw help.json bytes.
func ParseHelp(data []byte) (*Help, error) {
	h := &Help{}
	if err := json.Unmarshal(data, h); err != nil {
		return nil, fmt.Errorf("parse help: %w", err)
	}
	if h.Commands == nil {
		h.Commands = map[string]CommandHelp{}
	}
	return h, nil
}

// GeneratedHelp returns the embedded generated help metadata.
func GeneratedHelp() *Help {
	var h Help
	if len(generatedHelpJSON) == 0 {
		return &Help{Commands: map[string]CommandHelp{}}
	}
	if err := json.Unmarshal(generatedHelpJSON, &h); err != nil {
		return &Help{Commands: map[string]CommandHelp{}}
	}
	if h.Commands == nil {
		h.Commands = map[string]CommandHelp{}
	}
	return &h
}

// CommandHelpFor returns help metadata for a generated command ID.
func CommandHelpFor(commandID string) CommandHelp {
	h := GeneratedHelp()
	return h.Commands[commandID]
}

// FieldDescription returns generated field help or fallback when no help exists.
func FieldDescription(commandID, fieldName, fallback string) string {
	ch := CommandHelpFor(commandID)
	if fh, ok := ch.Fields[fieldName]; ok && fh.Description != "" {
		return fh.Description
	}
	return fallback
}

// InputHelpFor returns generated input help or fallback when no help exists.
func InputHelpFor(commandID, fieldName, flagName, fallback string) InputHelp {
	ch := CommandHelpFor(commandID)
	if fh, ok := ch.Fields[fieldName]; ok {
		if ih, ok := fh.Inputs[flagName]; ok {
			if ih.Usage == "" {
				ih.Usage = fh.Description
			}
			if ih.Usage == "" {
				ih.Usage = fallback
			}
			return ih
		}
		if fh.Description != "" {
			return InputHelp{Usage: fh.Description}
		}
	}
	return InputHelp{Usage: fallback}
}
