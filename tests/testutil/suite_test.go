package testutil

import (
	"slices"
	"testing"
)

func TestBuildCLIBinaryCommandDisablesVCSStamping(t *testing.T) {
	cmd := buildCLIBinaryCommand("/tmp/agr")

	if !slices.Contains(cmd.Args, "-buildvcs=false") {
		t.Fatalf("expected build command to disable VCS stamping, got %v", cmd.Args)
	}
}
