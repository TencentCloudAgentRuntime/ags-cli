package patchscenarios

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apivalue"
)

func TestVerifyEndpointMetricSamples(t *testing.T) {
	ids := [2]string{"ep-first", "ep-second"}
	start, end := time.Unix(600, 0).UTC(), time.Unix(720, 0).UTC()
	for _, mode := range []string{"vector", "matrix"} {
		t.Run(mode, func(t *testing.T) {
			point := func(id string, at int64, value string) string {
				sample := fmt.Sprintf(`[%d,%q]`, at, value)
				field := `"value":` + sample
				if mode == "matrix" {
					field = `"values":[` + sample + "]"
				}
				return fmt.Sprintf(`{"metric":{"endpoint_id":%q},%s}`, id, field)
			}
			payload := func(series ...string) string {
				return fmt.Sprintf(`{"resultType":%q,"result":[%s]}`, mode, strings.Join(series, ","))
			}
			valid := func(first, second string) string {
				return payload(point(ids[0], 720, first), point(ids[1], 720, second))
			}
			for _, tc := range []struct {
				name, invocations, latency, errorRate string
				pass                                  bool
			}{
				{"valid samples", valid("1", "4"), valid("100", "180"), valid("0", "0"), true},
				{"missing result", fmt.Sprintf(`{"resultType":%q}`, mode), valid("100", "180"), valid("0", "0"), false},
				{"empty result", payload(), valid("100", "180"), valid("0", "0"), false},
				{"missing Endpoint", payload(point(ids[0], 720, "1")), valid("100", "180"), valid("0", "0"), false},
				{"foreign Endpoint", payload(point(ids[0], 720, "1"), point("ep-foreign", 720, "4")), valid("100", "180"), valid("0", "0"), false},
				{"wrong timestamp", payload(point(ids[0], 719, "1"), point(ids[1], 720, "4")), valid("100", "180"), valid("0", "0"), false},
				{"swapped counts", valid("4", "1"), valid("100", "180"), valid("0", "0"), false},
				{"zero invocations", valid("0", "0"), valid("100", "180"), valid("0", "0"), false},
				{"invalid latency", valid("1", "4"), valid("NaN", "180"), valid("0", "0"), false},
				{"unexpected errors", valid("1", "4"), valid("100", "180"), valid("0", "0.5"), false},
			} {
				t.Run(tc.name, func(t *testing.T) {
					var period int64
					if mode == "matrix" {
						period = 120
					}
					data := apivalue.Object{
						"WindowStartTime": start.Format(time.RFC3339), "WindowEndTime": end.Format(time.RFC3339),
						"Period": period,
						"Results": []map[string]string{
							{"MetricName": "InvocationCount", "Data": tc.invocations},
							{"MetricName": "P95LatencyMs", "Data": tc.latency},
							{"MetricName": "ErrorRate", "Data": tc.errorRate},
						},
					}
					if err := verifyEndpointMetricSamples(data, ids, start, end, mode, period); (err == nil) != tc.pass {
						t.Fatalf("pass = %v; verifier returned %v", tc.pass, err)
					}
				})
			}
		})
	}
}

func TestEndpointUpdateReadbackRejectsStaleFields(t *testing.T) {
	endpoint := apivalue.Object{"EndpointId": "ep-fixture", "Name": "updated", "Description": "", "Status": "DISABLED", "QPS": 31, "Revision": 2}
	target := apivalue.Object{"TargetId": "tgt-fixture", "Name": "updated target", "Description": "", "Status": "DISABLED", "VersionId": "ver-new", "Revision": 2}
	for _, tc := range []struct {
		name  string
		value apivalue.Object
		check func(apivalue.Object) bool
		stale apivalue.Object
	}{
		{"Endpoint", endpoint, func(value apivalue.Object) bool {
			return endpointUpdated(apivalue.Object{"Endpoint": value}, "ep-fixture", "updated")
		}, apivalue.Object{"Name": "original", "Description": "old description", "Status": "ACTIVE", "QPS": 17, "Revision": 1}},
		{"Target", target, func(value apivalue.Object) bool {
			return endpointTargetUpdated(apivalue.Object{"Target": value}, "tgt-fixture", "ver-new")
		}, apivalue.Object{"Name": "original", "Description": "old description", "Status": "ENABLED", "VersionId": "ver-old", "PathAlias": "previous", "InvocationURL": "https://example.test/mcp/previous", "Revision": 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if !tc.check(tc.value) {
				t.Fatal("expected confirmed update")
			}
			for field, stale := range tc.stale {
				value, err := apivalue.Decode(tc.value)
				if err != nil {
					t.Fatal(err)
				}
				value[field] = stale
				if tc.check(value) {
					t.Errorf("accepted stale %s", field)
				}
			}
		})
	}
}
