package apicontract_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"time"

	"github.com/TencentCloudAgentRuntime/ags-cli/tests/testutil"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	cleanupMountImage    = "ccr.ccs.tencentyun.com/ags-image/envd"
	cleanupMountOldImage = cleanupMountImage + ":v0.5.14"
	cleanupMountNewImage = cleanupMountImage + ":v0.6.13"
	cleanupMountSubPath  = "/usr/bin"
	runtimeMountPath     = "/mnt/agr-runtime"
	startMountPath       = "/mnt/agr-start"
)

var cleanupMountDigest = regexp.MustCompile(`^(sha256:)?[a-f0-9]{64}$`)

// sandboxMountCustomConfiguration is the tool configuration pinned in
// internal/patchscenarios/sandbox_config.go; the service only reports ACTIVE for
// this complete shape, and the update steps reuse its image family.
const sandboxMountCustomConfiguration = `{"Image":"ccr.ccs.tencentyun.com/ags-image/envd:v0.6.13","ImageRegistryType":"personal","Command":["/bin/bash","-c","/usr/bin/envd"],"Resources":{"CPU":"2","Memory":"4Gi"},"Probe":{"HttpGet":{"Path":"/health","Port":49983,"Scheme":"HTTP"},"ReadyTimeoutMs":30000,"ProbeTimeoutMs":1000,"ProbePeriodMs":1000,"SuccessThreshold":1,"FailureThreshold":3},"Ports":[{"Name":"envd","Port":49983,"Protocol":"TCP"}]}`

// envelopeOf decodes stdout without asserting so cleanup callbacks can inspect a
// failure instead of aborting on it.
func envelopeOf(result testutil.CommandResult) (testutil.Envelope, error) {
	var envelope testutil.Envelope
	if err := json.Unmarshal([]byte(result.Stdout), &envelope); err != nil {
		return envelope, fmt.Errorf("decode envelope: %w\n%s", err, result.Diagnostics())
	}
	return envelope, nil
}

func succeededData(result testutil.CommandResult) (map[string]any, error) {
	envelope, err := envelopeOf(result)
	if err != nil {
		return nil, err
	}
	if envelope.Status != "succeeded" {
		return nil, fmt.Errorf("status %s failure %+v", envelope.Status, envelope.Failure)
	}
	return envelope.Data, nil
}

// liveMount finds a mount by name in tool or instance readback.
func liveMount(data map[string]any, name string) (map[string]any, error) {
	mounts, ok := data["StorageMounts"].([]any)
	if !ok {
		return nil, fmt.Errorf("StorageMounts missing: %#v", data)
	}
	for _, value := range mounts {
		mount, ok := value.(map[string]any)
		if ok && mount["Name"] == name {
			return mount, nil
		}
	}
	return nil, fmt.Errorf("mount %q missing: %#v", name, mounts)
}

func liveMountImage(mount map[string]any) (map[string]any, error) {
	source, ok := mount["StorageSource"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("StorageSource missing: %#v", mount)
	}
	image, ok := source["Image"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("StorageSource.Image missing: %#v", source)
	}
	return image, nil
}

func runtimeMountSpec() string {
	return fmt.Sprintf(`[{"Name":"runtime","MountPath":%q,"ReadOnly":true,"StorageSource":{"Image":{"Reference":%q,"ImageRegistryType":"personal","SubPath":%q}}}]`, runtimeMountPath, cleanupMountOldImage, cleanupMountSubPath)
}

func startMountSpec() string {
	return fmt.Sprintf(`[{"Name":"agr-start","MountPath":%q,"ReadOnly":true,"StorageSource":{"Image":{"Reference":%q,"ImageRegistryType":"personal","SubPath":%q}}}]`, startMountPath, cleanupMountOldImage, cleanupMountSubPath)
}

// runtimeImageUpdate builds the tool-update payload for the runtime mount. A nil
// subPath omits the member (keep the current directory).
func runtimeImageUpdate(reference string, subPath *string) string {
	image := map[string]any{"Reference": reference, "ImageRegistryType": "personal"}
	if subPath != nil {
		image["SubPath"] = *subPath
	}
	payload := []map[string]any{{"Name": "runtime", "StorageSource": map[string]any{"Image": image}}}
	raw, err := json.Marshal(payload)
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
	return string(raw)
}

