package patchscenarios

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/patchtest"
)

var sandboxKeepaliveAssertions = []string{
	"keepalive.tool.create",
	"keepalive.tool.update",
	"keepalive.instance.create",
	"keepalive.instance.update",
	"keepalive.inheritance",
	"keepalive.readback",
	"keepalive.boundaries",
	"keepalive.fork",
}

var sandboxLogAssertions = []string{
	"log.raw-default",
	"log.auto",
	"log.normalization",
	"log.invalid",
	"log.readback",
	"log.fork",
}

const (
	// The keepalive scenario keeps the sandbox network. The service rejects
	// CLSConfig on it ("sidecar requires network access to push logs to CLS"),
	// so every tool that carries a CLS topic uses a network mode with egress.
	sandboxNetworkConfiguration    = `{"NetworkMode":"SANDBOX"}`
	sandboxLogNetworkConfiguration = `{"NetworkMode":"PUBLIC"}`
	sandboxToolDefaultTimeout      = "30m"
	sandboxInstanceTimeout         = "30m"
	// The service accepts LogConfiguration.CLSConfig only on custom tools, and
	// a custom tool needs a custom configuration that names its start command:
	// omitting Command is rejected with "CustomConfiguration.Command is
	// required" (observed live on 2026-11-17). A configuration the service
	// accepts can still stop at FAILED with "FailedOperation.DependencyUnavailable:
	// Internal server error" and no more specific cause, so the scenario pins the
	// whole configuration observed to reach ACTIVE instead of attributing the stop
	// to one member; the attempts that hit it also differed in image, registry
	// type, resources, ports and probe. That configuration has the shape of the
	// custom tools already running in this account: a public CCR image, started
	// through a shell that runs the agent binary, with the agent's health
	// endpoint declared as the probe and as a port.
	sandboxCustomConfiguration = `{"Image":"ccr.ccs.tencentyun.com/ags-image/envd:v0.6.13","ImageRegistryType":"personal","Command":["/bin/bash","-c","/usr/bin/envd"],"Resources":{"CPU":"2","Memory":"4Gi"},"Probe":{"HttpGet":{"Path":"/health","Port":49983,"Scheme":"HTTP"},"ReadyTimeoutMs":30000,"ProbeTimeoutMs":1000,"ProbePeriodMs":1000,"SuccessThreshold":1,"FailureThreshold":3},"Ports":[{"Name":"envd","Port":49983,"Protocol":"TCP"}]}`
)

// sandboxToolSpec declares the tool a scenario creates. The tool type is a
// documented precondition, not a shared default: the service rejects a CLS
// topic on the built-in types with "LogConfiguration with CLSConfig is only
// supported for custom sandbox type for now".
type sandboxToolSpec struct {
	toolType string
	network  string
	args     []string
}

var (
	sandboxKeepaliveTool = sandboxToolSpec{toolType: "code-interpreter", network: sandboxNetworkConfiguration}
	sandboxLogTool       = sandboxToolSpec{
		toolType: "custom",
		network:  sandboxLogNetworkConfiguration,
		args:     []string{"--custom-configuration", sandboxCustomConfiguration},
	}
)

// The waits are variables so the offline fixture test can shorten them; the
// strict patch runner keeps the production bounds.
var (
	// sandboxRunningDeadline bounds the single readiness wait. Configuration
	// readback never waits for the data plane.
	sandboxRunningDeadline = 8 * time.Minute
	sandboxGoneDeadline    = 90 * time.Second
	// Tool creation and forking are asynchronous: the members are readable only
	// after the copy is ACTIVE. The wait only reads.
	sandboxToolActiveDeadline = 5 * time.Minute
	sandboxPollInterval       = 3 * time.Second
)

// The sandbox scenarios reuse the CLI envelope and list decoding proven by the
// volume scenarios; only the domain steps below are new.
type sandboxEnvelope = volumeEnvelope

// sandboxDiagnostic writes one redacted line to the suite log. Live runs persist
// it next to the report, so a failing assertion can be classified from the run
// directory without exposing credentials or container payloads.
func sandboxDiagnostic(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "sandbox-diagnostic: "+format+"\n", args...)
}

// sandboxRunExpected runs a command whose failure is itself the assertion, so it
// stays out of the suite log.
func sandboxRunExpected(s *patchtest.Session, ctx context.Context, args ...string) (sandboxEnvelope, error) {
	return volumeCLI(s, ctx, args...)
}

// sandboxScenario wraps a scenario so a returned error or a recovered panic is
// written to the suite log. patchtest reports only "scenario X did not pass", so
// without this the cause of a failure never reaches the run log.
func sandboxScenario(id string, run func(*patchtest.Session) error) func(*patchtest.Session) error {
	return func(s *patchtest.Session) (err error) {
		defer func() {
			if value := recover(); value != nil {
				sandboxDiagnostic("%s panicked: %v", id, value)
				err = fmt.Errorf("scenario panicked")
			}
		}()
		if runErr := run(s); runErr != nil {
			sandboxDiagnostic("%s returned: %v", id, runErr)
			return runErr
		}
		return nil
	}
}

func sandboxRun(s *patchtest.Session, ctx context.Context, args ...string) (sandboxEnvelope, error) {
	envelope, err := sandboxRunExpected(s, ctx, args...)
	if err != nil {
		sandboxDiagnostic("cli %s: %v", strings.Join(args, " "), err)
	}
	return envelope, err
}

// sandboxDiagnoseKeepalive reports the observed configuration members in a log
// line so a failing live assertion says which member diverged.
// sandboxFailureDetail reads a failure detail without dereferencing an absent
// failure object, which a successful response legitimately omits.
func sandboxFailureDetail(envelope sandboxEnvelope, key string) string {
	if envelope.Failure == nil {
		return ""
	}
	return volumeString(envelope.Failure.Details, key)
}

// sandboxFailureSummary renders a failure for a diagnostic line without leaking
// a raw response body.
func sandboxFailureSummary(envelope sandboxEnvelope) string {
	if envelope.Failure == nil {
		return "accepted with no failure"
	}
	return fmt.Sprintf("code=%s message=%s", envelope.Failure.Code, envelope.Failure.Message)
}

