//go:build preview

package harnesscli

import (
	"context"
	"errors"
	"fmt"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apicli"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apivalue"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
)

// Client issues the read calls shared by wrappers. It reuses the generated
// commands' control plane so requests follow the same transport and errors.
type Client struct {
	cp apicli.ControlPlane
}

// NewClient asserts the generic control-plane capability.
func NewClient(deps command.Deps, commandID string) (Client, error) {
	cp, ok := deps.ControlPlane.(apicli.ControlPlane)
	if !ok {
		return Client{}, fmt.Errorf("%s requires command.Deps.ControlPlane implementing apicli.ControlPlane", commandID)
	}
	return Client{cp: cp}, nil
}

func (c Client) object(ctx context.Context, action string, request map[string]any, field string) (apivalue.Object, error) {
	value, err := c.cp.Call(ctx, action, request)
	if err != nil {
		return nil, err
	}
	response, err := apivalue.Decode(value)
	if err != nil {
		return nil, err
	}
	result := response.Object(field)
	if result == nil {
		return nil, fmt.Errorf("%s response is missing %s", action, field)
	}
	return result, nil
}

func (c Client) Harness(ctx context.Context, harnessID string) (apivalue.Object, error) {
	return c.object(ctx, "DescribeHarness", map[string]any{"HarnessId": harnessID}, "Harness")
}

func (c Client) Revision(ctx context.Context, harnessID string, revision int64) (apivalue.Object, error) {
	return c.object(ctx, "DescribeHarnessRevision", map[string]any{"HarnessId": harnessID, "Revision": revision}, "HarnessRevision")
}

// Revisions reads every retained revision; the service keeps a small bounded
// history, so a few pages at most.
func (c Client) Revisions(ctx context.Context, harnessID string) ([]apivalue.Object, error) {
	var all []apivalue.Object
	for offset := 0; ; {
		value, err := c.cp.Call(ctx, "DescribeHarnessRevisionList", map[string]any{"HarnessId": harnessID, "Offset": offset, "Limit": 100})
		if err != nil {
			return nil, err
		}
		response, err := apivalue.Decode(value)
		if err != nil {
			return nil, err
		}
		page, err := response.ReadObjects("HarnessRevisionSet")
		if err != nil {
			return nil, err
		}
		all = append(all, page...)
		offset += len(page)
		if len(page) == 0 || int64(offset) >= response.Int64("TotalCount") {
			return all, nil
		}
	}
}

func (c Client) Engines(ctx context.Context) ([]apivalue.Object, error) {
	value, err := c.cp.Call(ctx, "DescribeHarnessEngineList", map[string]any{})
	if err != nil {
		return nil, err
	}
	response, err := apivalue.Decode(value)
	if err != nil {
		return nil, err
	}
	return response.ReadObjects("HarnessEngineSet")
}

// HarnessGetter adapts Harness for resourcewait.
func (c Client) HarnessGetter() func(context.Context, string) (apivalue.Object, error) {
	return c.Harness
}

// RevisionGetter adapts Revision for resourcewait, which passes a fixed ID.
func (c Client) RevisionGetter(harnessID string, revision int64) func(context.Context, string) (apivalue.Object, error) {
	return func(ctx context.Context, _ string) (apivalue.Object, error) {
		return c.Revision(ctx, harnessID, revision)
	}
}

// RevisionNumber normalizes the parsed Revision request value.
func RevisionNumber(value any) (int64, error) {
	var number int64
	switch v := value.(type) {
	case int:
		number = int64(v)
	case int64:
		number = v
	case float64:
		number = int64(v)
	case interface{ Int64() (int64, error) }:
		parsed, err := v.Int64()
		if err != nil {
			return 0, output.NewUsageError("INVALID_REVISION", fmt.Sprintf("revision %v is not an integer", value), "Use a revision number from 'agr harness revision list <harness-id>'.")
		}
		number = parsed
	}
	if number < 1 {
		return 0, output.NewUsageError("INVALID_REVISION", fmt.Sprintf("revision must be a positive integer, got %v", value), "Use a revision number from 'agr harness revision list <harness-id>'.")
	}
	return number, nil
}

// IsNotFound reports a structured not-found failure from the control plane.
func IsNotFound(err error) bool {
	var cliErr *output.CLIError
	return errors.As(err, &cliErr) && cliErr.Failure != nil && cliErr.Failure.Kind == output.KindNotFound
}

// LiveRevision returns the PUBLISHED revision and the newest revision, either
// of which may be nil.
func LiveRevision(revisions []apivalue.Object) (live, latest apivalue.Object) {
	for _, revision := range revisions {
		if latest == nil || revision.Int64("Revision") > latest.Int64("Revision") {
			latest = revision
		}
		if revision.String("Status") == "PUBLISHED" {
			live = revision
		}
	}
	return live, latest
}
