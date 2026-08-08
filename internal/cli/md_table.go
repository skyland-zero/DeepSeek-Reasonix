package cli

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// renderTableGrid renders a table with box-drawing grid borders.
// open=true means the table may still grow — a streamed table whose final
// rows are not yet known — so the bottom border is omitted and new rows
// appear in-place; the caller re-renders with open=false once the table
// closes or the answer is committed.
func renderTableGrid(buf *strings.Builder, header []string, rows [][]string, widths []int, indent int, open bool) {
	cols := len(widths)
	if cols == 0 {
		return
	}
	prefix := strings.Repeat(" ", indent)

	border := func(left, mid, right string) string {
		var b strings.Builder
		b.WriteString(dim(left))
		for i := 0; i < cols; i++ {
			if i > 0 {
				b.WriteString(dim(mid))
			}
			b.WriteString(dim(strings.Repeat("─", widths[i]+2)))
		}
		b.WriteString(dim(right))
		return b.String()
	}

	// Top border — always drawn, even in streaming mode.
	buf.WriteString(prefix)
	buf.WriteString(border("┌", "┬", "┐"))
	buf.WriteByte('\n')

	// Header row.
	if len(header) > 0 {
		renderGridRow(buf, prefix, header, widths, cols, true)
		buf.WriteString(prefix)
		buf.WriteString(border("├", "┼", "┤"))
		buf.WriteByte('\n')
	}

	// Data rows with separators between them.
	for i, row := range rows {
		if i > 0 {
			buf.WriteString(prefix)
			buf.WriteString(border("├", "┼", "┤"))
			buf.WriteByte('\n')
		}
		renderGridRow(buf, prefix, row, widths, cols, false)
	}

	// Bottom border — skipped while the table may still grow.
	if !open {
		buf.WriteString(prefix)
		buf.WriteString(border("└", "┴", "┘"))
		buf.WriteByte('\n')
	}
}

// normalizeCell collapses runs of whitespace — including the model's own
// line breaks inside a cell, which goldmark surfaces as soft breaks — into
// single spaces. Without this, word wrap honours the author's arbitrary break
// points and splits cells at ragged, non-column widths (e.g. 37/66/76 chars
// instead of the column width).
func normalizeCell(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// renderGridRow lays out one logical row across multiple visual rows when
// any cell wraps. Each cell is framed by │ borders with 1-column padding on
// each side so content never touches the rails.
func renderGridRow(buf *strings.Builder, prefix string, cells []string, widths []int, cols int, isHeader bool) {
	wrapped := make([][]string, cols)
	maxLines := 1
	for i := 0; i < cols; i++ {
		var text string
		if i < len(cells) {
			text = cells[i]
		}
		wrapped[i] = strings.Split(wrapAnsi(normalizeCell(text), widths[i]), "\n")
		if len(wrapped[i]) > maxLines {
			maxLines = len(wrapped[i])
		}
	}
	for line := 0; line < maxLines; line++ {
		buf.WriteString(prefix)
		buf.WriteString(dim("│"))
		for i := 0; i < cols; i++ {
			buf.WriteByte(' ')
			var cell string
			if line < len(wrapped[i]) {
				cell = wrapped[i][line]
			}
			// Truncate first (CJK content may overflow the column width),
			// then pad to the exact column width.
			truncated := ansi.Truncate(cell, widths[i], "")
			padded := padRight(truncated, widths[i])
			if isHeader {
				padded = bold(padded)
			}
			buf.WriteString(padded)
			buf.WriteByte(' ')
			buf.WriteString(dim("│"))
		}
		buf.WriteByte('\n')
	}
}
