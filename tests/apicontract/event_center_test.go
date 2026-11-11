package apicontract_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/TencentCloudAgentRuntime/ags-cli/tests/testutil"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// The pipeline focuses this group on both builds. All queries use existing
// events; no resource is created, mutated, or deleted by these scenarios.
var _ = Describe("Event center dedicated command", func() {
	var cli *testutil.CLI
	var start, end time.Time
	var run func(...string) map[string]any
	var query func(string, string, int, int, ...string) map[string]any

	BeforeEach(func() {
		cli = testutil.NewCLI()
		DeferCleanup(cli.Cleanup)
		// Leave indexing headroom and avoid the moving seven-day boundary.
		end = time.Now().UTC().Add(-2 * time.Minute).Truncate(time.Second)
		start = end.Add(-6 * 24 * time.Hour)
		run = func(args ...string) map[string]any {
			result := cli.Run(context.Background(), append(args, "-o", "json")...)
			result.ExpectSuccess()
			Expect(result.Stderr).To(BeEmpty())
			envelope := result.Envelope()
			Expect(envelope.Command).To(Equal("event-center.list"))
			Expect(envelope.Status).To(Equal("succeeded"))
			Expect(envelope.Failure).To(BeNil())
			Expect(envelope.Warnings).To(BeEmpty())
			Expect(envelope.Data["RequestId"]).To(BeAssignableToTypeOf(""))
			Expect(envelope.Data["RequestId"]).NotTo(BeEmpty())
			return envelope.Data
		}
		query = func(mode, order string, offset, limit int, extra ...string) map[string]any {
			args := []string{"event-center", "list", "--mode", mode, "--order", order,
				"--start-time", start.Format(time.RFC3339Nano), "--end-time", end.Format(time.RFC3339Nano),
				"--offset", strconv.Itoa(offset), "--limit", strconv.Itoa(limit)}
			data := run(append(args, extra...)...)
			if effective, present := data["Mode"]; present {
				Expect(effective).To(Equal(mode))
			}
			for field, expected := range map[string]time.Time{"StartTime": start, "EndTime": end} {
				if value, present := data[field]; present {
					Expect(eventCenterTime(value).Equal(expected)).To(BeTrue(), field)
				}
			}
			return data
		}
	})

	It("queries nonempty Raw and Aggregate data through the dedicated entry point", func() {
		for _, mode := range []string{"Raw", "Aggregate"} {
			By("querying nonempty " + mode + " data")
			data := query(mode, "Desc", 0, 20)
			rows := eventCenterRows(data)
			Expect(rows).NotTo(BeEmpty(), "requires existing events within the fixed six-day window")
			Expect(testutil.NumberField(data, "TotalCount")).To(BeNumerically(">=", len(rows)))
			for _, row := range rows {
				for _, field := range []string{"EventId", "Source", "ObjectType", "ObjectId", "EventType", "EventLevel", "Message"} {
					Expect(row[field]).To(BeAssignableToTypeOf(""), field)
				}
				Expect(row["EventId"]).NotTo(BeEmpty())
				if value, present := row["Extensions"]; present {
					text, ok := value.(string)
					Expect(ok).To(BeTrue(), "Extensions retains its wire string type")
					Expect(json.Valid([]byte(text))).To(BeTrue(), "Extensions is JSON encoded inside a string")
				}
				if value, present := row["Count"]; present {
					count, ok := value.(json.Number)
					Expect(ok).To(BeTrue())
					integer, err := count.Int64()
					Expect(err).NotTo(HaveOccurred())
					Expect(integer).To(BeNumerically(">=", 1)) // Lifecycle aggregates may contain only one event.
				}
				for _, field := range []string{"OccurredAt", "FirstOccurredAt", "LastOccurredAt"} {
					if value, present := row[field]; present {
						stamp := eventCenterTime(value)
						Expect(stamp.Before(start)).To(BeFalse())
						Expect(stamp.Before(end)).To(BeTrue())
					}
				}
			}
		}
	})

	It("matches an existing resource and returns an empty nonmatching resource query", func() {
		rows := eventCenterRows(query("Raw", "Desc", 0, 1))
		Expect(rows).NotTo(BeEmpty(), "requires an existing event")
		id := rows[0]["ObjectId"].(string)
		Expect(id).NotTo(BeEmpty())
		filters, err := json.Marshal([]map[string]any{{"Name": "ObjectId", "Values": []string{id}}})
		Expect(err).NotTo(HaveOccurred())
		for _, mode := range []string{"Raw", "Aggregate"} {
			matched := eventCenterRows(query(mode, "Desc", 0, 100, "--filters", string(filters)))
			Expect(matched).NotTo(BeEmpty())
			for _, row := range matched {
				Expect(row["ObjectId"]).To(Equal(id))
			}
		}
		filters, err = json.Marshal([]map[string]any{{"Name": "ObjectId", "Values": []string{fmt.Sprintf("agr-event-center-missing-%d", time.Now().UnixNano())}}})
		Expect(err).NotTo(HaveOccurred())
		data := query("Raw", "Desc", 0, 20, "--filters", string(filters))
		Expect(eventCenterRows(data)).To(BeEmpty())
		Expect(testutil.NumberField(data, "TotalCount")).To(Equal(0))
	})

	It("pages a fixed window and checks both time orders in each mode", func() {
		for _, mode := range []string{"Raw", "Aggregate"} {
			field := "OccurredAt"
			if mode == "Aggregate" {
				field = "LastOccurredAt"
			}
			first := query(mode, "Asc", 0, 100)
			rows := eventCenterRows(first)
			Expect(len(rows)).To(BeNumerically(">=", 2), "requires at least two existing sortable results in "+mode)
			eventCenterCheckOrder(rows, field, "Asc", start, end)
			eventCenterCheckOrder(eventCenterRows(query(mode, "Desc", 0, 100)), field, "Desc", start, end)
			// Split at a timestamp boundary, so no extra ordering guarantee is
			// invented for distinct events with equal timestamps.
			split := 1
			for split < len(rows) && eventCenterTime(rows[split][field]).Equal(eventCenterTime(rows[0][field])) {
				split++
			}
			Expect(split).To(BeNumerically("<", len(rows)), "requires two distinct timestamps among the first 100 results in "+mode)
			page := query(mode, "Asc", 0, split)
			next := query(mode, "Asc", split, 1)
			pageRows, nextRows := eventCenterRows(page), eventCenterRows(next)
			Expect(pageRows).To(HaveLen(split))
			Expect(nextRows).To(HaveLen(1))
			Expect(next["TotalCount"]).To(Equal(page["TotalCount"]))
			Expect(page["TotalCount"]).To(Equal(first["TotalCount"]))
			for _, row := range pageRows {
				Expect(row["EventId"]).NotTo(Equal(nextRows[0]["EventId"]))
				Expect(eventCenterTime(row[field]).Equal(eventCenterTime(rows[0][field]))).To(BeTrue())
			}
			Expect(eventCenterTime(nextRows[0][field]).Equal(eventCenterTime(rows[split][field]))).To(BeTrue())
		}
	})

	It("uses an inclusive start and exclusive end around an existing event", func() {
		rows := eventCenterRows(query("Raw", "Desc", 0, 1))
		Expect(rows).NotTo(BeEmpty(), "requires an existing event with OccurredAt")
		event := rows[0]
		stamp := eventCenterTime(event["OccurredAt"])
		filter, err := json.Marshal([]map[string]any{{"Name": "EventId", "Values": []string{event["EventId"].(string)}}})
		Expect(err).NotTo(HaveOccurred())
		start = stamp
		data := query("Raw", "Asc", 0, 20, "--filters", string(filter))
		Expect(eventCenterRows(data)).To(HaveLen(1))
		Expect(eventCenterRows(data)[0]["EventId"]).To(Equal(event["EventId"]))
		start, end = stamp.Add(-time.Second), stamp
		data = query("Raw", "Asc", 0, 20, "--filters", string(filter))
		Expect(eventCenterRows(data)).To(BeEmpty())
		Expect(testutil.NumberField(data, "TotalCount")).To(Equal(0))
	})

	It("renders an existing resource as text and accepts the default query", func() {
		rows := eventCenterRows(query("Raw", "Desc", 0, 1))
		Expect(rows).NotTo(BeEmpty(), "requires an existing event")
		row := rows[0]
		filter, err := json.Marshal([]map[string]any{{"Name": "EventId", "Values": []string{row["EventId"].(string)}}})
		Expect(err).NotTo(HaveOccurred())
		result := cli.Run(context.Background(), "event-center", "list", "--filters", string(filter), "--start-time", start.Format(time.RFC3339Nano), "--end-time", end.Format(time.RFC3339Nano), "-o", "text")
		result.ExpectSuccess()
		Expect(result.Stderr).To(BeEmpty())
		for _, value := range []string{"EVENT ID", "OBJECT ID", "EVENT TYPE", "LEVEL", "REASON", "OCCURRED AT", row["EventId"].(string), row["ObjectId"].(string), row["EventType"].(string), row["EventLevel"].(string)} {
			Expect(result.Stdout).To(ContainSubstring(value))
		}
		if stamp, present := row["OccurredAt"]; present {
			Expect(result.Stdout).To(ContainSubstring(stamp.(string)))
		}
		defaults := run("event-center", "list")
		Expect(defaults).To(HaveKey("Events"))
		Expect(testutil.NumberField(defaults, "TotalCount")).To(BeNumerically(">=", 0))
		if mode, present := defaults["Mode"]; present {
			Expect(mode).To(Equal("Raw"))
		}
	})

	It("rejects invalid input and preserves service time-boundary failures", func() {
		for _, args := range [][]string{
			{"--mode", "invalid"}, {"--order", "ASC"}, {"--offset", "-1"}, {"--offset", "10001"}, {"--limit", "0"}, {"--limit", "101"},
			{"--start-time", "invalid"}, {"--filters", `[{"Name":"Unknown","Values":["x"]}]`},
		} {
			result := cli.Run(context.Background(), append(append([]string{"event-center", "list"}, args...), "-o", "json")...)
			result.ExpectExit(2)
			Expect(result.Stderr).To(BeEmpty())
			failure := result.Envelope().Failure
			Expect(failure).NotTo(BeNil())
			Expect(failure.Kind).To(Equal("usage"))
			Expect(failure.Hint).NotTo(BeEmpty())
		}
		now := time.Now().UTC()
		for _, bounds := range [][2]time.Time{{now.Add(-8 * 24 * time.Hour), now.Add(-time.Hour)}, {now.Add(-time.Hour), now.Add(time.Hour)}} {
			result := cli.Run(context.Background(), "event-center", "list", "--start-time", bounds[0].Format(time.RFC3339), "--end-time", bounds[1].Format(time.RFC3339), "-o", "json")
			result.ExpectExit(2)
			Expect(result.Stderr).To(BeEmpty())
			failure := result.Envelope().Failure
			Expect(failure).NotTo(BeNil())
			Expect(failure.Kind).To(Equal("usage"))
			Expect(strings.HasPrefix(failure.Code, "InvalidParameter")).To(BeTrue(), "the service must enforce its own time boundary")
			Expect(failure.Details["RequestId"]).NotTo(BeEmpty())
		}
	})
})