// cleanupRunner is the subset of *testutil.CLI the cleanup helpers use. The
// helpers return errors instead of asserting so a failed lookup reports that
// cleanup is unverified, and so the recovery paths can be exercised against a
// scripted runner without a live endpoint.
type cleanupRunner interface {
	Run(ctx context.Context, args ...string) testutil.CommandResult
}

// findToolByName locates the tool this run created when its create response was
// lost. The list filter key is tool-name: the service lowercases the filter name
// and only its own lowercase keys select the exact-name filter, so a camel-case
// key is ignored and the call would scan the account's first page instead.
// The unique per-run name keeps the lookup scoped to this run's tool.
func findToolByName(ctx context.Context, cli cleanupRunner, name string) (string, error) {
	filters, err := json.Marshal([]map[string]any{{"Name": "tool-name", "Values": []string{name}}})
	if err != nil {
		return "", fmt.Errorf("encode the tool-name filter for %q: %w", name, err)
	}
	listed := cli.Run(ctx, "tool", "list", "--filters", string(filters), "--limit", "100", "-o", "json")
	data, err := succeededData(listed)
	if err != nil {
		return "", fmt.Errorf("look up tool %q by name: %w\n%s", name, err, listed.Diagnostics())
	}
	items, ok := data["Items"].([]any)
	if !ok {
		return "", fmt.Errorf("tool list for %q has no Items: %#v", name, data)
	}
	for _, value := range items {
		item, ok := value.(map[string]any)
		if !ok || item["ToolName"] != name {
			continue
		}
		if id, ok := item["ToolId"].(string); ok && id != "" {
			return id, nil
		}
	}
	return "", nil
}

// cleanupInstance deletes one instance and confirms it is stopped or gone.
func cleanupInstance(ctx context.Context, cli cleanupRunner, instanceID string) error {
	deleted := cli.Run(ctx, "instance", "delete", instanceID, "--wait", "--ignore-not-found", "-o", "json")
	if deleted.ExitCode != 0 {
		return fmt.Errorf("delete instance %s: %s", instanceID, deleted.Diagnostics())
	}
	observed := cli.Run(ctx, "instance", "get", instanceID, "-o", "json")
	if observed.ExitCode == 0 {
		data, err := succeededData(observed)
		if err != nil {
			return fmt.Errorf("read instance %s after delete: %w", instanceID, err)
		}
		if status, _ := data["Status"].(string); status != "STOPPED" {
			return fmt.Errorf("instance %s is %q after delete, want STOPPED", instanceID, status)
		}
		return nil
	}
	envelope, err := envelopeOf(observed)
	if err != nil {
		return err
	}
	if envelope.Failure == nil || envelope.Failure.Kind != "not_found" {
		return fmt.Errorf("instance %s is neither STOPPED nor gone: %s", instanceID, observed.Diagnostics())
	}
	return nil
}

// cleanupInstancesByTool removes every instance this run's tool owns, from the
// tool ID rather than from an error payload, and reports a failed lookup instead
// of leaving instances behind silently.
func cleanupInstancesByTool(ctx context.Context, cli cleanupRunner, toolID string) error {
	listed := cli.Run(ctx, "instance", "list", "--all", "--tool-id", toolID, "-o", "json")
	data, err := succeededData(listed)
	if err != nil {
		return fmt.Errorf("list instances of tool %s: %w\n%s", toolID, err, listed.Diagnostics())
	}
	items, ok := data["Items"].([]any)
	if !ok {
		return fmt.Errorf("instance list for tool %s has no Items: %#v", toolID, data)
	}
	for _, value := range items {
		item, ok := value.(map[string]any)
		if !ok || item["ToolId"] != toolID {
			continue
		}
		instanceID, _ := item["InstanceId"].(string)
		if instanceID == "" {
			continue
		}
		if err := cleanupInstance(ctx, cli, instanceID); err != nil {
			return err
		}
	}
	return nil
}

