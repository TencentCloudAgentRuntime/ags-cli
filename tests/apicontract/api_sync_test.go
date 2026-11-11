package apicontract_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/TencentCloudAgentRuntime/ags-cli/tests/testutil"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Opt in explicitly: these tests use a real account and the configured endpoint.
func TestAPISyncLive(t *testing.T) {
	if os.Getenv("AGR_TEST_API_SYNC_LIVE") != "1" {
		t.Skip("set AGR_TEST_API_SYNC_LIVE=1 to run real API contract tests")
	}
	testutil.RunSpecs(t, "AGS API sync live contract")
}

var _ = BeforeSuite(testutil.SetupSuite)
var _ = AfterSuite(testutil.CleanupSuite)

var _ = Describe("Synced public API contract", func() {
	var cli *testutil.CLI
	var run func(...string) testutil.Envelope
	var raw func(string, string) map[string]any

	BeforeEach(func() {
		cli = testutil.NewCLI()
		DeferCleanup(cli.Cleanup)
		run = func(args ...string) testutil.Envelope {
			result := cli.Run(context.Background(), append(args, "-o", "json")...)
			result.ExpectSuccess()
			envelope := result.Envelope()
			Expect(envelope.Status).To(Equal("succeeded"))
			return envelope
		}
		raw = func(action, request string) map[string]any {
			data := run("api", "call", action, "--request", request).Data
			for data["Response"] != nil {
				data = data["Response"].(map[string]any)
			}
			Expect(data).NotTo(HaveKey("Error"))
			return data
		}
	})

	It("preserves ReadyTime and supports image volume selectors", func(ctx SpecContext) {
		rows := raw("DescribePreCacheImageList", `{"MaxResults":100}`)["PreCacheImageList"].([]any)
		var ready map[string]any
		for _, value := range rows {
			row := value.(map[string]any)
			if row["Status"] == "Success" && row["ReadyTime"] != nil {
				ready = row
				break
			}
		}
		Expect(ready).NotTo(BeNil(), "requires an existing successful image pre-cache task")
		id := ready["PreCacheImageId"].(string)
		for _, mode := range [][]string{nil, {"--pre-cache-type", "image"}} {
			data := run(append([]string{"pre-cache-image-task", "get", "--pre-cache-image-id", id}, mode...)...).Data
			Expect(data["ReadyTime"]).To(Equal(ready["ReadyTime"]))
			_, err := time.Parse(time.RFC3339Nano, data["ReadyTime"].(string))
			Expect(err).NotTo(HaveOccurred())
		}
		data := run("pre-cache-image-task", "get", ready["ImageDigest"].(string), "--image", ready["Image"].(string), "--image-registry-type", ready["ImageRegistryType"].(string), "--pre-cache-type", "image_volume").Data
		// A pending volume proves lookup support; it does not prove pre-warming completed.
		Expect(data["Status"]).NotTo(BeEmpty())
		if data["Status"] == "Success" {
			Expect(data).To(HaveKey("ReadyTime"))
		}
		invalid := cli.Run(ctx, "pre-cache-image-task", "get", "--pre-cache-image-id", id, "--pre-cache-type", "image_volume", "-o", "json")
		invalid.ExpectExit(2)
		Expect(invalid.Envelope().Failure.Code).To(Equal("CONFLICTING_INPUTS"))
	})

	It("browses marketplace versions and filters metadata without RecordId", func() {
		args := []string{"registry", "record", "version", "list", "--registry-id", "MARKETPLACE", "--limit", "100"}
		rows := run(args...).Data["VersionSet"].([]any)
		Expect(rows).NotTo(BeEmpty())
		var metadata map[string]any
		for _, value := range rows {
			row := value.(map[string]any)
			for _, key := range []string{"Name", "Description", "DescriptorType", "SupportedHarnessVersions", "Metadata"} {
				Expect(row).To(HaveKey(key))
			}
			if entries := row["Metadata"].([]any); len(entries) > 0 {
				metadata = entries[0].(map[string]any)
			}
		}
		Expect(metadata).NotTo(BeNil(), "requires a marketplace version with metadata")
		filters := []map[string]any{{"Name": "metadata." + metadata["Name"].(string), "Values": []string{metadata["Value"].(string)}}}
		encoded, err := json.Marshal(filters)
		Expect(err).NotTo(HaveOccurred())
		Expect(testutil.NumberField(run(append(args, "--filters", string(encoded))...).Data, "TotalCount")).To(BeNumerically(">", 0))
		filters[0]["Values"] = []string{fmt.Sprintf("agr-no-match-%d", time.Now().UnixNano())}
		encoded, err = json.Marshal(filters)
		Expect(err).NotTo(HaveOccurred())
		Expect(testutil.NumberField(run(append(args, "--filters", string(encoded))...).Data, "TotalCount")).To(Equal(0))
	})

	It("queries event center raw and aggregate results", func() {
		for _, mode := range []string{"Raw", "Aggregate"} {
			data := raw("DescribeEventCenterEvents", fmt.Sprintf(`{"Mode":%q,"Limit":2}`, mode))
			Expect(data["Mode"]).To(Equal(mode))
			Expect(data).To(HaveKey("Events"))
			Expect(testutil.NumberField(data, "TotalCount")).To(BeNumerically(">=", 0))
			for _, key := range []string{"StartTime", "EndTime"} {
				_, err := time.Parse(time.RFC3339Nano, data[key].(string))
				Expect(err).NotTo(HaveOccurred())
			}
		}
	})

	It("starts WAA instances with both modes and cleans up owned resources", func(ctx SpecContext) {
		source := os.Getenv("AGR_TEST_WAA_SOURCE_TOOL_ID")
		if source == "" {
			Skip("set AGR_TEST_WAA_SOURCE_TOOL_ID to an ACTIVE WAA tool; this test forks it")
		}
		cli.Timeout = 6 * time.Minute
		name := fmt.Sprintf("api-sync-e2e-%d", time.Now().UnixNano())
		tool := run("tool", "fork", source, "--tool-name", name, "--persistent=false", "--wait").Data["ToolId"].(string)
		DeferCleanup(func() {
			run("tool", "delete", tool, "--wait")
			missing := cli.Run(context.Background(), "tool", "get", tool, "-o", "json")
			Expect(missing.ExitCode).NotTo(Equal(0))
			Expect(missing.Envelope().Failure.Kind).To(Equal("not_found"))
		})
		for _, mode := range []string{"WAIT_UNTIL_HEALTHY", "DEFAULT"} {
			created := run("instance", "create", "--tool-id", tool, "--start-mode", mode, "--timeout", "5m", "--client-token", name+mode).Data
			instance := created["InstanceId"].(string)
			deleted := false
			DeferCleanup(func() {
				if !deleted {
					run("instance", "delete", instance, "--wait")
				}
			})
			Expect(created["Status"]).To(BeElementOf("STARTING", "RUNNING"))
			Expect(run("instance", "get", instance, "--wait").Data["Status"]).To(Equal("RUNNING"))
			run("instance", "delete", instance, "--wait")
			deleted = true
			observed := cli.Run(ctx, "instance", "get", instance, "-o", "json")
			if observed.ExitCode == 0 {
				Expect(observed.Envelope().Data["Status"]).To(Equal("STOPPED"))
			} else {
				Expect(observed.Envelope().Failure.Kind).To(Equal("not_found"))
			}
		}
	}, SpecTimeout(15*time.Minute))
})
