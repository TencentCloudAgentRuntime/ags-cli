package pty

import "testing"

// TestNewSession_TokenPreserved ensures NewSession preserves the provided
// access token (including the empty-string "no auth" case) and domain.
func TestNewSession_TokenPreserved(t *testing.T) {
	tests := []struct {
		name        string
		accessToken string
		domain      string
	}{
		{
			name:        "with token",
			accessToken: "tok-abc",
			domain:      "ap-guangzhou.tencentags.com",
		},
		{
			name:        "empty token (no-auth mode)",
			accessToken: "",
			domain:      "ap-guangzhou.tencentags.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewSession(tt.accessToken, tt.domain)
			if s == nil {
				t.Fatal("NewSession returned nil")
			}
			if s.accessToken != tt.accessToken {
				t.Errorf("accessToken = %q, want %q", s.accessToken, tt.accessToken)
			}
			if s.domain != tt.domain {
				t.Errorf("domain = %q, want %q", s.domain, tt.domain)
			}
		})
	}
}

// TestSession_EnvdHost verifies that envdHost builds the expected
// region-qualified hostname for the given instance, and does not depend on
// the access token (so --no-auth mode still resolves the correct URL).
func TestSession_EnvdHost(t *testing.T) {
	tests := []struct {
		name        string
		accessToken string
		domain      string
		instanceID  string
		want        string
	}{
		{
			name:        "with token",
			accessToken: "tok-abc",
			domain:      "ap-guangzhou.tencentags.com",
			instanceID:  "sbi-123",
			want:        "49983-sbi-123.ap-guangzhou.tencentags.com",
		},
		{
			name:        "no-auth (empty token) produces same host",
			accessToken: "",
			domain:      "ap-guangzhou.tencentags.com",
			instanceID:  "sbi-123",
			want:        "49983-sbi-123.ap-guangzhou.tencentags.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewSession(tt.accessToken, tt.domain)
			got := s.envdHost(tt.instanceID)
			if got != tt.want {
				t.Errorf("envdHost(%q) = %q, want %q", tt.instanceID, got, tt.want)
			}
		})
	}
}