// deleteToolEventually deletes the tool accepted by the service, retrying while
// the tool still owns instances.
func deleteToolEventually(ctx context.Context, cli cleanupRunner, toolID string) error {
	deadline := time.Now().Add(5 * time.Minute)
	request := fmt.Sprintf(`{"ToolId":%q}`, toolID)
	for {
		deleted := cli.Run(ctx, "tool", "delete", "--request", request, "-o", "json")
		if deleted.ExitCode == 0 {
			return nil
		}
		envelope, err := envelopeOf(deleted)
		if err != nil {
			return err
		}
		if envelope.Failure == nil {
			return fmt.Errorf("delete tool %s failed without a failure body: %s", toolID, deleted.Diagnostics())
		}
		if envelope.Failure.Code != "ResourceInUse.SandboxTool" {
			return fmt.Errorf("delete tool %s: %s", toolID, deleted.Diagnostics())
		}
		if !time.Now().Before(deadline) {
			return fmt.Errorf("tool %s still owns instances after 5m: %s", toolID, deleted.Diagnostics())
		}
		time.Sleep(5 * time.Second)
		if err := cleanupInstancesByTool(ctx, cli, toolID); err != nil {
			return err
		}
	}
}

// expectToolGone confirms the tool disappears after an accepted delete. The
// service reports DELETING until the backend drops the record, so poll instead
// of assuming the first read already answers not_found.
func expectToolGone(ctx context.Context, cli cleanupRunner, toolID string, window, interval time.Duration) error {
	deadline := time.Now().Add(window)
	for {
		observed := cli.Run(ctx, "tool", "get", toolID, "-o", "json")
		if observed.ExitCode == 0 {
			data, err := succeededData(observed)
			if err != nil {
				return err
			}
			status, _ := data["Status"].(string)
			if !time.Now().Before(deadline) {
				return fmt.Errorf("tool %s is still present with status %q after %s", toolID, status, window)
			}
			select {
			case <-ctx.Done():
				return fmt.Errorf("tool %s is still present with status %q: %w", toolID, status, ctx.Err())
			case <-time.After(interval):
			}
			continue
		}
		envelope, err := envelopeOf(observed)
		if err != nil {
			return err
		}
		if envelope.Failure == nil {
			return fmt.Errorf("tool %s get failed without a failure body: %s", toolID, observed.Diagnostics())
		}
		if envelope.Failure.Kind != "not_found" {
			return fmt.Errorf("tool %s get failed with kind %s: %s", toolID, envelope.Failure.Kind, observed.Diagnostics())
		}
		if envelope.Failure.Code != "TOOL_NOT_FOUND" && envelope.Failure.Code != "ResourceNotFound.SandboxTool" {
			return fmt.Errorf("tool %s get reported not_found with code %s", toolID, envelope.Failure.Code)
		}
		return nil
	}
}

// findPolicyByName locates the cleanup policy this run created when its create
// response was lost.
func findPolicyByName(ctx context.Context, cli cleanupRunner, name, filters string) (string, error) {
	listed := cli.Run(ctx, "pre-cache-image-cleanup-policy", "list", "--filter", filters, "-o", "json")
	data, err := succeededData(listed)
	if err != nil {
		return "", fmt.Errorf("look up cleanup policy %q: %w\n%s", name, err, listed.Diagnostics())
	}
	rows, ok := data["CleanupPolicySet"].([]any)
	if !ok {
		return "", fmt.Errorf("cleanup policy list for %q has no CleanupPolicySet: %#v", name, data)
	}
	for _, value := range rows {
		policy, ok := value.(map[string]any)
		if !ok || policy["Name"] != name {
			continue
		}
		if id, ok := policy["CleanupPolicyId"].(string); ok && id != "" {
			return id, nil
		}
	}
	return "", nil
}

// deletePolicy removes the policy this run created. The service answers not_found
// for an ID this account never saw and success for one it already soft-deleted,
// so both outcomes mean the policy is gone.
func deletePolicy(ctx context.Context, cli cleanupRunner, policyID string) error {
	deleted := cli.Run(ctx, "pre-cache-image-cleanup-policy", "delete", policyID, "-o", "json")
	if deleted.ExitCode == 0 {
		return nil
	}
	envelope, err := envelopeOf(deleted)
	if err != nil {
		return err
	}
	if envelope.Failure != nil && envelope.Failure.Kind == "not_found" {
		return nil
	}
	return fmt.Errorf("delete cleanup policy %s: %s", policyID, deleted.Diagnostics())
}

