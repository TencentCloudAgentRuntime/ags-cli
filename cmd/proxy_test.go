package cmd

import (
	"strings"
	"testing"
)

func TestParsePortSpec(t *testing.T) {
	tests := []struct {
		name        string
		spec        string
		wantLocal   int
		wantRemote  int
		wantErr     bool
		errContains string
	}{
		{
			name:       "single port",
			spec:       "3000",
			wantLocal:  3000,
			wantRemote: 3000,
		},
		{
			name:       "local:remote",
			spec:       "3000:8080",
			wantLocal:  3000,
			wantRemote: 8080,
		},
		{
			name:       "different ports",
			spec:       "9090:80",
			wantLocal:  9090,
			wantRemote: 80,
		},
		{
			name:       "max port",
			spec:       "65535",
			wantLocal:  65535,
			wantRemote: 65535,
		},
		{
			name:       "min port",
			spec:       "1",
			wantLocal:  1,
			wantRemote: 1,
		},
		{
			name:        "invalid single port",
			spec:        "abc",
			wantErr:     true,
			errContains: "invalid port",
		},
		{
			name:        "port zero",
			spec:        "0",
			wantErr:     true,
			errContains: "between 1 and 65535",
		},
		{
			name:        "port too large",
			spec:        "70000",
			wantErr:     true,
			errContains: "between 1 and 65535",
		},
		{
			name:        "negative port",
			spec:        "-1",
			wantErr:     true,
			errContains: "between 1 and 65535",
		},
		{
			name:        "invalid local port",
			spec:        "abc:3000",
			wantErr:     true,
			errContains: "invalid local port",
		},
		{
			name:        "invalid remote port",
			spec:        "3000:abc",
			wantErr:     true,
			errContains: "invalid remote port",
		},
		{
			name:        "local port zero",
			spec:        "0:3000",
			wantErr:     true,
			errContains: "local port must be between",
		},
		{
			name:        "remote port zero",
			spec:        "3000:0",
			wantErr:     true,
			errContains: "remote port must be between",
		},
		{
			name:        "local port too large",
			spec:        "70000:3000",
			wantErr:     true,
			errContains: "local port must be between",
		},
		{
			name:        "remote port too large",
			spec:        "3000:70000",
			wantErr:     true,
			errContains: "remote port must be between",
		},
		{
			name:        "empty spec",
			spec:        "",
			wantErr:     true,
			errContains: "invalid port",
		},
		{
			name:        "multiple colons treated as local:remote with bad remote",
			spec:        "8080:80:8080",
			wantErr:     true,
			errContains: "invalid remote port",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			local, remote, err := parsePortSpec(tt.spec)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tt.errContains != "" {
					if got := err.Error(); !strings.Contains(got, tt.errContains) {
						t.Errorf("error %q should contain %q", got, tt.errContains)
					}
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if local != tt.wantLocal {
				t.Errorf("localPort = %d, want %d", local, tt.wantLocal)
			}
			if remote != tt.wantRemote {
				t.Errorf("remotePort = %d, want %d", remote, tt.wantRemote)
			}
		})
	}
}
