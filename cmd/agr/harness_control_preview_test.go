//go:build preview

package main

import (
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
)

func TestHarnessCreateRejectsMalformedInitialization(t *testing.T) {
	fake := newHarnessFake(t)
	server := httptest.NewTLSServer(fake)
	defer server.Close()
	base := []string{"harness", "create", "--name", "agent", "--engine", "pi", "--engine-version", "1.3.0", "--model", "mp-a:hy3"}
	shortcuts := []string{"--role-arn", "r", "--subnet-id", "subnet-1", "--security-group-id", "sg-1"}
	for _, tc := range []struct {
		name, config, field, code string
		withShortcuts, dryRun     bool
	}{
		{"malformed JSON", `{bad`, "--initialize-configuration", "INVALID_JSON_FLAG", false, false},
		{"sandbox tool", `{"SandboxTool":"bad"}`, "SandboxTool", "INVALID_JSON_FLAG", true, false},
		{"network configuration", `{"SandboxTool":{"NetworkConfiguration":[]}}`, "SandboxTool.NetworkConfiguration", "INVALID_JSON_FLAG", true, false},
		{"VPC config", `{"SandboxTool":{"NetworkConfiguration":{"VpcConfig":true}}}`, "SandboxTool.NetworkConfiguration.VpcConfig", "INVALID_JSON_FLAG", true, false},
		{"role ARN", `{"SandboxTool":{"RoleArn":42}}`, "SandboxTool.RoleArn", "INVALID_JSON_FLAG", true, false},
		{"network mode", `{"SandboxTool":{"NetworkConfiguration":{"NetworkMode":true}}}`, "SandboxTool.NetworkConfiguration.NetworkMode", "INVALID_JSON_FLAG", true, false},
		{"subnet IDs", `{"SandboxTool":{"NetworkConfiguration":{"VpcConfig":{"SubnetIds":"subnet-1"}}}}`, "SubnetIds", "INVALID_JSON_FLAG", true, false},
		{"security group IDs in dry run", `{"SandboxTool":{"RoleArn":"r","NetworkConfiguration":{"VpcConfig":{"SubnetIds":["subnet-1"],"SecurityGroupIds":[123]}}}}`, "SecurityGroupIds", "INVALID_JSON_FLAG", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := append(slices.Clone(base), "--initialize-configuration", tc.config)
			if tc.withShortcuts {
				args = append(args, shortcuts...)
			}
			if tc.dryRun {
				args = append(args, "--dry-run")
			}
			result, _, err := runHarness(t, server, args...)
			if err == nil || failureCode(result) != tc.code || !strings.Contains(result.Failure.Message, tc.field) {
				t.Fatalf("error = %v, failure = %+v, want %s naming %s", err, result.Failure, tc.code, tc.field)
			}
		})
	}
	if calls := fake.actions(); len(calls) != 0 {
		t.Fatalf("invalid initialization reached the control plane: %v", calls)
	}
}

func TestHarnessStatusRecommendsProgressOfExistingAction(t *testing.T) {
	for _, tc := range []struct {
		name, state, next string
		revisions         []string
	}{
		{"rollback in progress", "ACTIVE", "agr harness revision publish hns-test 1 --wait", []string{"PUBLISHING", "PUBLISHED", "UNPUBLISHED"}},
		{"deletion in progress", "DELETING", "agr harness get hns-test", []string{"PUBLISHED"}},
		{"failed deletion can be retried", "DELETE_FAILED", "agr harness delete hns-test --yes --wait", []string{"PUBLISHED"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := newHarnessFake(t)
			fake.seedHarness(tc.revisions...)
			fake.harness["Status"] = tc.state
			fake.deleteReads = 1
			server := httptest.NewTLSServer(fake)
			defer server.Close()
			status, _, err := runHarness(t, server, "harness", "status", "hns-test")
			if err != nil || status.Data["Next"] != tc.next {
				t.Fatalf("status = %+v, err = %v, want Next = %q", status, err, tc.next)
			}
			if got := fake.actions(); !slices.Equal(got, []string{"DescribeHarness", "DescribeHarnessRevisionList"}) {
				t.Fatalf("status performed unexpected calls: %v", got)
			}
		})
	}
}
