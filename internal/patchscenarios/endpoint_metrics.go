package patchscenarios

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apivalue"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/patchtest"
)

type endpointMetricFixture struct {
	tool      string
	arguments map[string]any
}

func loadEndpointMetricFixture() (endpointMetricFixture, error) {
	fixture := endpointMetricFixture{tool: strings.TrimSpace(os.Getenv("AGR_ENDPOINT_FIXTURE_TOOL"))}
	if fixture.tool == "" {
		return fixture, errors.New("AGR_ENDPOINT_FIXTURE_TOOL must name a read-only MCP tool in the fixture Record")
	}
	raw := os.Getenv("AGR_ENDPOINT_FIXTURE_ARGUMENTS")
	if raw == "" {
		raw = "{}"
	}
	if json.Unmarshal([]byte(raw), &fixture.arguments) != nil || fixture.arguments == nil {
		return fixture, errors.New("AGR_ENDPOINT_FIXTURE_ARGUMENTS must be a JSON object")
	}
	return fixture, nil
}

func runEndpointMetrics(s *patchtest.Session, first, second, version apivalue.Object, fixture endpointMetricFixture) error {
	ctx := s.Context
	endpoints := []apivalue.Object{first, second}
	ids := [2]string{first.String("EndpointId"), second.String("EndpointId")}
	clients := make([]endpointMCPSession, 0, len(endpoints))
	for i, endpoint := range endpoints {
		if _, err := endpointCall(s, ctx, "target create", "--endpoint-id", ids[i],
			"--registry-id", version.String("RegistryId"), "--record-id", version.String("RecordId"),
			"--version-id", version.String("VersionId")); err != nil {
			return err
		}
		token, err := endpointCall(s, ctx, "token create", "--endpoint-id", ids[i], "--name", "metrics", "--expires-in", "900")
		if err != nil {
			return err
		}
		client, err := openEndpointMCPSession(ctx, endpoint.String("MCPURL"), token.String("Token"))
		if err != nil {
			return err
		}
		clients = append(clients, client)
	}

	// Fresh Endpoints have no invocation history. Query a window containing
	// successful tools/call requests with distinguishable counts for each ID.
	start := time.Now().UTC().Truncate(time.Second).Add(-time.Second)
	for i, count := range []int{1, 4} {
		if err := clients[i].invokeFixture(ctx, fixture, count); err != nil {
			return fmt.Errorf("metrics fixture Endpoint %d: %w", i+1, err)
		}
	}
	// Give the final call time to be scraped and use one complete trend
	// bucket with the same bounds as the summary so both cover the traffic.
	bucket := max(2*time.Minute, (time.Since(start) + time.Minute).Round(time.Minute))
	end := start.Add(bucket)
	if err := waitEndpointMetricPoll(ctx, time.Until(end)+2*time.Second); err != nil {
		return err
	}
	poll, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	for _, mode := range []struct {
		resultType string
		period     int64
	}{
		{"vector", 0},
		{"matrix", int64(bucket / time.Second)},
	} {
		if err := waitForEndpointMetricSamples(poll, s, ids, start, end, mode.resultType, mode.period); err != nil {
			return err
		}
	}
	return s.Assert("endpoint.metrics", true)
}

var errEndpointMetricsPending = errors.New("waiting for Endpoint invocation samples")

func waitForEndpointMetricSamples(ctx context.Context, s *patchtest.Session, ids [2]string, start, end time.Time, resultType string, period int64) error {
	flags := []string{"--endpoint-ids", ids[0], "--endpoint-ids", ids[1],
		"--metric-names", "InvocationCount", "--metric-names", "P95LatencyMs", "--metric-names", "ErrorRate",
		"--start-time", start.Format(time.RFC3339), "--end-time", end.Format(time.RFC3339)}
	if period != 0 {
		flags = append(flags, "--period", strconv.FormatInt(period, 10))
	}
	for {
		data, err := endpointCall(s, ctx, "metrics", flags...)
		if err != nil {
			return err
		}
		err = verifyEndpointMetricSamples(data, ids, start, end, resultType, period)
		if !errors.Is(err, errEndpointMetricsPending) {
			return err
		}
		if err := waitEndpointMetricPoll(ctx, 10*time.Second); err != nil {
			return fmt.Errorf("metrics %s never returned complete invocation samples: %w", resultType, err)
		}
	}
}

