package webshell

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidateTTYDBinary(t *testing.T) {
	// Create a temporary directory for test files
	tmpDir, err := os.MkdirTemp("", "ttyd-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	tests := []struct {
		name        string
		setupFile   func() string
		expectError bool
		errorMsg    string
	}{
		{
			name: "file does not exist",
			setupFile: func() string {
				return filepath.Join(tmpDir, "nonexistent")
			},
			expectError: true,
			errorMsg:    "does not exist",
		},
		{
			name: "file too small",
			setupFile: func() string {
				path := filepath.Join(tmpDir, "small")
				err := os.WriteFile(path, []byte("small"), 0644)
				if err != nil {
					t.Fatalf("Failed to create small file: %v", err)
				}
				return path
			},
			expectError: true,
			errorMsg:    "too small",
		},
		{
			name: "file too large",
			setupFile: func() string {
				path := filepath.Join(tmpDir, "large")
				// Create a file larger than 50MB
				data := make([]byte, 51*1024*1024)
				err := os.WriteFile(path, data, 0644)
				if err != nil {
					t.Fatalf("Failed to create large file: %v", err)
				}
				return path
			},
			expectError: true,
			errorMsg:    "too large",
		},
		{
			name: "valid file size",
			setupFile: func() string {
				path := filepath.Join(tmpDir, "valid")
				// Create a file with valid size (2MB)
				data := make([]byte, 2*1024*1024)
				err := os.WriteFile(path, data, 0644)
				if err != nil {
					t.Fatalf("Failed to create valid file: %v", err)
				}
				return path
			},
			expectError: false,
		},
		{
			name: "directory instead of file",
			setupFile: func() string {
				path := filepath.Join(tmpDir, "dir")
				err := os.Mkdir(path, 0755)
				if err != nil {
					t.Fatalf("Failed to create directory: %v", err)
				}
				return path
			},
			expectError: true,
			errorMsg:    "not a regular file",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filePath := tt.setupFile()
			err := validateTTYDBinary(filePath)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error but got none")
				} else if tt.errorMsg != "" && !contains(err.Error(), tt.errorMsg) {
					t.Errorf("Expected error message to contain '%s', got: %v", tt.errorMsg, err)
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error but got: %v", err)
				}
			}
		})
	}
}

// Helper function to check if a string contains a substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > len(substr) && s[:len(substr)] == substr) ||
		(len(s) > len(substr) && s[len(s)-len(substr):] == substr) ||
		containsSubstring(s, substr))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