// sandboxParameterRejection reports whether the call failed with the service's
// parameter-validation answer, which is either a value rejection or a missing
// member. An internal error, an expired credential or a transport failure is not
// evidence that the service validated the value, so it must never satisfy a
// rejection assertion.
func sandboxParameterRejection(callErr error, envelope sandboxEnvelope) bool {
	if callErr == nil || envelope.Failure == nil {
		return false
	}
	code := envelope.Failure.Code
	return strings.HasPrefix(code, "InvalidParameter") || strings.HasPrefix(code, "MissingParameter")
}

// sandboxKeepaliveInvalidRequests lists the keepalive requests the contract
// rejects. An explicit null object or null member is a rejected value, not a
// no-op, exactly like an out-of-range timeout; the scenario asserts that the
// service answers each of them with a parameter error.
func sandboxKeepaliveInvalidRequests() []map[string]any {
	return []map[string]any{
		{"KeepaliveConfiguration": nil},
		{"KeepaliveConfiguration": map[string]any{"KeepaliveMode": nil}},
		{"KeepaliveConfiguration": map[string]any{"KeepaliveTimeout": nil}},
		{"KeepaliveConfiguration": map[string]any{"KeepaliveTimeout": ""}},
		{"KeepaliveConfiguration": map[string]any{"KeepaliveMode": "INVALID"}},
		{"KeepaliveConfiguration": map[string]any{"KeepaliveTimeout": 600}},
		{"KeepaliveConfiguration": map[string]any{"KeepaliveTimeout": "600"}},
		{"KeepaliveConfiguration": map[string]any{"KeepaliveTimeout": "-600s"}},
		{"KeepaliveConfiguration": map[string]any{"KeepaliveTimeout": "600.5s"}},
		{"KeepaliveConfiguration": map[string]any{"KeepaliveTimeout": "299s"}},
		{"KeepaliveConfiguration": map[string]any{"KeepaliveTimeout": "1801s"}},
	}
}

func sandboxDiagnoseKeepalive(label string, object map[string]any) {
	mode, timeout, present := sandboxKeepaliveMembers(object)
	pause, pauseSet := sandboxBool(object, "AutoPause")
	resume, resumeSet := sandboxBool(object, "AutoResume")
	sandboxDiagnostic("%s: present=%v mode=%q timeout=%q autoPause=%v(set=%v) autoResume=%v(set=%v) status=%q",
		label, present, mode, timeout, pause, pauseSet, resume, resumeSet, sandboxString(object, "Status"))
}

func sandboxObject(value any) map[string]any { return volumeObject(value) }

func sandboxString(object map[string]any, key string) string { return volumeString(object, key) }

func sandboxNested(object map[string]any, path ...string) map[string]any {
	return volumeNested(object, path...)
}

func sandboxBool(object map[string]any, key string) (bool, bool) {
	value, ok := object[key].(bool)
	return value, ok
}

func sandboxUniqueName(prefix string) string {
	return fmt.Sprintf("agr-%s-%d", prefix, time.Now().UnixNano())
}

// sandboxKeepaliveMatches compares a readback with the effective members. The
// timeout is compared verbatim: the service echoes the accepted source spelling
// (600s, 5m, 30m) instead of a client-side normalization.
func sandboxKeepaliveMatches(object map[string]any, mode, timeout string, autoPause, autoResume bool) bool {
	configuration := sandboxNested(object, "KeepaliveConfiguration")
	pause, pauseSet := sandboxBool(object, "AutoPause")
	resume, resumeSet := sandboxBool(object, "AutoResume")
	return sandboxString(configuration, "KeepaliveMode") == mode &&
		sandboxString(configuration, "KeepaliveTimeout") == timeout &&
		pauseSet && resumeSet && pause == autoPause && resume == autoResume
}

// sandboxKeepaliveMembersMatch is sandboxKeepaliveMatches with an optional
// member: an empty mode means "the service echoed no mode".
func sandboxKeepaliveMembersMatch(object map[string]any, mode, timeout string, autoPause, autoResume bool) bool {
	if mode != "" && !sandboxKeepaliveMatches(object, mode, timeout, autoPause, autoResume) {
		return false
	}
	configuration := sandboxNested(object, "KeepaliveConfiguration")
	pause, pauseSet := sandboxBool(object, "AutoPause")
	resume, resumeSet := sandboxBool(object, "AutoResume")
	return sandboxString(configuration, "KeepaliveTimeout") == timeout &&
		pauseSet && resumeSet && pause == autoPause && resume == autoResume
}

// sandboxKeepaliveAbsent accepts a missing or null object: the service must not
// fabricate the platform default when no source configured keepalive.
func sandboxKeepaliveAbsent(object map[string]any) bool {
	value, present := object["KeepaliveConfiguration"]
	return !present || value == nil
}

// sandboxKeepaliveMembers reports the resolved keepalive members and whether the
// object was returned at all.
func sandboxKeepaliveMembers(object map[string]any) (mode, timeout string, present bool) {
	value, ok := object["KeepaliveConfiguration"]
	if !ok || value == nil {
		return "", "", false
	}
	configuration := sandboxObject(value)
	return sandboxString(configuration, "KeepaliveMode"), sandboxString(configuration, "KeepaliveTimeout"), true
}

// sandboxBoolPair renders a bool member for a diagnostic line as "value/present".
func sandboxBoolPair(object map[string]any, key string) string {
	value, present := sandboxBool(object, key)
	return fmt.Sprintf("%v/%v", value, present)
}

// sandboxBoolOff reports a bool the service did not enable. #172 only promises
// that an omitted request member does not overwrite the stored value; it does not
// promise the response drops the member, so an echoed false and an absent member
// are both acceptable here. That an omitted member is not sent at all is proven
// on the request side by the control-plane and command tests.
func sandboxBoolOff(object map[string]any, key string) bool {
	value, present := sandboxBool(object, key)
	return !present || !value
}

// sandboxOwnTool registers cleanup for a tool immediately after its ID is known,
// including IDs returned on a non-success envelope.
func sandboxOwnTool(s *patchtest.Session, toolID string) {
	s.Cleanup(func(ctx context.Context) error {
		if _, err := sandboxRun(s, ctx, "tool", "delete", toolID); err != nil {
			return err
		}
		return sandboxToolGone(s, ctx, toolID)
	})
}

func sandboxOwnInstance(s *patchtest.Session, instanceID string) {
	s.Cleanup(func(ctx context.Context) error {
		if _, err := sandboxRun(s, ctx, "instance", "delete", instanceID); err != nil {
			return err
		}
		return sandboxInstanceGone(s, ctx, instanceID)
	})
}

