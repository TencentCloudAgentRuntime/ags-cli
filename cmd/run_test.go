package cmd

import (
	"strings"
	"testing"
)

func TestValidateRunFlags(t *testing.T) {
	origInstance := runInstance
	origTool := runTool
	origRepeat := runRepeat
	origMaxParallel := runMaxParallel
	origLanguage := runLanguage
	t.Cleanup(func() {
		runInstance = origInstance
		runTool = origTool
		runRepeat = origRepeat
		runMaxParallel = origMaxParallel
		runLanguage = origLanguage
	})

	tests := []struct {
		name        string
		instance    string
		tool        string
		repeat      int
		maxParallel int
		language    string
		errContains string
	}{
		{
			name:        "valid defaults",
			tool:        "code-interpreter-v1",
			repeat:      1,
			maxParallel: 0,
			language:    "python",
		},
		{
			name:        "instance and custom tool",
			instance:    "sandbox-123",
			tool:        "other-tool",
			repeat:      1,
			language:    "python",
			errContains: "cannot specify both",
		},
		{
			name:        "repeat too small",
			tool:        "code-interpreter-v1",
			repeat:      0,
			language:    "python",
			errContains: "--repeat",
		},
		{
			name:        "negative max parallel",
			tool:        "code-interpreter-v1",
			repeat:      1,
			maxParallel: -1,
			language:    "python",
			errContains: "--max-parallel",
		},
		{
			name:        "unsupported language",
			tool:        "code-interpreter-v1",
			repeat:      1,
			language:    "ruby",
			errContains: "unsupported language",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runInstance = tt.instance
			runTool = tt.tool
			runRepeat = tt.repeat
			runMaxParallel = tt.maxParallel
			runLanguage = tt.language

			err := validateRunFlags()
			if tt.errContains == "" {
				if err != nil {
					t.Fatalf("expected no error, got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.errContains) {
				t.Fatalf("error %q should contain %q", err.Error(), tt.errContains)
			}
		})
	}
}
