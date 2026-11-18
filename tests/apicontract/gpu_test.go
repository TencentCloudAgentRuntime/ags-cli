package apicontract_test

import (
	"crypto/rand"
	"encoding/json"
	"strings"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apivalue"
	"github.com/TencentCloudAgentRuntime/ags-cli/tests/testutil"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("GPU resource commands", func() {
	It("queries specs, groups, and resources in the configured region", func(ctx SpecContext) {
		cli := testutil.NewCLI()
		DeferCleanup(cli.Cleanup)
		for _, query := range []struct {
			subgroup, resultField string
		}{{"spec", "GPUResourceSpecSet"}, {"group", "ResourceGroups"}, {"resource", "Resources"}} {
			result := cli.Run(ctx, "gpu", query.subgroup, "list", "--offset", "0", "--limit", "1", "-o", "json")
			result.ExpectSuccess()
			envelope := result.Envelope()
			Expect(envelope.Command).To(Equal("gpu." + query.subgroup + ".list"))
			Expect(envelope.Failure).To(BeNil())
			Expect(envelope.Data["RequestId"]).NotTo(BeEmpty())
			Expect(envelope.Data[query.resultField]).To(BeAssignableToTypeOf([]any{}))
			items, err := apivalue.Object(envelope.Data).ReadObjects(query.resultField)
			Expect(err).NotTo(HaveOccurred())
			Expect(len(items)).To(BeNumerically("<=", 1))
			Expect(testutil.NumberField(envelope.Data, "TotalCount")).To(BeNumerically(">=", len(items)))
			if query.subgroup == "spec" && len(items) > 0 {
				item := items[0]
				Expect(item.String("GPUType")).NotTo(BeEmpty())
				Expect(item).To(HaveKey("AvailableCount"))
				exact := cli.Run(ctx, "gpu", "spec", "list", "--gpu-type", item.String("GPUType"),
					"--cpu", item.Display("CPU"), "--memory", item.Display("Memory"), "--gpu-num", item.Display("GPUNum"), "-o", "json")
				exact.ExpectSuccess()
				rows, err := apivalue.Object(exact.Envelope().Data).ReadObjects(query.resultField)
				Expect(err).NotTo(HaveOccurred())
				Expect(rows).NotTo(BeEmpty())
				for _, row := range rows {
					for _, field := range []string{"GPUType", "CPU", "Memory", "GPUNum"} {
						Expect(row[field]).To(Equal(item[field]), field)
					}
				}
			}
		}
	})

	It("preserves not-found failures for a verified absent group", func(ctx SpecContext) {
		cli := testutil.NewCLI()
		DeferCleanup(cli.Cleanup)
		id := "gpug-" + strings.ToLower(rand.Text())
		filters, err := json.Marshal([]map[string]any{{"Name": "resource-group-id", "Values": []string{id}}})
		Expect(err).NotTo(HaveOccurred())
		check := cli.Run(ctx, "gpu", "group", "list", "--filters", string(filters), "-o", "json")
		check.ExpectSuccess()
		Expect(check.Envelope().Data["ResourceGroups"]).To(BeEmpty())
		Expect(testutil.NumberField(check.Envelope().Data, "TotalCount")).To(Equal(0))
		for _, args := range [][]string{
			{"gpu", "group", "rename", id, "--name", "agr-missing-group-test", "-o", "json"},
			{"gpu", "group", "delete", id, "-o", "json"},
		} {
			result := cli.Run(ctx, args...)
			Expect(result.ExitCode).NotTo(BeZero(), result.Diagnostics())
			envelope := result.Envelope()
			Expect(envelope.Failure).NotTo(BeNil())
			Expect(envelope.Failure.Code).To(Equal("ResourceNotFound"))
			Expect(envelope.Failure.Details["RequestId"]).NotTo(BeEmpty())
		}
	})
})