// expectPolicyGone confirms the policy no longer appears in an ID lookup.
func expectPolicyGone(ctx context.Context, cli cleanupRunner, policyID string) error {
	remaining := cli.Run(ctx, "pre-cache-image-cleanup-policy", "list", "--cleanup-policy-ids", policyID, "-o", "json")
	data, err := succeededData(remaining)
	if err != nil {
		return fmt.Errorf("confirm cleanup policy %s is gone: %w\n%s", policyID, err, remaining.Diagnostics())
	}
	rows, ok := data["CleanupPolicySet"].([]any)
	if !ok {
		return fmt.Errorf("cleanup policy list for %s has no CleanupPolicySet: %#v", policyID, data)
	}
	if len(rows) != 0 {
		return fmt.Errorf("cleanup policy %s is still listed: %#v", policyID, rows)
	}
	return nil
}

var _ = Describe("Cleanup policy and sandbox storage mounts", func() {
	var cli *testutil.CLI

	BeforeEach(func() {
		cli = testutil.NewCLI()
		// The runner strips ambient AGR_* variables, so opt out of update
		// notices explicitly to keep empty-stderr assertions meaningful.
		cli.ExtraEnv = []string{"AGR_NON_INTERACTIVE=1"}
		DeferCleanup(cli.Cleanup)
	})

	It("manages cleanup policies by ID", func(ctx SpecContext) {
		run := func(args ...string) testutil.Envelope {
			result := cli.Run(ctx, append(args, "-o", "json")...)
			result.ExpectSuccess()
			return result.Envelope()
		}
		list := func(args ...string) (map[string]any, []map[string]any) {
			data := run(append([]string{"pre-cache-image-cleanup-policy", "list"}, args...)...).Data
			rows, ok := data["CleanupPolicySet"].([]any)
			ExpectWithOffset(1, ok).To(BeTrue(), "missing CleanupPolicySet: %#v", data)
			policies := make([]map[string]any, 0, len(rows))
			for _, value := range rows {
				policy, ok := value.(map[string]any)
				ExpectWithOffset(1, ok).To(BeTrue(), "CleanupPolicySet entry: %#v", value)
				policies = append(policies, policy)
			}
			return data, policies
		}

		token := strconv.FormatInt(time.Now().UnixNano(), 10)
		name := "agr-issue180-" + token
		pattern := "^agr-issue180/" + token + "$"
		nameFilter, err := json.Marshal([]map[string]any{{"Name": "name", "Values": []string{name}}})
		Expect(err).NotTo(HaveOccurred())

		var policyID string
		DeferCleanup(func() {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			defer cancel()
			id := policyID
			if id == "" {
				// A lost create response still leaves the unique name to find.
				found, err := findPolicyByName(cleanupCtx, cli, name, string(nameFilter))
				ExpectWithOffset(1, err).NotTo(HaveOccurred(), "cleanup unverified: locate the policy created by this run")
				id = found
			}
			if id == "" {
				return
			}
			ExpectWithOffset(1, deletePolicy(cleanupCtx, cli, id)).To(Succeed(), "cleanup unverified: delete the policy created by this run")
			ExpectWithOffset(1, expectPolicyGone(cleanupCtx, cli, id)).To(Succeed(), "cleanup unverified: confirm the policy is gone")
		})

		created := run("pre-cache-image-cleanup-policy", "create",
			"--name", name,
			"--idle-ttl", "3d",
			"--registry-host", "example.invalid",
			"--repo-pattern", pattern,
		)
		policy, ok := created.Data["CleanupPolicy"].(map[string]any)
		Expect(ok).To(BeTrue(), "missing CleanupPolicy: %#v", created.Data)
		policyID = testutil.StringField(policy, "CleanupPolicyId")
		Expect(policyID).NotTo(BeEmpty())
		Expect(policy["Enabled"]).To(BeTrue(), "omitted Enabled must read back as true")
		Expect(policy["IdleTTL"]).To(Equal("3d"))
		Expect(policy["RegistryHost"]).To(Equal("example.invalid"))
		Expect(policy["RepoPattern"]).To(Equal(pattern))
		Expect(policy["Name"]).To(Equal(name))

		first, rows := list("--filter", string(nameFilter), "--limit", "1", "--offset", "0")
		Expect(testutil.NumberField(first, "TotalCount")).To(Equal(1))
		Expect(rows).To(HaveLen(1))
		Expect(rows[0]["CleanupPolicyId"]).To(Equal(policyID))
		second, rows := list("--filter", string(nameFilter), "--limit", "1", "--offset", "1")
		Expect(testutil.NumberField(second, "TotalCount")).To(Equal(1))
		Expect(rows).To(BeEmpty())

		_, rows = list("--cleanup-policy-ids", policyID, "--cleanup-policy-ids", "pccp-missing-"+token)
		Expect(rows).To(HaveLen(1))
		Expect(rows[0]["CleanupPolicyId"]).To(Equal(policyID))

		updated := run("pre-cache-image-cleanup-policy", "update", policyID, "--enabled=false", "--repo-pattern=")
		policy, ok = updated.Data["CleanupPolicy"].(map[string]any)
		Expect(ok).To(BeTrue(), "missing CleanupPolicy: %#v", updated.Data)
		Expect(policy["Enabled"]).To(BeFalse())
		Expect(policy["IdleTTL"]).To(Equal("3d"), "omitted fields must keep their values")
		Expect(policy["RegistryHost"]).To(Equal("example.invalid"))
		Expect(policy["Name"]).To(Equal(name))
		if value, present := policy["RepoPattern"]; present {
			Expect(value).To(BeEmpty())
		}

		_, rows = list("--cleanup-policy-ids", policyID)
		Expect(rows).To(HaveLen(1))
		Expect(rows[0]["Enabled"]).To(BeFalse())
		if value, present := rows[0]["RepoPattern"]; present {
			Expect(value).To(BeEmpty())
		}

		run("pre-cache-image-cleanup-policy", "delete", policyID)
		_, rows = list("--cleanup-policy-ids", policyID)
		Expect(rows).To(BeEmpty())
		policyID = ""
	})

	It("starts instances with storage mounts and updates the tool image mount", func(ctx SpecContext) {
		cli.Timeout = 6 * time.Minute
		role := os.Getenv("AGR_TEST_ROLE_ARN")
		if role == "" {
			Fail("set AGR_TEST_ROLE_ARN to a role that can pull the CCR sandbox image fixtures")
		}

		token := strconv.FormatInt(time.Now().UnixNano(), 10)
		toolName := "agr-issue180-" + token
		var toolID, instanceID string

		// Register cleanup before the tool exists: it finds a lost create
		// response by the run's unique name and never trusts error details.
		DeferCleanup(func() {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
			defer cancel()
			id := toolID
			if id == "" {
				found, err := findToolByName(cleanupCtx, cli, toolName)
				ExpectWithOffset(1, err).NotTo(HaveOccurred(), "cleanup unverified: locate the tool created by this run")
				id = found
			}
			if id == "" {
				return
			}
			ExpectWithOffset(1, cleanupInstancesByTool(cleanupCtx, cli, id)).To(Succeed(), "cleanup unverified: remove the run's instances")
			ExpectWithOffset(1, deleteToolEventually(cleanupCtx, cli, id)).To(Succeed(), "cleanup unverified: delete the run's tool")
			ExpectWithOffset(1, expectToolGone(cleanupCtx, cli, id, 5*time.Minute, 5*time.Second)).To(Succeed(), "cleanup unverified: confirm the tool is gone")
		})

		createdTool := cli.Run(ctx, "tool", "create",
			"--tool-name", toolName,
			"--tool-type", "custom",
			"--network-configuration", `{"NetworkMode":"PUBLIC"}`,
			"--role-arn", role,
			"--custom-configuration", sandboxMountCustomConfiguration,
			"--storage-mounts", runtimeMountSpec(),
			"-o", "json",
		)
		if createdTool.ExitCode == 0 {
			data, err := succeededData(createdTool)
			Expect(err).NotTo(HaveOccurred(), createdTool.Diagnostics())
			toolID = testutil.StringField(data, "ToolId")
		} else {
			found, err := findToolByName(ctx, cli, toolName)
			Expect(err).NotTo(HaveOccurred(), "tool create failed and the unique-name lookup errored: %s", createdTool.Diagnostics())
			Expect(found).NotTo(BeEmpty(), "tool create failed and the unique-name lookup found nothing: %s", createdTool.Diagnostics())
			toolID = found
		}

		readTool := func() (map[string]any, error) {
			return succeededData(cli.Run(ctx, "tool", "get", toolID, "-o", "json"))
		}
		waitedTool := cli.Run(ctx, "tool", "get", toolID, "--wait", "-o", "json")
		waitedTool.ExpectSuccess()
		toolData, err := succeededData(waitedTool)
		Expect(err).NotTo(HaveOccurred(), waitedTool.Diagnostics())
		Expect(testutil.StringField(toolData, "Status")).To(Equal("ACTIVE"))
		var initialSubPath string
		Eventually(func() error {
			data, err := readTool()
			if err != nil {
				return err
			}
			mount, err := liveMount(data, "runtime")
			if err != nil {
				return err
			}
			if mount["MountPath"] != runtimeMountPath || mount["ReadOnly"] != true {
				return fmt.Errorf("initial mount = %#v", mount)
			}
			image, err := liveMountImage(mount)
			if err != nil {
				return err
			}
			if image["Reference"] != cleanupMountOldImage {
				return fmt.Errorf("initial Reference = %v", image["Reference"])
			}
			subPath, ok := image["SubPath"].(string)
			if !ok || subPath == "" {
				return fmt.Errorf("initial SubPath = %#v", image["SubPath"])
			}
			initialSubPath = subPath
			return nil
		}, "2m", "5s").Should(Succeed(), "tool readback: %#v", toolData)

		created := cli.Run(ctx, "instance", "create",
			"--tool-id", toolID,
			"--storage-mounts", startMountSpec(),
			"--timeout", "5m",
			"-o", "json",
		)
		created.ExpectSuccess()
		createdData, err := succeededData(created)
		Expect(err).NotTo(HaveOccurred(), created.Diagnostics())
		instanceID = testutil.StringField(createdData, "InstanceId")
		Expect(instanceID).NotTo(BeEmpty())

		DeferCleanup(func() {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			ExpectWithOffset(1, cleanupInstance(cleanupCtx, cli, instanceID)).To(Succeed(), "cleanup unverified: remove the run's instance")
		})

		waitedInstance := cli.Run(ctx, "instance", "get", instanceID, "--wait", "-o", "json")
		waitedInstance.ExpectSuccess()
		instanceData, err := succeededData(waitedInstance)
		Expect(err).NotTo(HaveOccurred(), waitedInstance.Diagnostics())
		Expect(testutil.StringField(instanceData, "InstanceId")).To(Equal(instanceID))
		Expect(testutil.StringField(instanceData, "Status")).To(Equal("RUNNING"))
		// The fixture image is the published envd image itself: an Ubuntu base that
		// only carries root, with envd as its entrypoint. It has no "user" account,
		// so the exec default (--user user) cannot start a process and the data
		// plane answers with an internal error. Run the mount check as root.
		exec := cli.Run(ctx, "instance", "exec", instanceID, "--user", "root", "--",
			"sh", "-lc", "test -x "+runtimeMountPath+"/envd && test -x "+startMountPath+"/envd")
		exec.ExpectSuccess()

		for _, rejected := range []struct{ name, mounts string }{
			{
				name:   "duplicate name",
				mounts: fmt.Sprintf(`[{"Name":"runtime","StorageSource":{"Image":{"Reference":%q,"ImageRegistryType":"personal"}}},{"Name":"runtime","StorageSource":{"Image":{"Reference":%q,"ImageRegistryType":"personal"}}}]`, cleanupMountNewImage, cleanupMountNewImage),
			},
			{
				name:   "unknown name",
				mounts: fmt.Sprintf(`[{"Name":"agr-issue180-missing-%s","StorageSource":{"Image":{"Reference":%q,"ImageRegistryType":"personal"}}}]`, token, cleanupMountNewImage),
			},
			{
				name:   "bare reference",
				mounts: fmt.Sprintf(`[{"Name":"runtime","StorageSource":{"Image":{"Reference":%q,"ImageRegistryType":"personal"}}}]`, cleanupMountImage),
			},
			{
				name:   "missing image registry type",
				mounts: fmt.Sprintf(`[{"Name":"runtime","StorageSource":{"Image":{"Reference":%q}}}]`, cleanupMountNewImage),
			},
		} {
			rejectedResult := cli.Run(ctx, "tool", "update", toolID, "--storage-mounts", rejected.mounts, "-o", "json")
			ExpectWithOffset(1, rejectedResult.ExitCode).NotTo(Equal(0), rejected.name+": "+rejectedResult.Diagnostics())
			envelope, err := envelopeOf(rejectedResult)
			ExpectWithOffset(1, err).NotTo(HaveOccurred())
			ExpectWithOffset(1, envelope.Failure).NotTo(BeNil(), rejected.name+": "+rejectedResult.Diagnostics())
			// The service classifies every invalid image-volume update as a
			// StorageMount parameter error; a generic non-zero exit would also pass
			// for an auth or transport failure.
			ExpectWithOffset(1, envelope.Failure.Code).To(Equal("InvalidParameterValue.StorageMount"), rejected.name+": "+rejectedResult.Diagnostics())
		}
		unchanged, err := readTool()
		Expect(err).NotTo(HaveOccurred())
		unchangedMount, err := liveMount(unchanged, "runtime")
		Expect(err).NotTo(HaveOccurred())
		unchangedImage, err := liveMountImage(unchangedMount)
		Expect(err).NotTo(HaveOccurred())
		Expect(unchangedImage["Reference"]).To(Equal(cleanupMountOldImage), "a rejected update must not change the mount")

		updateMount := func(mounts string) {
			updated := cli.Run(ctx, "tool", "update", toolID, "--storage-mounts", mounts, "--wait", "-o", "json")
			updated.ExpectSuccess()
		}
		// pollMount waits for the published mount state, which trails the update
		// response, and checks the fields every step must preserve.
		pollMount := func(describe string, check func(map[string]any, map[string]any) error) {
			Eventually(func() error {
				data, err := readTool()
				if err != nil {
					return err
				}
				mount, err := liveMount(data, "runtime")
				if err != nil {
					return err
				}
				if mount["MountPath"] != runtimeMountPath {
					return fmt.Errorf("%s: MountPath = %v", describe, mount["MountPath"])
				}
				if mount["ReadOnly"] != true {
					return fmt.Errorf("%s: ReadOnly = %v", describe, mount["ReadOnly"])
				}
				image, err := liveMountImage(mount)
				if err != nil {
					return err
				}
				if digest, _ := image["Digest"].(string); !cleanupMountDigest.MatchString(digest) {
					return fmt.Errorf("%s: Digest = %q", describe, digest)
				}
				return check(mount, image)
			}, "5m", "5s").Should(Succeed(), describe)
		}

		By("switching the runtime image without a SubPath and keeping the directory")
		updateMount(runtimeImageUpdate(cleanupMountNewImage, nil))
		pollMount("image switch", func(_ map[string]any, image map[string]any) error {
			if image["Reference"] != cleanupMountNewImage {
				return fmt.Errorf("Reference = %v", image["Reference"])
			}
			if subPath, _ := image["SubPath"].(string); subPath != initialSubPath {
				return fmt.Errorf("SubPath = %v, want the unchanged %q", image["SubPath"], initialSubPath)
			}
			return nil
		})

		By("switching the SubPath to the image root with an explicit empty string")
		updateMount(runtimeImageUpdate(cleanupMountNewImage, ptrString("")))
		pollMount("image root", func(_ map[string]any, image map[string]any) error {
			value, present := image["SubPath"]
			if !present {
				return nil
			}
			if text, ok := value.(string); ok && text == "" {
				return nil
			}
			return fmt.Errorf("SubPath = %v, want an omitted key or an empty string", value)
		})

		By("restoring the SubPath directory")
		updateMount(runtimeImageUpdate(cleanupMountNewImage, ptrString(cleanupMountSubPath)))
		pollMount("subpath restore", func(_ map[string]any, image map[string]any) error {
			if subPath, _ := image["SubPath"].(string); subPath != initialSubPath {
				return fmt.Errorf("SubPath = %v, want %q", image["SubPath"], initialSubPath)
			}
			if image["Reference"] != cleanupMountNewImage {
				return fmt.Errorf("Reference = %v", image["Reference"])
			}
			return nil
		})

		By("keeping the mounts when the update sends an empty array")
		updateMount("[]")
		pollMount("empty array", func(_ map[string]any, image map[string]any) error {
			if image["Reference"] != cleanupMountNewImage {
				return fmt.Errorf("Reference = %v", image["Reference"])
			}
			if subPath, _ := image["SubPath"].(string); subPath != initialSubPath {
				return fmt.Errorf("SubPath = %v, want %q", image["SubPath"], initialSubPath)
			}
			return nil
		})
	}, SpecTimeout(25*time.Minute))
})

func ptrString(value string) *string { return &value }
