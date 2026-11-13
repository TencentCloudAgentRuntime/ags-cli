package patchscenarios

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

type runtimeEvent struct {
	Type    string
	Data    map[string]any
	Failure map[string]any
	Result  map[string]any
	Error   any
}

func decodeRuntimeEvents(raw []byte) ([]runtimeEvent, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var events []runtimeEvent
	for {
		var event runtimeEvent
		if err := decoder.Decode(&event); errors.Is(err, io.EOF) {
			return events, nil
		} else if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
}

func (r *harnessRuntime) events(args ...string) ([]runtimeEvent, error) {
	raw, err := r.command(r.t.Context(), append(args, "-o", "ndjson")...).Output()
	events, decodeErr := decodeRuntimeEvents(raw)
	if decodeErr != nil {
		return nil, decodeErr
	}
	return events, err
}

func (r *harnessRuntime) streamSuccess(session runtimeSession) {
	r.t.Helper()
	args := []string{"harness", "prompt", r.harness, "Reply only AGR_STREAM_OK. Do not use tools.", "--cwd", "/tmp", "--timeout", "120s"}
	events, err := r.events(append(args, session.flags()...)...)
	if err != nil || !runtimeTerminal(events, "started", "completed") {
		r.t.Fatal("invalid successful NDJSON lifecycle")
	}
	data := events[len(events)-1].Data
	requireReply(r.t, data, "AGR_STREAM_OK")
	if sessionOf(r.t, data) != session {
		r.t.Fatal("streaming changed the session or instance affinity")
	}
}

func runtimeTerminal(events []runtimeEvent, first, last string) bool {
	if len(events) < 2 || events[0].Type != first || events[len(events)-1].Type != last {
		return false
	}
	count := 0
	for _, event := range events {
		if event.Type == "completed" || event.Type == "failed" {
			count++
		}
	}
	return count == 1
}

type runtimeProcess struct {
	cmd    *exec.Cmd
	cancel context.CancelFunc
	events <-chan runtimeEvent
	err    <-chan error
	stdin  io.WriteCloser
	waited bool
}

func (r *harnessRuntime) start(args ...string) *runtimeProcess {
	r.t.Helper()
	ctx, cancel := context.WithTimeout(r.t.Context(), 2*time.Minute)
	cmd := r.command(ctx, args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		r.t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		r.t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		cancel()
		r.t.Fatal(err)
	}
	events, failures := make(chan runtimeEvent, 64), make(chan error, 1)
	go func() {
		defer close(events)
		decoder := json.NewDecoder(stdout)
		decoder.UseNumber()
		for {
			var event runtimeEvent
			if err := decoder.Decode(&event); err != nil {
				if !errors.Is(err, io.EOF) {
					failures <- errors.New("invalid process protocol output")
				}
				close(failures)
				return
			}
			select {
			case events <- event:
			case <-ctx.Done():
				close(failures)
				return
			}
		}
	}()
	process := &runtimeProcess{cmd: cmd, cancel: cancel, events: events, err: failures, stdin: stdin}
	r.t.Cleanup(func() {
		if !process.waited {
			cancel()
			_ = cmd.Wait()
		}
		cancel()
	})
	return process
}

func (p *runtimeProcess) next(t *testing.T) runtimeEvent {
	t.Helper()
	timer := time.NewTimer(45 * time.Second)
	defer timer.Stop()
	select {
	case event, ok := <-p.events:
		if !ok {
			t.Fatal("process closed stdout before the expected protocol message")
		}
		return event
	case <-timer.C:
		t.Fatal("process protocol message exceeded its deadline")
		return runtimeEvent{}
	}
}

func (p *runtimeProcess) finish(t *testing.T, interrupt bool) ([]runtimeEvent, error) {
	t.Helper()
	started := time.Now()
	if interrupt {
		if err := p.cmd.Process.Signal(os.Interrupt); err != nil {
			t.Fatal("could not interrupt the live CLI")
		}
	}
	timer := time.AfterFunc(15*time.Second, p.cancel)
	defer timer.Stop()
	var events []runtimeEvent
	for event := range p.events {
		events = append(events, event)
	}
	err := p.cmd.Wait()
	p.waited = true
	if time.Since(started) >= 15*time.Second {
		t.Fatal("CLI did not exit within 15 seconds of EOF/interrupt")
	}
	if readErr := <-p.err; readErr != nil {
		t.Fatal(readErr)
	}
	return events, err
}

func (r *harnessRuntime) stdio() {
	r.t.Helper()
	p := r.start("harness", "acp", r.harness)
	initialize := map[string]any{"jsonrpc": "2.0", "id": 171, "method": "initialize",
		"params": map[string]any{"protocolVersion": 1, "clientCapabilities": map[string]any{},
			"clientInfo": map[string]any{"name": "agr-live-e2e", "version": "1"}}}
	if err := json.NewEncoder(p.stdin).Encode(initialize); err != nil {
		r.t.Fatal(err)
	}
	reply := p.next(r.t)
	if reply.Error != nil || harnessInt(reply.Result["protocolVersion"]) != 1 {
		r.t.Fatal("stdio ACP initialization failed")
	}
	_ = p.stdin.Close()
	if _, err := p.finish(r.t, false); err != nil {
		r.t.Fatal("stdio ACP did not exit successfully after EOF")
	}
}

func (r *harnessRuntime) proxy() {
	r.t.Helper()
	p := r.start("harness", "proxy", r.harness, "--port", "0", "-o", "ndjson")
	ready := p.next(r.t)
	if ready.Type != "ready" || !strings.HasPrefix(harnessText(ready.Data["AcpURL"]), "http://127.0.0.1:") {
		r.t.Fatal("proxy did not return its local ready URL")
	}
	address := harnessText(ready.Data["AcpURL"])
	client := &http.Client{Timeout: 40 * time.Second, Transport: &http.Transport{},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	defer client.CloseIdleConnections()
	request, err := http.NewRequestWithContext(r.t.Context(), http.MethodPost, address,
		strings.NewReader("{\"jsonrpc\":\"2.0\",\"id\":171,\"method\":\"initialize\",\"params\":{\"protocolVersion\":1,\"clientCapabilities\":{}}}"))
	if err != nil {
		r.t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		r.t.Fatal("proxy ACP request failed")
	}
	connection := response.Header.Get("Acp-Connection-Id")
	var reply runtimeEvent
	decoder := json.NewDecoder(response.Body)
	decoder.UseNumber()
	decodeErr := decoder.Decode(&reply)
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK || connection == "" || decodeErr != nil || reply.Error != nil || harnessInt(reply.Result["protocolVersion"]) != 1 {
		r.t.Fatal("proxy ACP initialization failed")
	}
	request, err = http.NewRequestWithContext(r.t.Context(), http.MethodDelete, address, nil)
	if err != nil {
		r.t.Fatal(err)
	}
	request.Header.Set("Acp-Connection-Id", connection)
	response, err = client.Do(request)
	if err != nil {
		r.t.Fatal("proxy ACP connection deletion failed")
	}
	_ = response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		r.t.Fatal("proxy ACP connection deletion was rejected")
	}
	events, err := p.finish(r.t, true)
	if err != nil || !runtimeTerminal(append([]runtimeEvent{ready}, events...), "ready", "completed") {
		r.t.Fatal("proxy did not emit one final completed event and exit successfully")
	}
}

func (r *harnessRuntime) streamCancel(session runtimeSession) {
	r.t.Helper()
	args := []string{"harness", "prompt", r.harness,
		"Print the numbers 1 through 10000, one per line. Begin immediately. Do not use tools.",
		"--cwd", "/tmp", "--timeout", "120s", "-o", "ndjson"}
	p := r.start(append(args, session.flags()...)...)
	var events []runtimeEvent
	for {
		event := p.next(r.t)
		events = append(events, event)
		if event.Type == "update" {
			break
		}
		if event.Type == "failed" || event.Type == "completed" {
			r.t.Fatal("prompt terminated before a real update could trigger cancellation")
		}
	}
	remaining, err := p.finish(r.t, true)
	events = append(events, remaining...)
	if err == nil || !runtimeTerminal(events, "started", "failed") {
		r.t.Fatal("cancelled prompt must exit nonzero with one final failed event")
	}
	last := events[len(events)-1]
	if harnessText(last.Failure["Code"]) != "PROMPT_FAILED" || sessionOf(r.t, last.Data) != session {
		r.t.Fatal("cancelled prompt lost its failure code or recovery identifiers")
	}
}

func mcpEvidence(events []runtimeEvent, tool, expected string) bool {
	if !runtimeTerminal(events, "started", "completed") {
		return false
	}
	last := events[len(events)-1]
	if harnessText(last.Data["StopReason"]) != "end_turn" {
		return false
	}
	for _, call := range runtimeItems(last.Data["ToolCalls"]) {
		if !strings.Contains(harnessText(call["Title"]), tool) || harnessText(call["Status"]) != "completed" {
			continue
		}
		id := harnessText(call["ToolCallId"])
		for _, event := range events {
			if event.Type != "update" || harnessText(event.Data["toolCallId"]) != id || harnessText(event.Data["sessionUpdate"]) != "tool_call_update" {
				continue
			}
			content, err := json.Marshal(event.Data["content"])
			if err == nil && strings.Contains(string(content), expected) {
				return true
			}
		}
	}
	return false
}

// Summaries omit replies, tool inputs/results, credentials and recovery values.
func mcpDiagnostic(events []runtimeEvent, tool, expected string) string {
	if len(events) == 0 {
		return "no protocol events"
	}
	last := events[len(events)-1]
	var calls []string
	for _, call := range runtimeItems(last.Data["ToolCalls"]) {
		title := harnessText(call["Title"])
		if title != "codemode" && !strings.HasPrefix(title, "mcp__") {
			title = "[other]"
		}
		calls = append(calls, fmt.Sprintf("tool=%s target=%t status=%s", title,
			strings.Contains(harnessText(call["Title"]), tool), harnessText(call["Status"])))
	}
	results, rawResults, inputs := 0, 0, 0
	for _, event := range events {
		if event.Type == "update" && harnessText(event.Data["sessionUpdate"]) == "tool_call_update" {
			content, _ := json.Marshal(event.Data["content"])
			if strings.Contains(string(content), expected) {
				results++
			}
			raw, _ := json.Marshal(event.Data["rawOutput"])
			if strings.Contains(string(raw), expected) {
				rawResults++
			}
		}
		if event.Type == "update" {
			raw, _ := json.Marshal(event.Data["rawInput"])
			if strings.Contains(string(raw), tool) {
				inputs++
			}
		}
	}
	return fmt.Sprintf("events=%d terminal=%s stop=%s failure=%s calls=[%s] matching_result_updates=%d matching_raw_results=%d target_inputs=%d",
		len(events), last.Type, harnessText(last.Data["StopReason"]), harnessText(last.Failure["Code"]),
		strings.Join(calls, ", "), results, rawResults, inputs)
}

func TestRuntimeInitializationIsolatesPublishedSnapshots(t *testing.T) {
	raw, err := runtimeInitialization("{\"SandboxTool\":{\"RoleArn\":\"reviewed-role\"},\"Deployment\":{\"ScalingConfiguration\":{\"MaxInstanceCount\":1}}}", 6)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatal(err)
	}
	deployment := harnessObject(out["Deployment"])
	if harnessObject(out["SandboxTool"])["RoleArn"] != "reviewed-role" ||
		harnessObject(deployment["ScalingConfiguration"])["MinInstanceCount"] != float64(0) ||
		harnessObject(deployment["ScalingConfiguration"])["MaxInstanceCount"] != float64(6) ||
		harnessObject(deployment["AffinityConfiguration"])["Mode"] != "EXCLUSIVE_BEST_EFFORT" {
		t.Fatal("runtime fixture lost role or instance isolation")
	}
}