func waitEndpointMetricPoll(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func verifyEndpointMetricSamples(data apivalue.Object, ids [2]string, start, end time.Time, resultType string, period int64) error {
	windowStart, _ := time.Parse(time.RFC3339, data.String("WindowStartTime"))
	windowEnd, _ := time.Parse(time.RFC3339, data.String("WindowEndTime"))
	if !windowStart.Equal(start) || !windowEnd.Equal(end) || data.Int64("Period") != period ||
		len(data.Objects("Results")) != 3 {
		return fmt.Errorf("metrics %s returned an unexpected window or metric count", resultType)
	}
	for _, name := range []string{"InvocationCount", "P95LatencyMs", "ErrorRate"} {
		metric := endpointFind(data, "Results", "MetricName", name)
		samples, err := endpointMetricSamples(metric.String("Data"), resultType, ids, end.Unix())
		if err != nil {
			return fmt.Errorf("metrics %s %s: %w", resultType, name, err)
		}
		switch name {
		case "InvocationCount":
			if samples[0] == 0 || samples[1] == 0 {
				return errEndpointMetricsPending
			}
			// Prometheus estimates and rounds counts. Check the ordering
			// of the distinct workloads instead of requiring exact counts.
			if samples[1] <= samples[0] || math.Trunc(samples[0]) != samples[0] || math.Trunc(samples[1]) != samples[1] {
				return fmt.Errorf("metrics %s did not distinguish the invocation counts", resultType)
			}
		case "ErrorRate":
			if samples != [2]float64{} {
				return fmt.Errorf("metrics %s reported errors for successful calls", resultType)
			}
		}
	}
	return nil
}

func endpointMetricSamples(raw, resultType string, ids [2]string, timestamp int64) ([2]float64, error) {
	var values [2]float64
	var payload struct {
		ResultType string `json:"resultType"`
		Result     []struct {
			Metric map[string]string   `json:"metric"`
			Value  []json.RawMessage   `json:"value"`
			Values [][]json.RawMessage `json:"values"`
		} `json:"result"`
	}
	if json.Unmarshal([]byte(raw), &payload) != nil || payload.ResultType != resultType || payload.Result == nil {
		return values, errors.New("invalid or missing metric result")
	}
	var seen [2]bool
	for _, series := range payload.Result {
		i := 0
		id := series.Metric["endpoint_id"]
		if id == ids[1] {
			i = 1
		} else if id != ids[0] {
			return values, errors.New("metric result includes an unrequested Endpoint")
		}
		if seen[i] {
			return values, errors.New("metric result includes a duplicate Endpoint")
		}
		seen[i] = true
		point := series.Value
		switch resultType {
		case "matrix":
			if series.Value != nil || len(series.Values) != 1 {
				return values, errors.New("expected one point in the complete trend bucket")
			}
			point = series.Values[0]
		case "vector":
			if series.Values != nil {
				return values, errors.New("summary has trend points")
			}
		}
		var at int64
		var number string
		if len(point) != 2 || json.Unmarshal(point[0], &at) != nil || at != timestamp ||
			json.Unmarshal(point[1], &number) != nil {
			return values, errors.New("invalid metric point or timestamp")
		}
		value, err := strconv.ParseFloat(number, 64)
		if err != nil || math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
			return values, errors.New("invalid metric sample value")
		}
		values[i] = value
	}
	if !seen[0] || !seen[1] {
		return values, errEndpointMetricsPending
	}
	return values, nil
}
