package session

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"text/tabwriter"
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
		object, fields = "SessionSpace", []string{"SpaceId", "Name", "Description", "Status", "Default", "CreateTime", "UpdateTime"}
	case "session.create", "session.get", "session.update":
		object, fields = "Session", []string{"SessionId", "SpaceId", "UserId", "Title", "EventCount", "State", "Metadata", "CreateTime", "UpdateTime"}
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
			pairs := make([][2]string, 0, len(fields))
			for _, field := range fields {
				if value, present := entry[field]; present {
					pairs = append(pairs, [2]string{field, sessionTextValue(value)})
				}
			}
			printKV(w, pairs)
			return
		}
		switch id {
		case "session.space.delete":
			fmt.Fprintln(w, "Deleted session space.")
		case "session.delete":
			fmt.Fprintln(w, "Deleted session.")
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

func printKV(w io.Writer, pairs [][2]string) {
	width := 0
	for _, pair := range pairs {
		width = max(width, len(pair[0]))
	}
	for _, pair := range pairs {
		fmt.Fprintf(w, "%-*s  %s\n", width, pair[0]+":", pair[1])
	}
}

func printTableWithPagination(w io.Writer, headers []string, rows [][]string, shown, total int) {
	table := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(table, strings.Join(headers, "\t"))
	for _, row := range rows {
		fmt.Fprintln(table, strings.Join(row, "\t"))
	}
	_ = table.Flush()
	if total > shown {
		fmt.Fprintf(w, "\nShowing %d of %d items (use --offset and --limit for pagination)\n", shown, total)
	}
}
