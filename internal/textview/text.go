// Package textview provides shared text rendering without command dependencies.
package textview

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
)

// KeyValue is one row in a text key/value view.
type KeyValue struct {
	Key   string
	Value string
}

// PrintKV writes aligned key/value rows.
func PrintKV(w io.Writer, pairs []KeyValue) {
	width := 0
	for _, pair := range pairs {
		width = max(width, len(pair.Key))
	}
	for _, pair := range pairs {
		fmt.Fprintf(w, "%-*s  %s\n", width, pair.Key+":", pair.Value)
	}
}

// PrintTable writes column headers and rows.
func PrintTable(w io.Writer, headers []string, rows [][]string) {
	table := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(table, strings.Join(headers, "\t"))
	for _, row := range rows {
		fmt.Fprintln(table, strings.Join(row, "\t"))
	}
	_ = table.Flush()
}

// PrintTableWithPagination appends a pagination hint when more items are available.
func PrintTableWithPagination(w io.Writer, headers []string, rows [][]string, shown, total int) {
	PrintTable(w, headers, rows)
	if total > shown {
		fmt.Fprintf(w, "\nShowing %d of %d items (use --offset and --limit for pagination)\n", shown, total)
	}
}