func TestMCPEvidenceRequiresCompletedRealToolContent(t *testing.T) {
	completed := runtimeEvent{Type: "completed", Data: map[string]any{"StopReason": "end_turn",
		"Reply": "expected.example result", "ToolCalls": []any{map[string]any{
			"ToolCallId": "call-one", "Title": "live_search", "Status": "completed"}}}}
	update := runtimeEvent{Type: "update", Data: map[string]any{"sessionUpdate": "tool_call_update",
		"toolCallId": "call-one", "content": []any{map[string]any{"text": "expected.example result"}}}}
	if !mcpEvidence([]runtimeEvent{{Type: "started"}, update, completed}, "live_search", "expected.example") {
		t.Fatal("valid matching tool content was rejected")
	}
	for _, events := range [][]runtimeEvent{
		{{Type: "started"}, completed}, // A model reply or claimed completion is insufficient.
		{{Type: "started"}, {Type: "update", Data: map[string]any{"toolCallId": "different", "sessionUpdate": "tool_call_update", "content": "expected.example"}}, completed},
		{{Type: "started"}, update, {Type: "failed", Data: completed.Data}},
	} {
		if mcpEvidence(events, "live_search", "expected.example") {
			t.Fatal("unproven MCP execution was accepted")
		}
	}
}
