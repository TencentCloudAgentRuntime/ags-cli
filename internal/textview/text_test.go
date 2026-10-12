package textview

import (
	"bytes"
	"testing"
)

func TestPrintKVAlignment(t *testing.T) {
	var out bytes.Buffer
	PrintKV(&out, []KeyValue{{Key: "ID", Value: "session-1"}, {Key: "Name", Value: "demo"}})
	const want = "ID:   session-1\nName:  demo\n"
	if got := out.String(); got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestPrintTablePagination(t *testing.T) {
	for _, tc := range []struct {
		name  string
		rows  [][]string
		total int
		want  string
	}{
		{"complete", [][]string{{"1", "demo"}}, 1, "ID  TITLE\n1   demo\n"},
		{"more", [][]string{{"1", "demo"}}, 2, "ID  TITLE\n1   demo\n\nShowing 1 of 2 items (use --offset and --limit for pagination)\n"},
		{"empty", nil, 0, "ID  TITLE\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			PrintTableWithPagination(&out, []string{"ID", "TITLE"}, tc.rows, len(tc.rows), tc.total)
			if got := out.String(); got != tc.want {
				t.Fatalf("output = %q, want %q", got, tc.want)
			}
		})
	}
}
