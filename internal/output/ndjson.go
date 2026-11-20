package output

import (
	"encoding/json"
	"io"
	"time"
)

// NDJSONEvent is one line-delimited event emitted by streaming commands.
type NDJSONEvent struct {
	SchemaVersion string   `json:"SchemaVersion"`
	Type          string   `json:"Type"`
	Command       string   `json:"Command"`
	Timestamp     string   `json:"Timestamp"`
	Data          any      `json:"Data"`
	Failure       *Failure `json:"Failure"`
}

// NDJSONWriter writes command lifecycle and stream events as NDJSON.
type NDJSONWriter struct {
	command string
	encoder *json.Encoder
}

// NewNDJSONWriter creates a writer that emits NDJSON events to w.
func NewNDJSONWriter(w io.Writer, command string) *NDJSONWriter {
	return &NDJSONWriter{command: command, encoder: json.NewEncoder(w)}
}

func (w *NDJSONWriter) writeEvent(eventType string, data any, failure *Failure) error {
	return w.encoder.Encode(NDJSONEvent{
		SchemaVersion: "agr.events.v1",
		Type:          eventType,
		Command:       w.command,
		Timestamp:     time.Now().UTC().Format(time.RFC3339),
		Data:          data,
		Failure:       failure,
	})
}

// WriteStarted emits the initial streaming event.
func (w *NDJSONWriter) WriteStarted(data any) error { return w.writeEvent("started", data, nil) }

// WriteStdout emits a stdout chunk event.
func (w *NDJSONWriter) WriteStdout(chunk string) error {
	return w.writeEvent("stdout", map[string]string{"Chunk": chunk}, nil)
}

// WriteStderr emits a stderr chunk event.
func (w *NDJSONWriter) WriteStderr(chunk string) error {
	return w.writeEvent("stderr", map[string]string{"Chunk": chunk}, nil)
}

// WriteEvent emits a command-specific intermediate event.
func (w *NDJSONWriter) WriteEvent(eventType string, data any) error {
	return w.writeEvent(eventType, data, nil)
}

// WriteCompleted emits the terminal success event.
func (w *NDJSONWriter) WriteCompleted(data any) error { return w.writeEvent("completed", data, nil) }

// WriteFailed emits the terminal failure event.
func (w *NDJSONWriter) WriteFailed(data any, f *Failure) error {
	return w.writeEvent("failed", data, f)
}
