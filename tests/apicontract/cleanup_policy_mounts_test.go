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

func findToolByName(ctx context.Context, cli *testutil.CLI, name string) string {
	filters, err := json.Marshal([]map[string]any{{"Name": "ToolName", "Values": []string{name}}})
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
	result := cli.Run(ctx, "tool", "list", "--filters", string(filters), "--limit", "100", "-o", "json")
	if result.ExitCode != 0 {
		return ""
	}
	data, err := succeededData(result)
	if err != nil {
		return ""
	}
	items, _ := data["Items"].([]any)
	for _, value := range items {
		item, ok := value.(map[string]any)
		if !ok || item["ToolName"] != name {
			continue
		}
		if id, ok := item["ToolId"].(string); ok {
			return id
		}
	}
	return ""
}

// cleanupInstance deletes one instance and confirms it is stopped or gone.
func cleanupInstance(ctx context.Context, cli *testutil.CLI, instanceID string) {
	deleted := cli.Run(ctx, "instance", "delete", instanceID, "--wait", "--ignore-not-found", "-o", "json")
	ExpectWithOffset(1, deleted.ExitCode).To(Equal(0), deleted.Diagnostics())
	observed := cli.Run(ctx, "instance", "get", instanceID, "-o", "json")
	if observed.ExitCode == 0 {
		data, err := succeededData(observed)
		ExpectWithOffset(1, err).NotTo(HaveOccurred())
		ExpectWithOffset(1, testutil.StringField(data, "Status")).To(Equal("STOPPED"))
		return
	}
	envelope, err := envelopeOf(observed)
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
	ExpectWithOffset(1, envelope.Failure).NotTo(BeNil(), observed.Diagnostics())
	ExpectWithOffset(1, envelope.Failure.Kind).To(Equal("not_found"))
}

// cleanupInstancesByTool removes every instance this run's tool owns, from the
// tool ID rather than from an error payload.
func cleanupInstancesByTool(ctx context.Context, cli *testutil.CLI, toolID string) {
	listed := cli.Run(ctx, "instance", "list", "--all", "--tool-id", toolID, "-o", "json")
	if listed.ExitCode != 0 {
		return
	}
	data, err := succeededData(listed)
	if err != nil {
		return
	}
	items, _ := data["Items"].([]any)
	for _, value := range items {
		item, ok := value.(map[string]any)
		if !ok || item["ToolId"] != toolID {
			continue
		}
		instanceID, _ := item["InstanceId"].(string)
		if instanceID == "" {
			continue
		}
		cleanupInstance(ctx, cli, instanceID)
	}
}

func deleteToolEventually(ctx context.Context, cli *testutil.CLI, toolID string) {
	deadline := time.Now().Add(5 * time.Minute)
	request := fmt.Sprintf(`{"ToolId":%q}`, toolID)
	for {
		deleted := cli.Run(ctx, "tool", "delete", "--request", request, "-o", "json")
		if deleted.ExitCode == 0 {
			return
		}
		envelope, err := envelopeOf(deleted)
		ExpectWithOffset(1, err).NotTo(HaveOccurred())
		if envelope.Failure != nil && envelope.Failure.Code == "ResourceInUse.SandboxTool" && time.Now().Before(deadline) {
			time.Sleep(5 * time.Second)
			cleanupInstancesByTool(ctx, cli, toolID)
			continue
		}
		ExpectWithOffset(1, deleted.ExitCode).To(Equal(0), deleted.Diagnostics())
	}
}

func expectToolGone(ctx context.Context, cli *testutil.CLI, toolID string) {
	observed := cli.Run(ctx, "tool", "get", toolID, "-o", "json")
	ExpectWithOffset(1, observed.ExitCode).NotTo(Equal(0), observed.Diagnostics())
	envelope, err := envelopeOf(observed)
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
	ExpectWithOffset(1, envelope.Failure).NotTo(BeNil(), observed.Diagnostics())
	ExpectWithOffset(1, envelope.Failure.Kind).To(Equal("not_found"))
	ExpectWithOffset(1, envelope.Failure.Code).To(BeElementOf("TOOL_NOT_FOUND", "ResourceNotFound.SandboxTool"))
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
				listed := cli.Run(cleanupCtx, "pre-cache-image-cleanup-policy", "list", "--filter", string(nameFilter), "-o", "json")
				if listed.ExitCode == 0 {
					if data, err := succeededData(listed); err == nil {
						rows, _ := data["CleanupPolicySet"].([]any)
						for _, value := range rows {
							if policy, ok := value.(map[string]any); ok && policy["Name"] == name {
								id, _ = policy["CleanupPolicyId"].(string)
								break
							}
						}
					}
				}
			}
			if id == "" {
				return
			}
			deleted := cli.Run(cleanupCtx, "pre-cache-image-cleanup-policy", "delete", id, "-o", "json")
			if deleted.ExitCode != 0 {
				envelope, err := envelopeOf(deleted)
				ExpectWithOffset(1, err).NotTo(HaveOccurred())
				ExpectWithOffset(1, envelope.Failure).NotTo(BeNil(), deleted.Diagnostics())
				ExpectWithOffset(1, envelope.Failure.Kind).To(Equal("not_found"), deleted.Diagnostics())
			}
			remaining, err := succeededData(cli.Run(cleanupCtx, "pre-cache-image-cleanup-policy", "list", "--cleanup-policy-ids", id, "-o", "json"))
			ExpectWithOffset(1, err).NotTo(HaveOccurred())
			ExpectWithOffset(1, remaining["CleanupPolicySet"]).To(BeEmpty())
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
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
			defer cancel()
			id := toolID
			if id == "" {
				id = findToolByName(cleanupCtx, cli, toolName)
			}
			if id == "" {
				return
			}
			cleanupInstancesByTool(cleanupCtx, cli, id)
			deleteToolEventually(cleanupCtx, cli, id)
			expectToolGone(cleanupCtx, cli, id)
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
			toolID = findToolByName(ctx, cli, toolName)
			Expect(toolID).NotTo(BeEmpty(), "tool create failed and the unique-name lookup found nothing: %s", createdTool.Diagnostics())
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
			cleanupInstance(cleanupCtx, cli, instanceID)
		})

		waitedInstance := cli.Run(ctx, "instance", "get", instanceID, "--wait", "-o", "json")
		waitedInstance.ExpectSuccess()
		instanceData, err := succeededData(waitedInstance)
		Expect(err).NotTo(HaveOccurred(), waitedInstance.Diagnostics())
		Expect(testutil.StringField(instanceData, "InstanceId")).To(Equal(instanceID))
		Expect(testutil.StringField(instanceData, "Status")).To(Equal("RUNNING"))
		exec := cli.Run(ctx, "instance", "exec", instanceID, "--",
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
			ExpectWithOffset(1, envelope.Failure.Code).NotTo(BeEmpty())
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