// sandboxList decodes the list contract shared by tool and instance list: the
// rows live under Items and the total under Pagination.Total.
func sandboxList(envelope sandboxEnvelope) ([]any, int64, error) {
	rows, ok := envelope.Data["Items"].([]any)
	if !ok {
		return nil, 0, errors.New("list response is missing Items")
	}
	count, ok := sandboxObject(envelope.Data["Pagination"])["Total"].(json.Number)
	if !ok {
		return nil, 0, errors.New("list response is missing Pagination.Total")
	}
	total, err := count.Int64()
	if err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

func sandboxToolGone(s *patchtest.Session, ctx context.Context, toolID string) error {
	deadline := time.Now().Add(sandboxGoneDeadline)
	for {
		envelope, err := sandboxRun(s, ctx, "tool", "list", "--tool-ids", toolID)
		if err == nil {
			rows, total, listErr := sandboxList(envelope)
			if listErr == nil && total == 0 && len(rows) == 0 {
				return nil
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("tool %s is still listed after deletion", toolID)
		}
		if err := sandboxSleep(ctx); err != nil {
			return err
		}
	}
}

// sandboxInstanceGone accepts the contract's terminal outcomes: the instance is
// absent from the list or reports a stopped terminal status.
func sandboxInstanceGone(s *patchtest.Session, ctx context.Context, instanceID string) error {
	deadline := time.Now().Add(sandboxGoneDeadline)
	for {
		envelope, err := sandboxRun(s, ctx, "instance", "list", "--instance-ids", instanceID)
		if err == nil {
			rows, _, listErr := sandboxList(envelope)
			if listErr == nil {
				if len(rows) == 0 {
					return nil
				}
				switch sandboxString(sandboxObject(rows[0]), "Status") {
				case "STOPPED", "TERMINATED":
					return nil
				}
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("instance %s is neither absent nor stopped after deletion", instanceID)
		}
		if err := sandboxSleep(ctx); err != nil {
			return err
		}
	}
}

func sandboxSleep(ctx context.Context) error {
	timer := time.NewTimer(sandboxPollInterval)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// sandboxWaitRunning proves the instance reached the lifecycle state the
// configuration readback was taken against. It only reads.
func sandboxWaitRunning(s *patchtest.Session, ctx context.Context, instanceID string) error {
	deadline := time.Now().Add(sandboxRunningDeadline)
	for {
		envelope, err := sandboxRun(s, ctx, "instance", "get", instanceID)
		if err != nil {
			return err
		}
		switch status := sandboxString(envelope.Data, "Status"); status {
		case "RUNNING":
			return nil
		case "STOPPED", "TERMINATED", "FAILED":
			return fmt.Errorf("instance %s reached %s instead of RUNNING", instanceID, status)
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("instance %s did not reach RUNNING before the deadline", instanceID)
		}
		if err := sandboxSleep(ctx); err != nil {
			return err
		}
	}
}

// sandboxWaitToolActive proves an asynchronous tool create or fork became
// readable before the configuration is read back. It only reads.
func sandboxWaitToolActive(s *patchtest.Session, ctx context.Context, toolID string) error {
	deadline := time.Now().Add(sandboxToolActiveDeadline)
	for {
		rows, total, err := sandboxToolRows(s, ctx, toolID)
		if err == nil && total >= 1 {
			if row := sandboxToolRow(rows, toolID); row != nil {
				switch status := sandboxString(row, "Status"); status {
				case "ACTIVE":
					return nil
				case "FAILED", "DELETED":
					reason := sandboxToolStatusReason(row)
					sandboxDiagnoseTool(fmt.Sprintf("tool %s reached %s", toolID, status), row)
					return fmt.Errorf("tool %s reached %s instead of ACTIVE: reason=%s", toolID, status, reason)
				}
			}
		}
		if time.Now().After(deadline) {
			sandboxDiagnostic("tool %s did not become ACTIVE before the deadline", toolID)
			if rows, total, err := sandboxToolRows(s, ctx, toolID); err == nil && total >= 1 {
				if row := sandboxToolRow(rows, toolID); row != nil {
					sandboxDiagnoseTool(fmt.Sprintf("tool %s still not ACTIVE at the deadline", toolID), row)
				}
			}
			return fmt.Errorf("tool %s did not reach ACTIVE before the deadline", toolID)
		}
		if err := sandboxSleep(ctx); err != nil {
			return err
		}
	}
}

// sandboxToolStatusReason renders the service's own explanation for a tool that
// did not become ACTIVE. The reason field is the only place the service states
// why provisioning stopped, so its absence is reported rather than guessed.
func sandboxToolStatusReason(row map[string]any) string {
	if reason := strings.TrimSpace(sandboxString(row, "StatusReason")); reason != "" {
		return reason
	}
	return "the service returned no StatusReason"
}

// sandboxDiagnoseTool reports why a tool did not become ACTIVE together with the
// preconditions the scenario chose, so a rejection can be attributed to the tool
// type, the network, the image or the log sidecar instead of costing another
// live round. It prints no credential, topic id or request payload.
func sandboxDiagnoseTool(label string, row map[string]any) {
	custom := sandboxObject(row["CustomConfiguration"])
	log := sandboxObject(row["LogConfiguration"])
	sandboxDiagnostic(`%s: Status=%q reason=%q ToolType=%q networkMode=%q logContentFormat=%q logCLSConfigPresent=%v image=%q command=%q args=%q`,
		label,
		sandboxString(row, "Status"),
		sandboxToolStatusReason(row),
		sandboxString(row, "ToolType"),
		sandboxString(sandboxObject(row["NetworkConfiguration"]), "NetworkMode"),
		sandboxString(log, "ContentFormat"),
		sandboxObject(log["CLSConfig"]) != nil,
		sandboxString(custom, "Image"),
		sandboxJoinedList(custom["Command"]),
		sandboxJoinedList(custom["Args"]))
}

// sandboxJoinedList renders a string list from a response member for a log line.
func sandboxJoinedList(value any) string {
	items, ok := value.([]any)
	if !ok {
		return ""
	}
	parts := make([]string, 0, len(items))
	for _, item := range items {
		parts = append(parts, fmt.Sprintf("%v", item))
	}
	return strings.Join(parts, " ")
}

func sandboxCreateTool(s *patchtest.Session, ctx context.Context, name string, tool sandboxToolSpec, extra ...string) (string, error) {
	args := []string{"tool", "create", "-n", name, "-t", tool.toolType,
		"--network-configuration", tool.network,
		"--persistent=false", "--default-timeout", sandboxToolDefaultTimeout}
	args = append(args, tool.args...)
	envelope, err := sandboxRun(s, ctx, append(args, extra...)...)
	toolID := sandboxString(envelope.Data, "ToolId")
	if toolID == "" {
		toolID = sandboxFailureDetail(envelope, "ResourceId")
	}
	if toolID != "" {
		sandboxOwnTool(s, toolID)
	}
	if err != nil {
		return toolID, err
	}
	if toolID == "" {
		return "", errors.New("tool create returned no ToolId")
	}
	return toolID, sandboxWaitToolActive(s, ctx, toolID)
}

func sandboxGetTool(s *patchtest.Session, ctx context.Context, toolID string) (map[string]any, error) {
	envelope, err := sandboxRun(s, ctx, "tool", "get", toolID)
	if err != nil {
		return nil, err
	}
	return envelope.Data, nil
}

func sandboxCreateInstance(s *patchtest.Session, ctx context.Context, toolID string, extra ...string) (string, map[string]any, error) {
	args := []string{"instance", "create", "--tool-id", toolID, "--timeout", sandboxInstanceTimeout}
	envelope, err := sandboxRun(s, ctx, append(args, extra...)...)
	instance := sandboxObject(envelope.Data["Instance"])
	instanceID := sandboxString(instance, "InstanceId")
	if instanceID == "" {
		instanceID = sandboxFailureDetail(envelope, "ResourceId")
	}
	if instanceID != "" {
		sandboxOwnInstance(s, instanceID)
	}
	if err != nil {
		return instanceID, nil, err
	}
	if instanceID == "" {
		return "", nil, errors.New("instance create returned no InstanceId")
	}
	return instanceID, instance, nil
}

func sandboxGetInstance(s *patchtest.Session, ctx context.Context, instanceID string) (map[string]any, error) {
	envelope, err := sandboxRun(s, ctx, "instance", "get", instanceID)
	if err != nil {
		return nil, err
	}
	return envelope.Data, nil
}

// sandboxRawCall reaches the service through the candidate's raw pass-through so
// service-side validation is exercised without widening the CLI contract.
func sandboxRawCall(s *patchtest.Session, ctx context.Context, action string, request map[string]any) (sandboxEnvelope, error) {
	payload, err := json.Marshal(request)
	if err != nil {
		return sandboxEnvelope{}, err
	}
	return sandboxRunExpected(s, ctx, "api", "call", action, "--request", string(payload))
}

func sandboxRawResponseData(envelope sandboxEnvelope) (map[string]any, bool) {
	data := sandboxObject(envelope.Data["Response"])
	for data["Response"] != nil {
		data = sandboxObject(data["Response"])
	}
	if len(data) == 0 {
		return nil, false
	}
	return data, true
}

func sandboxToolRows(s *patchtest.Session, ctx context.Context, toolIDs ...string) ([]any, int64, error) {
	args := []string{"tool", "list"}
	for _, id := range toolIDs {
		args = append(args, "--tool-ids", id)
	}
	envelope, err := sandboxRun(s, ctx, args...)
	if err != nil {
		return nil, 0, err
	}
	return sandboxList(envelope)
}

func sandboxToolRow(rows []any, toolID string) map[string]any {
	for _, row := range rows {
		if tool := sandboxObject(row); sandboxString(tool, "ToolId") == toolID {
			return tool
		}
	}
	return nil
}

func sandboxInstanceRows(s *patchtest.Session, ctx context.Context, instanceID string) ([]any, error) {
	envelope, err := sandboxRun(s, ctx, "instance", "list", "--instance-ids", instanceID)
	if err != nil {
		return nil, err
	}
	rows, _, err := sandboxList(envelope)
	return rows, err
}

func sandboxInstanceRow(rows []any, instanceID string) map[string]any {
	return volumeRow(rows, "InstanceId", instanceID)
}

// sandboxEqualKeepalive compares only the keepalive object of two readbacks.
func sandboxEqualKeepalive(left, right map[string]any) bool {
	leftConfiguration := sandboxNested(left, "KeepaliveConfiguration")
	rightConfiguration := sandboxNested(right, "KeepaliveConfiguration")
	keys := make([]string, 0, len(leftConfiguration))
	for key := range leftConfiguration {
		keys = append(keys, key)
	}
	if len(keys) != len(rightConfiguration) {
		return false
	}
	slices.Sort(keys)
	for _, key := range keys {
		if leftConfiguration[key] != rightConfiguration[key] {
			return false
		}
	}
	return true
}

func runSandboxKeepalive(s *patchtest.Session) error {
	ctx := s.Context
	toolName := sandboxUniqueName("keepalive")
	toolID, err := sandboxCreateTool(s, ctx, toolName, sandboxKeepaliveTool,
		"--keepalive-configuration", `{"KeepaliveMode":"REQUEST","KeepaliveTimeout":"600s"}`,
		"--auto-pause", "--auto-resume")
	if err != nil {
		return err
	}
	created, err := sandboxGetTool(s, ctx, toolID)
	if err != nil {
		return err
	}
	createdStep := sandboxString(created, "ToolName") == toolName &&
		sandboxKeepaliveMatches(created, "REQUEST", "600s", true, true)
	if !createdStep {
		sandboxDiagnostic("tool create step: nameMatches=%v", sandboxString(created, "ToolName") == toolName)
		sandboxDiagnoseKeepalive("created tool", created)
	}
	if err := s.Assert("keepalive.tool.create", createdStep); err != nil {
		return err
	}

	inheritedID, inherited, err := sandboxCreateInstance(s, ctx, toolID)
	if err != nil {
		return err
	}
	// an empty override means no override; the tool value keeps applying
	emptyID, empty, err := sandboxCreateInstance(s, ctx, toolID, "--keepalive-configuration", "{}")
	if err != nil {
		return err
	}
	modeOnlyID, modeOnly, err := sandboxCreateInstance(s, ctx, toolID,
		"--keepalive-configuration", `{"KeepaliveMode":"NONE"}`)
	if err != nil {
		return err
	}
	timeoutOnlyID, timeoutOnly, err := sandboxCreateInstance(s, ctx, toolID,
		"--keepalive-configuration", `{"KeepaliveTimeout":"5m"}`)
	if err != nil {
		return err
	}
	overrideID, override, err := sandboxCreateInstance(s, ctx, toolID,
		"--keepalive-configuration", `{"KeepaliveMode":"CONNECTION","KeepaliveTimeout":"30m"}`)
	if err != nil {
		return err
	}
	createStep := sandboxKeepaliveMatches(inherited, "REQUEST", "600s", true, true) &&
		sandboxKeepaliveMatches(empty, "REQUEST", "600s", true, true) &&
		sandboxKeepaliveMatches(override, "CONNECTION", "30m", true, true) &&
		sandboxString(inherited, "InstanceId") == inheritedID &&
		sandboxString(override, "InstanceId") == overrideID
	if !createStep {
		sandboxDiagnoseKeepalive("inherited instance", inherited)
		sandboxDiagnoseKeepalive("empty override instance", empty)
		sandboxDiagnoseKeepalive("full override instance", override)
	}
	if err := s.Assert("keepalive.instance.create", createStep); err != nil {
		return err
	}
	// Before the tool update every omitted member follows the tool value.
	inheritanceStep := sandboxKeepaliveMatches(modeOnly, "NONE", "600s", true, true) &&
		sandboxKeepaliveMatches(timeoutOnly, "REQUEST", "5m", true, true)
	if !inheritanceStep {
		sandboxDiagnoseKeepalive("mode-only instance", modeOnly)
		sandboxDiagnoseKeepalive("timeout-only instance", timeoutOnly)
	}
	if err := s.Assert("keepalive.inheritance", inheritanceStep); err != nil {
		return err
	}
	if err := sandboxWaitRunning(s, ctx, inheritedID); err != nil {
		return err
	}

	if _, err := sandboxRun(s, ctx, "tool", "update", toolID,
		"--keepalive-configuration", `{"KeepaliveMode":"MIXED","KeepaliveTimeout":"900s"}`); err != nil {
		return err
	}
	updated, err := sandboxGetTool(s, ctx, toolID)
	if err != nil {
		return err
	}
	if !sandboxKeepaliveMatches(updated, "MIXED", "900s", true, true) {
		sandboxDiagnoseKeepalive("updated tool", updated)
	}
	if err := s.Assert("keepalive.tool.update", sandboxKeepaliveMatches(updated, "MIXED", "900s", true, true)); err != nil {
		return err
	}
	// Every omitted member follows the new tool value; explicit members keep the
	// instance value and the accepted spelling stays verbatim.
	followed := true
	for _, expected := range []struct {
		id, mode, timeout string
	}{
		{inheritedID, "MIXED", "900s"},
		{emptyID, "MIXED", "900s"},
		{modeOnlyID, "NONE", "900s"},
		{timeoutOnlyID, "MIXED", "5m"},
		{overrideID, "CONNECTION", "30m"},
	} {
		instance, err := sandboxGetInstance(s, ctx, expected.id)
		if err != nil {
			return err
		}
		if !sandboxKeepaliveMatches(instance, expected.mode, expected.timeout, true, true) {
			sandboxDiagnostic("followed step: instance %s wants %s/%s", expected.id, expected.mode, expected.timeout)
			sandboxDiagnoseKeepalive("followed instance", instance)
			followed = false
		}
	}
	if err := s.Assert("keepalive.inheritance", followed); err != nil {
		return err
	}

	// Instance update sets keepalive and must carry explicit false booleans.
	if _, err := sandboxRun(s, ctx, "instance", "update", modeOnlyID,
		"--keepalive-configuration", `{"KeepaliveMode":"CONNECTION"}`,
		"--auto-pause=false", "--auto-resume=false"); err != nil {
		return err
	}
	modeOnly, err = sandboxGetInstance(s, ctx, modeOnlyID)
	if err != nil {
		return err
	}
	if !sandboxKeepaliveMatches(modeOnly, "CONNECTION", "900s", false, false) {
		sandboxDiagnoseKeepalive("instance update instance", modeOnly)
	}
	if err := s.Assert("keepalive.instance.update",
		sandboxKeepaliveMatches(modeOnly, "CONNECTION", "900s", false, false)); err != nil {
		return err
	}

	// Boundaries: the accepted minimum and maximum are echoed verbatim.
	if _, err := sandboxRun(s, ctx, "instance", "update", timeoutOnlyID,
		"--keepalive-configuration", `{"KeepaliveMode":"MIXED","KeepaliveTimeout":"300s"}`,
		"--auto-pause", "--auto-resume"); err != nil {
		return err
	}
	timeoutOnly, err = sandboxGetInstance(s, ctx, timeoutOnlyID)
	if err != nil {
		return err
	}
	minimumAccepted := sandboxKeepaliveMatches(timeoutOnly, "MIXED", "300s", true, true)
	if _, err := sandboxRun(s, ctx, "instance", "update", timeoutOnlyID,
		"--keepalive-configuration", `{"KeepaliveTimeout":"1800s"}`); err != nil {
		return err
	}
	timeoutOnly, err = sandboxGetInstance(s, ctx, timeoutOnlyID)
	if err != nil {
		return err
	}
	maxAccepted := sandboxKeepaliveMatches(timeoutOnly, "MIXED", "1800s", true, true)
	if !minimumAccepted || !maxAccepted {
		sandboxDiagnostic("boundaries step: minimumAccepted=%v maxAccepted=%v", minimumAccepted, maxAccepted)
		sandboxDiagnoseKeepalive("boundary instance", timeoutOnly)
	}
	if err := s.Assert("keepalive.boundaries", minimumAccepted && maxAccepted); err != nil {
		return err
	}

	// A tool without keepalive and with AutoPause=false yields no object, while
	// an instance that enables AutoPause gets the documented platform default.
	bareID, err := sandboxCreateTool(s, ctx, sandboxUniqueName("keepalive-bare"),
		sandboxKeepaliveTool, "--auto-pause=false")
	if err != nil {
		return err
	}
	bare, err := sandboxGetTool(s, ctx, bareID)
	if err != nil {
		return err
	}
	bareInstanceID, bareInstance, err := sandboxCreateInstance(s, ctx, bareID)
	if err != nil {
		return err
	}
	// Neither bool was enabled, so "off or not sent" is the only claim here; the
	// explicit false the CLI sent is echoed-checked on the instance update below.
	bareToolClean := sandboxKeepaliveAbsent(bare) && sandboxBoolOff(bare, "AutoPause") &&
		sandboxBoolOff(bare, "AutoResume")
	bareInstanceClean := sandboxKeepaliveAbsent(bareInstance) && sandboxBoolOff(bareInstance, "AutoPause") &&
		sandboxBoolOff(bareInstance, "AutoResume")
	if !bareToolClean || !bareInstanceClean {
		sandboxDiagnostic("bare step: toolClean=%v instanceClean=%v toolPause=%s toolResume=%s instancePause=%s instanceResume=%s",
			bareToolClean, bareInstanceClean,
			sandboxBoolPair(bare, "AutoPause"), sandboxBoolPair(bare, "AutoResume"),
			sandboxBoolPair(bareInstance, "AutoPause"), sandboxBoolPair(bareInstance, "AutoResume"))
		sandboxDiagnoseKeepalive("bare tool", bare)
		sandboxDiagnoseKeepalive("bare instance", bareInstance)
	}
	if err := s.Assert("keepalive.inheritance", bareToolClean && bareInstanceClean); err != nil {
		return err
	}
	defaultID, defaultInstance, err := sandboxCreateInstance(s, ctx, bareID, "--auto-pause=true")
	if err != nil {
		return err
	}
	// AutoResume was never configured, so the documented platform default
	// REQUEST/10m is the only keepalive source here.
	mode, timeout, present := sandboxKeepaliveMembers(defaultInstance)
	defaultPause, _ := sandboxBool(defaultInstance, "AutoPause")
	defaultStep := sandboxString(defaultInstance, "InstanceId") == defaultID &&
		present && mode == "REQUEST" && timeout == "10m" &&
		defaultPause && sandboxBoolOff(defaultInstance, "AutoResume")
	if !defaultStep {
		sandboxDiagnostic("default step: idMatches=%v keepalive=%v mode=%q timeout=%q autoPause=%s autoResume=%s",
			sandboxString(defaultInstance, "InstanceId") == defaultID, present, mode, timeout,
			sandboxBoolPair(defaultInstance, "AutoPause"), sandboxBoolPair(defaultInstance, "AutoResume"))
		sandboxDiagnoseKeepalive("default instance", defaultInstance)
	}
	if err := s.Assert("keepalive.inheritance", defaultStep); err != nil {
		return err
	}
	defaultReadback, err := sandboxGetInstance(s, ctx, defaultID)
	if err != nil {
		return err
	}
	readbackMode, readbackTimeout, readbackPresent := sandboxKeepaliveMembers(defaultReadback)
	if !readbackPresent || readbackMode != "REQUEST" || readbackTimeout != "10m" {
		sandboxDiagnostic("default readback: present=%v mode=%q timeout=%q", readbackPresent, readbackMode, readbackTimeout)
		sandboxDiagnoseKeepalive("default readback", defaultReadback)
	}
	if err := s.Assert("keepalive.inheritance",
		readbackPresent && readbackMode == "REQUEST" && readbackTimeout == "10m"); err != nil {
		return err
	}
	if _, err := sandboxGetInstance(s, ctx, bareInstanceID); err != nil {
		return err
	}

	// Fork copies the tool configuration and honours explicit overrides. The
	// helper records the returned id before it propagates a failure, so a fork
	// that fails after creating a tool is still cleaned up.
	forked, err := sandboxForkTool(s, ctx, toolID, sandboxUniqueName("keepalive-fork"))
	if err != nil {
		return err
	}
	overrideForked, err := sandboxForkTool(s, ctx, toolID, sandboxUniqueName("keepalive-fork-override"),
		"--keepalive-configuration", `{"KeepaliveTimeout":"1800s"}`, "--auto-pause=false")
	if err != nil {
		return err
	}
	// The override fork sends only a timeout, so the service echoes only that
	// member: the mode of the source tool is an inheritance input, not a
	// synthesized member of the forked tool.
	forkCopied := sandboxKeepaliveMatches(forked, "MIXED", "900s", true, true)
	forkOverridden := sandboxKeepaliveMembersMatch(overrideForked, "", "1800s", false, true)
	if !forkCopied || !forkOverridden {
		sandboxDiagnoseKeepalive("fork copy", forked)
		sandboxDiagnoseKeepalive("fork override", overrideForked)
	}
	if err := s.Assert("keepalive.fork", forkCopied && forkOverridden); err != nil {
		return err
	}

	// JSON readback through get/list and the raw query actions, plus text columns.
	rows, total, err := sandboxToolRows(s, ctx, toolID)
	if err != nil {
		return err
	}
	listed := sandboxToolRow(rows, toolID)
	rawEnvelope, rawErr := sandboxRawCall(s, ctx, "DescribeSandboxInstances", map[string]any{
		"InstanceIds": []string{inheritedID},
	})
	rawResponse, rawOK := sandboxRawResponseData(rawEnvelope)
	rawRows, _ := rawResponse["InstanceSet"].([]any)
	rawRow := sandboxInstanceRow(rawRows, inheritedID)
	instanceRows, err := sandboxInstanceRows(s, ctx, inheritedID)
	if err != nil {
		return err
	}
	listedInstance := sandboxInstanceRow(instanceRows, inheritedID)
	toolText, err := s.CLI(ctx, "tool", "get", toolID)
	if err != nil {
		return err
	}
	instanceText, err := s.CLI(ctx, "instance", "get", inheritedID)
	if err != nil {
		return err
	}
	readback := rawErr == nil && rawOK && total >= 1 && listed != nil &&
		sandboxKeepaliveMatches(listed, "MIXED", "900s", true, true) &&
		rawRow != nil && sandboxKeepaliveMatches(rawRow, "MIXED", "900s", true, true) &&
		listedInstance != nil && sandboxKeepaliveMatches(listedInstance, "MIXED", "900s", true, true) &&
		strings.Contains(string(toolText), "KeepaliveMode") && strings.Contains(string(toolText), "900s") &&
		strings.Contains(string(instanceText), "KeepaliveMode") && strings.Contains(string(instanceText), "MIXED")
	if !readback {
		sandboxDiagnostic("readback: rawErr=%v rawOK=%v total=%d listed=%v rawRow=%v listedInstance=%v toolTextMode=%v toolTextTimeout=%v instanceTextMode=%v instanceTextValue=%v",
			rawErr, rawOK, total, listed != nil, rawRow != nil, listedInstance != nil,
			strings.Contains(string(toolText), "KeepaliveMode"), strings.Contains(string(toolText), "900s"),
			strings.Contains(string(instanceText), "KeepaliveMode"), strings.Contains(string(instanceText), "MIXED"))
		sandboxDiagnoseKeepalive("readback tool list row", listed)
		sandboxDiagnoseKeepalive("readback raw instance row", rawRow)
		sandboxDiagnoseKeepalive("readback instance list row", listedInstance)
		sandboxDiagnostic("tool get text (%d bytes): %s", len(toolText), sandboxTextExcerpt(toolText))
		sandboxDiagnostic("instance get text (%d bytes): %s", len(instanceText), sandboxTextExcerpt(instanceText))
	}
	if err := s.Assert("keepalive.readback", readback); err != nil {
		return err
	}

	// Service-side rejection of invalid values must leave the stored
	// configuration untouched. Every request here is invalid per the contract:
	// an explicit null is a rejected value, not a no-op, and only the service's
	// parameter-validation answer counts as a rejection.
	beforeInvalid, err := sandboxGetInstance(s, ctx, overrideID)
	if err != nil {
		return err
	}
	unchanged := true
	for _, members := range sandboxKeepaliveInvalidRequests() {
		payload := map[string]any{"InstanceId": overrideID}
		for name, value := range members {
			payload[name] = value
		}
		envelope, callErr := sandboxRawCall(s, ctx, "UpdateSandboxInstance", payload)
		afterInvalid, readErr := sandboxGetInstance(s, ctx, overrideID)
		if readErr != nil {
			return readErr
		}
		if !sandboxEqualKeepalive(beforeInvalid, afterInvalid) {
			unchanged = false
		}
		if !sandboxParameterRejection(callErr, envelope) {
			sandboxDiagnostic("service did not reject invalid keepalive request %v: %s", members, sandboxFailureSummary(envelope))
			unchanged = false
		}
	}
	// Local structured rejection of unknown top-level members proves the CLI
	// contract; it is not backend evidence.
	local := true
	for _, member := range []string{"KeepaliveMode", "MaxKeepaliveDuration"} {
		payload, marshalErr := json.Marshal(map[string]any{"InstanceId": overrideID, member: "REQUEST"})
		if marshalErr != nil {
			return marshalErr
		}
		envelope, callErr := sandboxRunExpected(s, ctx, "instance", "update", overrideID, "--request", string(payload))
		if callErr == nil || envelope.Failure == nil || envelope.Failure.Kind != "usage" {
			sandboxDiagnostic("local rejection missing for top-level member %s", member)
			local = false
		}
	}
	return s.Assert("keepalive.boundaries", unchanged && local)
}

// sandboxLogFixture carries the reviewed CLS topic and its role. Neither is
// created, configured or reported by this scenario.
type sandboxLogFixture struct {
	topicID, roleArn string
}

func sandboxLogFixtureFromEnv() (sandboxLogFixture, error) {
	fixture := sandboxLogFixture{
		topicID: strings.TrimSpace(os.Getenv("AGR_TEST_CLS_TOPIC_ID")),
		roleArn: strings.TrimSpace(os.Getenv("AGR_TEST_ROLE_ARN")),
	}
	variables := []struct{ name, value string }{
		{"AGR_TEST_CLS_TOPIC_ID", fixture.topicID},
		{"AGR_TEST_ROLE_ARN", fixture.roleArn},
	}
	missing := make([]string, 0, len(variables))
	for _, variable := range variables {
		if variable.value == "" {
			missing = append(missing, variable.name)
		}
	}
	if len(missing) > 0 {
		sandboxDiagnostic("log fixture: %s is not configured", strings.Join(missing, ", "))
		return sandboxLogFixture{}, fmt.Errorf("%s must name an existing disposable CLS log fixture", strings.Join(missing, ", "))
	}
	return fixture, nil
}

func sandboxLogConfigurationJSON(fixture sandboxLogFixture, contentFormat string, includeFormat bool) string {
	configuration := map[string]any{"CLSConfig": map[string]any{"TopicId": fixture.topicID}}
	if includeFormat {
		configuration["ContentFormat"] = contentFormat
	}
	payload, err := json.Marshal(configuration)
	if err != nil {
		// The fixture only holds strings, so marshaling cannot fail.
		panic(err)
	}
	return string(payload)
}

func sandboxLogFormat(object map[string]any) string {
	return sandboxString(sandboxNested(object, "LogConfiguration"), "ContentFormat")
}

// sandboxTextExcerpt keeps a bounded single-line view of CLI text output for the
// suite log so a failing display assertion shows what was rendered.
func sandboxTextExcerpt(text []byte) string {
	const limit = 800
	value := strings.Join(strings.Fields(string(text)), " ")
	if len(value) > limit {
		value = value[:limit] + "..."
	}
	return value
}

func sandboxToolLogTopic(object map[string]any) string {
	return sandboxString(sandboxNested(object, "LogConfiguration", "CLSConfig"), "TopicId")
}

func sandboxDiagnoseLog(label string, object map[string]any) {
	sandboxDiagnostic("%s: contentFormat=%q topic=%q status=%q",
		label, sandboxLogFormat(object), sandboxToolLogTopic(object), sandboxString(object, "Status"))
}

func runSandboxLogFormat(s *patchtest.Session) error {
	fixture, err := sandboxLogFixtureFromEnv()
	if err != nil {
		return err
	}
	ctx := s.Context

	createLogged := func(name, logConfiguration string) (map[string]any, error) {
		toolID, err := sandboxCreateTool(s, ctx, name, sandboxLogTool,
			"--role-arn", fixture.roleArn, "--log-configuration", logConfiguration)
		if err != nil {
			return nil, err
		}
		return sandboxGetTool(s, ctx, toolID)
	}

	autoTool, err := createLogged(sandboxUniqueName("log-auto"),
		sandboxLogConfigurationJSON(fixture, "auto", true))
	if err != nil {
		return err
	}
	if sandboxLogFormat(autoTool) != "auto" || sandboxToolLogTopic(autoTool) != fixture.topicID {
		sandboxDiagnoseLog("log auto tool", autoTool)
	}
	if err := s.Assert("log.auto",
		sandboxLogFormat(autoTool) == "auto" && sandboxToolLogTopic(autoTool) == fixture.topicID); err != nil {
		return err
	}
	autoID := sandboxString(autoTool, "ToolId")

	// Omitting ContentFormat keeps the server-side raw default.
	omitted, err := createLogged(sandboxUniqueName("log-default"),
		sandboxLogConfigurationJSON(fixture, "", false))
	if err != nil {
		return err
	}
	// An explicit empty string is normalized to raw as well.
	emptyFormat, err := createLogged(sandboxUniqueName("log-empty"),
		sandboxLogConfigurationJSON(fixture, "", true))
	if err != nil {
		return err
	}
	if sandboxLogFormat(omitted) != "raw" || sandboxToolLogTopic(omitted) != fixture.topicID ||
		sandboxLogFormat(emptyFormat) != "raw" || sandboxToolLogTopic(emptyFormat) != fixture.topicID {
		sandboxDiagnoseLog("log omitted format", omitted)
		sandboxDiagnoseLog("log empty format", emptyFormat)
	}
	if err := s.Assert("log.raw-default",
		sandboxLogFormat(omitted) == "raw" && sandboxToolLogTopic(omitted) == fixture.topicID &&
			sandboxLogFormat(emptyFormat) == "raw" && sandboxToolLogTopic(emptyFormat) == fixture.topicID); err != nil {
		return err
	}

	for _, tc := range []struct{ sent, want string }{
		{"RAW", "raw"},
		{"AuTo", "auto"},
	} {
		tool, err := createLogged(sandboxUniqueName("log-normalize"),
			sandboxLogConfigurationJSON(fixture, tc.sent, true))
		if err != nil {
			return err
		}
		if sandboxLogFormat(tool) != tc.want {
			sandboxDiagnostic("log normalization %q: want %q", tc.sent, tc.want)
			sandboxDiagnoseLog("log normalization tool", tool)
		}
		if err := s.Assert("log.normalization", sandboxLogFormat(tool) == tc.want); err != nil {
			return err
		}
	}

	// Invalid combinations must be rejected by the service before creation.
	invalid := true
	for _, logConfiguration := range []string{
		`{"ContentFormat":"auto"}`,
		fmt.Sprintf(`{"CLSConfig":{"TopicId":%q},"ContentFormat":"json"}`, fixture.topicID),
	} {
		args := []string{"tool", "create",
			"-n", sandboxUniqueName("log-invalid"), "-t", sandboxLogTool.toolType,
			"--network-configuration", sandboxLogTool.network,
			"--persistent=false", "--default-timeout", sandboxToolDefaultTimeout}
		args = append(args, sandboxLogTool.args...)
		args = append(args, "--role-arn", fixture.roleArn, "--log-configuration", logConfiguration)
		envelope, callErr := sandboxRunExpected(s, ctx, args...)
		toolID := sandboxString(envelope.Data, "ToolId")
		if toolID == "" {
			toolID = sandboxFailureDetail(envelope, "ResourceId")
		}
		if toolID != "" {
			sandboxOwnTool(s, toolID)
		}
		if !sandboxParameterRejection(callErr, envelope) {
			sandboxDiagnostic("log invalid %s: %s", logConfiguration, sandboxFailureSummary(envelope))
			invalid = false
		}
	}
	if err := s.Assert("log.invalid", invalid); err != nil {
		return err
	}

	// Fork copies ContentFormat and honours an explicit replacement.
	copied, err := sandboxForkTool(s, ctx, autoID, sandboxUniqueName("log-fork"))
	if err != nil {
		return err
	}
	replaced, err := sandboxForkTool(s, ctx, autoID, sandboxUniqueName("log-fork-raw"),
		"--log-configuration", sandboxLogConfigurationJSON(fixture, "raw", true))
	if err != nil {
		return err
	}
	if sandboxLogFormat(copied) != "auto" || sandboxLogFormat(replaced) != "raw" {
		sandboxDiagnoseLog("log fork copy", copied)
		sandboxDiagnoseLog("log fork replaced", replaced)
	}
	if err := s.Assert("log.fork",
		sandboxLogFormat(copied) == "auto" && sandboxLogFormat(replaced) == "raw"); err != nil {
		return err
	}

	// Readback through get/list, the raw list action and the text columns.
	rows, total, err := sandboxToolRows(s, ctx, autoID)
	if err != nil {
		return err
	}
	listed := sandboxToolRow(rows, autoID)
	rawEnvelope, rawErr := sandboxRawCall(s, ctx, "DescribeSandboxToolList", map[string]any{
		"ToolIds": []string{autoID},
	})
	rawResponse, rawOK := sandboxRawResponseData(rawEnvelope)
	rawRows, _ := rawResponse["SandboxToolSet"].([]any)
	rawRow := map[string]any(nil)
	if len(rawRows) > 0 {
		rawRow = sandboxObject(rawRows[0])
	}
	text, err := s.CLI(ctx, "tool", "get", autoID)
	if err != nil {
		return err
	}
	readback := rawErr == nil && rawOK && total >= 1 && listed != nil &&
		sandboxLogFormat(listed) == "auto" &&
		rawRow != nil && sandboxLogFormat(rawRow) == "auto" &&
		strings.Contains(string(text), "LogConfiguration.ContentFormat") && strings.Contains(string(text), "auto")
	if !readback {
		sandboxDiagnostic("log readback: rawErr=%v rawOK=%v total=%d listed=%v rawRow=%v textFormat=%v textValue=%v",
			rawErr, rawOK, total, listed != nil, rawRow != nil,
			strings.Contains(string(text), "LogConfiguration.ContentFormat"), strings.Contains(string(text), "auto"))
		sandboxDiagnoseLog("log readback list row", listed)
		sandboxDiagnoseLog("log readback raw row", rawRow)
		sandboxDiagnostic("tool get text (%d bytes): %s", len(text), sandboxTextExcerpt(text))
	}
	return s.Assert("log.readback", readback)
}

// sandboxForkTool forks a tool, registers cleanup for the copy as soon as its ID
// is known, and returns the copy's readback.
func sandboxForkTool(s *patchtest.Session, ctx context.Context, toolID, name string, extra ...string) (map[string]any, error) {
	args := append([]string{"tool", "fork", toolID, "-n", name}, extra...)
	envelope, err := sandboxRun(s, ctx, args...)
	forkID := sandboxString(envelope.Data, "ToolId")
	if forkID == "" {
		forkID = sandboxFailureDetail(envelope, "ResourceId")
	}
	if forkID != "" {
		sandboxOwnTool(s, forkID)
	}
	if err != nil {
		return nil, err
	}
	if forkID == "" {
		return nil, errors.New("tool fork returned no ToolId")
	}
	if err := sandboxWaitToolActive(s, ctx, forkID); err != nil {
		return nil, err
	}
	return sandboxGetTool(s, ctx, forkID)
}
