package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
)

func sessionTextRenderer(id string, data any) (func(io.Writer), error) {
	raw, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("encode session response: %w", err)
	}
	var response map[string]any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&response); err != nil {
		return nil, fmt.Errorf("decode session response: %w", err)
	}
	var object, list string
	var fields []string
	switch id {
	case "session.space.create", "session.space.get", "session.space.update":
		object, fields = "SessionSpace", []string{"SpaceId", "Name", "Description", "Status", "Default"}
	case "session.create", "session.get", "session.update":
		object, fields = "Session", []string{"SessionId", "SpaceId", "UserId", "Title", "EventCount", "State", "Metadata"}
	case "session.event.append":
		object, fields = "Event", []string{"EventId", "Author", "Timestamp", "Content"}
	case "session.space.list":
		list, fields = "SessionSpaces", []string{"SpaceId", "Name", "Status", "Default"}
	case "session.list":
		list, fields = "Sessions", []string{"SessionId", "UserId", "Title", "EventCount"}
	case "session.event.list":
		list, fields = "Events", []string{"EventId", "Author", "Timestamp", "Content"}
	}
	return func(w io.Writer) {
		if list != "" {
			items, _ := response[list].([]any)
			rows := make([][]string, 0, len(items))
			for _, item := range items {
				entry, _ := item.(map[string]any)
				row := make([]string, 0, len(fields))
				for _, field := range fields {
					row = append(row, sessionTextValue(entry[field]))
				}
				rows = append(rows, row)
			}
			total, _ := strconv.Atoi(sessionTextValue(response["TotalCount"]))
			printTableWithPagination(w, fields, rows, len(rows), total)
			return
		}
		if object != "" {
			entry, _ := response[object].(map[string]any)
			pairs := make([]KeyValue, 0, len(fields))
			for _, field := range fields {
				if value, present := entry[field]; present {
					pairs = append(pairs, KeyValue{Key: field, Value: sessionTextValue(value)})
				}
			}
			printKV(w, pairs)
			return
		}
		switch id {
		case "session.space.delete":
			fmt.Fprintln(w, "Deleted session space.")
		case "session.delete":
			fmt.Fprintln(w, "Deleted session and its events.")
		}
	}, nil
}

func sessionTextValue(value any) string {
	if value == nil {
		return "-"
	}
	if text, ok := value.(string); ok {
		return text
	}
	raw, _ := json.Marshal(value)
	return string(raw)
}