func eventCenterRows(data map[string]any) []map[string]any {
	values, ok := data["Events"].([]any)
	ExpectWithOffset(1, ok).To(BeTrue(), "Events must be an array")
	rows := make([]map[string]any, 0, len(values))
	for _, value := range values {
		row, ok := value.(map[string]any)
		ExpectWithOffset(1, ok).To(BeTrue())
		rows = append(rows, row)
	}
	return rows
}

func eventCenterTime(value any) time.Time {
	text, ok := value.(string)
	ExpectWithOffset(1, ok).To(BeTrue(), "requires a returned RFC3339 timestamp")
	stamp, err := time.Parse(time.RFC3339Nano, text)
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
	return stamp
}

func eventCenterCheckOrder(rows []map[string]any, field, order string, start, end time.Time) {
	ExpectWithOffset(1, len(rows)).To(BeNumerically(">=", 2), "requires at least two sortable results")
	seen := map[string]bool{}
	var previous time.Time
	for _, row := range rows {
		id := row["EventId"].(string)
		ExpectWithOffset(1, seen[id]).To(BeFalse(), "deduplicated event/result IDs")
		seen[id] = true
		stamp := eventCenterTime(row[field])
		ExpectWithOffset(1, stamp.Before(start)).To(BeFalse())
		ExpectWithOffset(1, stamp.Before(end)).To(BeTrue())
		if !previous.IsZero() {
			if order == "Asc" {
				ExpectWithOffset(1, stamp.Before(previous)).To(BeFalse())
			} else {
				ExpectWithOffset(1, stamp.After(previous)).To(BeFalse())
			}
		}
		previous = stamp
	}
}
