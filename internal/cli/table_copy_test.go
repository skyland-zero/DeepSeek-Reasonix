package cli

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// tableCopyLines renders the user report table at the given width and returns
// the wrapped visual lines as copyTranscriptLine values (like the copy path
// sees them after parseCopyTranscript).
func tableCopyLines(t *testing.T, w int) []copyTranscriptLine {
	t.Helper()
	out := newMarkdownRenderer(w).Render(userReviewTable)
	raw := strings.Split(strings.TrimRight(out, "\n"), "\n")
	lines := make([]copyTranscriptLine, len(raw))
	for i, l := range raw {
		lines[i] = copyTranscriptLine{text: l}
	}
	return lines
}

func tableLineIndex(t *testing.T, lines []copyTranscriptLine, substr string) int {
	t.Helper()
	for i, l := range lines {
		if strings.Contains(ansi.Strip(l.text), substr) {
			return i
		}
	}
	t.Fatalf("line containing %q not found", substr)
	return -1
}

// TestSelectedCopyTablePrefersSameColumn is the regression test for the
// wrapped-line cross-column copy: a drag inside the 证据 cell across its
// wrapped continuation lines must copy only that cell's text — no rails, no
// neighbouring columns that share the same visual columns on later lines.
func TestSelectedCopyTablePrefersSameColumn(t *testing.T) {
	lines := tableCopyLines(t, 120)
	rails, ok := tableRailColumns(lines[0].text)
	if !ok {
		t.Fatal("no rails on the top border line")
	}
	evLo, evHi, _ := cellInterior(rails, 2) // 证据 cell interior
	first := tableLineIndex(t, lines, "Student.Web/4-Student.Web.csproj")
	last := tableLineIndex(t, lines, "2.20.8）")
	if first == last {
		t.Fatal("cell content did not wrap across visual lines at width 120")
	}

	got := selectedCopyText(lines, selPos{line: first, col: evLo}, selPos{line: last, col: evHi - 1}, nil)
	plain := ansi.Strip(got)
	for _, want := range []string{
		"Student.Web/4-Student.Web.csproj", "AspNet.ScriptManager", "2.20.8）",
	} {
		if !strings.Contains(plain, want) {
			t.Errorf("copy missing %q:\n%s", want, got)
		}
	}
	for _, leak := range []string{"│", "──", "问题", "建议", "逐一核对", "构建脚本"} {
		if strings.Contains(plain, leak) {
			t.Errorf("copy leaked %q (crossed columns after wrap):\n%s", leak, got)
		}
	}
}

// TestSelectedCopyTableJoinsCellsWithTab checks a drag spanning two cells on
// one visual line: both cells' text is copied, joined with a tab, with no
// rails between them.
func TestSelectedCopyTableJoinsCellsWithTab(t *testing.T) {
	lines := tableCopyLines(t, 120)
	rails, _ := tableRailColumns(lines[0].text)
	evLo, _, _ := cellInterior(rails, 2)
	_, suHi, _ := cellInterior(rails, 3)
	first := tableLineIndex(t, lines, "Student.Web/4-Student.Web.csproj")

	got := selectedCopyText(lines, selPos{line: first, col: evLo + 1}, selPos{line: first, col: suHi - 1}, nil)
	plain := ansi.Strip(got)
	if !strings.Contains(plain, "\t") {
		t.Errorf("expected tab-joined cells, got:\n%s", got)
	}
	if strings.Contains(plain, "│") {
		t.Errorf("rails leaked into multi-cell copy:\n%s", got)
	}
	if !strings.Contains(plain, "逐一核对") {
		t.Errorf("second cell missing from multi-cell copy:\n%s", got)
	}
}

// TestSelectedCopyTableSkipsBorderRows verifies a full-table selection copies
// every cell's text but never the box-drawing borders or the rails.
func TestSelectedCopyTableSkipsBorderRows(t *testing.T) {
	lines := tableCopyLines(t, 120)
	start := selPos{line: 0, col: 0}
	end := selPos{line: len(lines) - 1, col: ansi.StringWidth(lines[len(lines)-1].text)}
	got := selectedCopyText(lines, start, end, nil)
	plain := ansi.Strip(got)
	for _, leak := range []string{"│", "─", "┌", "┐"} {
		if strings.Contains(plain, leak) {
			t.Errorf("box drawing leaked into full-table copy:\n%s", plain)
		}
	}
	for _, want := range []string{"Student.Web", "构建脚本重复", "残留文件与副本", "前端外部依赖残留", "问题", "证据", "建议"} {
		if !strings.Contains(plain, want) {
			t.Errorf("full-table copy missing %q:\n%s", want, plain)
		}
	}
}

// TestSelectedCopyPlainSelectionUnchanged guards the non-table path: text
// without rails must keep the exact rectangular extraction semantics.
func TestSelectedCopyPlainSelectionUnchanged(t *testing.T) {
	rendered := newMarkdownRenderer(40).Render("一段普通文本，跨行 wrap 后依然按矩形复制，不吸附任何单元格。")
	raw := strings.Split(strings.TrimRight(rendered, "\n"), "\n")
	lines := make([]copyTranscriptLine, len(raw))
	for i, l := range raw {
		lines[i] = copyTranscriptLine{text: l}
	}
	start, end := selPos{line: 0, col: 2}, selPos{line: len(lines) - 1, col: 6}
	got := selectedCopyText(lines, start, end, nil)

	var want []string
	for i := start.line; i <= end.line; i++ {
		lo, hi := 0, ansi.StringWidth(lines[i].text)
		if i == start.line {
			lo = start.col
		}
		if i == end.line {
			hi = end.col
		}
		want = append(want, strings.TrimRight(ansi.Strip(ansi.Cut(lines[i].text, lo, hi)), " "))
	}
	if got != strings.Join(want, "\n") {
		t.Errorf("plain selection semantics changed:\n got  %q\n want %q", got, strings.Join(want, "\n"))
	}
}

// TestTableGeometryHelpers pins the rail/interior/index arithmetic: CJK cells
// are 2 columns wide, interiors exclude both padding columns, and boundary
// columns resolve to the cell on the right.
func TestTableGeometryHelpers(t *testing.T) {
	rails, ok := tableRailColumns("│ ab │ 中 │ cd │")
	if !ok {
		t.Fatal("expected ≥2 rails")
	}
	want := []int{0, 5, 10, 15}
	for i := range want {
		if i >= len(rails) || rails[i] != want[i] {
			t.Fatalf("rails = %v, want %v", rails, want)
		}
	}
	if lo, hi, ok := cellInterior(rails, 0); !ok || lo != 2 || hi != 4 {
		t.Errorf("cell 0 interior = [%d,%d) ok=%v, want [2,4)", lo, hi, ok)
	}
	if lo, hi, ok := cellInterior(rails, 1); !ok || lo != 7 || hi != 9 {
		t.Errorf("cell 1 (CJK) interior = [%d,%d) ok=%v, want [7,9)", lo, hi, ok)
	}
	if _, _, ok := cellInterior(rails, 3); ok {
		t.Error("cell 3 (out of range) should not resolve")
	}
	if got := cellIndexAt(rails, 8); got != 1 {
		t.Errorf("column 8 → cell %d, want 1", got)
	}
	if got := cellIndexAt(rails, 5); got != 1 {
		t.Errorf("rail boundary column 5 → cell %d, want 1 (cell on the right)", got)
	}
	if got := cellIndexAt(rails, 0); got != 0 {
		t.Errorf("column 0 → cell %d, want 0", got)
	}
	if got := cellIndexAt(rails, 99); got != 2 {
		t.Errorf("column 99 → cell %d, want 2", got)
	}

	// Fenced code lines ("│ " prefix + one rail) are not tables.
	if _, ok := tableRailColumns("  │ echo hello"); ok {
		t.Error("fenced-code line must not look like a table")
	}
	if !hasTableContent("  │ ab │ cd │") {
		t.Error("content row must count as table content")
	}
	if hasTableContent("  ├──────┼────┤") {
		t.Error("border row must not count as table content")
	}
}

// TestSelectedCopyTableMixedSelection checks a drag that starts outside a
// table and ends inside it: the table lines snap, the paragraph line keeps
// its rectangular window.
func TestSelectedCopyTableMixedSelection(t *testing.T) {
	md := "前言段落文字。\n\n" + userReviewTable
	rendered := newMarkdownRenderer(120).Render(md)
	raw := strings.Split(strings.TrimRight(rendered, "\n"), "\n")
	lines := make([]copyTranscriptLine, len(raw))
	for i, l := range raw {
		lines[i] = copyTranscriptLine{text: l}
	}
	rails, _ := tableRailColumns(raw[2])
	_, evHi, _ := cellInterior(rails, 2)

	got := selectedCopyText(lines,
		selPos{line: 0, col: 1},
		selPos{line: tableLineIndex(t, lines, "2.20.8）"), col: evHi - 1}, nil)
	plain := ansi.Strip(got)
	if !strings.Contains(plain, "前言段落文字") {
		t.Errorf("paragraph line dropped:\n%s", got)
	}
	if !strings.Contains(plain, "2.20.8）") {
		t.Errorf("table cell tail missing:\n%s", got)
	}
	if strings.Contains(plain, "│") {
		t.Errorf("rails leaked in mixed selection:\n%s", got)
	}
}

// cellSourceTable mirrors the field report's long CJK cell: a single
// sentence of mixed punctuation that wraps across several visual lines.
const cellSourceTable = `| 示例 | 测试 | 内容 |
|---|---|---|
| a | b | 本单元格内容包含逗号,句号。以及各种标点符号——破折号、分号、冒号,甚至还有括号(圆括号)和[方括号],用来验证它们在 Markdown表格单元格中是否能够被正确、完整地解析和展示。 |
| c | d | 第二行单元格内容。 |
`

const cellSourceWant = "本单元格内容包含逗号,句号。以及各种标点符号——破折号、分号、冒号,甚至还有括号(圆括号)和[方括号],用来验证它们在 Markdown表格单元格中是否能够被正确、完整地解析和展示。"

// sourceResolverLines renders markdown as one transcript block and returns
// the copy lines plus a source resolver wired to that block.
func sourceResolverLines(t *testing.T, md string, width int) ([]copyTranscriptLine, func(line, col int) (string, int, bool)) {
	t.Helper()
	rendered := newMarkdownRenderer(width).Render(md)
	raw := strings.Split(strings.TrimRight(rendered, "\n"), "\n")
	lines := make([]copyTranscriptLine, len(raw))
	for i, l := range raw {
		lines[i] = copyTranscriptLine{text: l}
	}
	resolver := buildTableCellResolver(raw,
		[]string{strings.TrimRight(rendered, "\n")},
		[]transcriptSource{{kind: transcriptSourceMarkdown, raw: md}},
		width)
	return lines, resolver
}

// TestSelectedCopyTableCellSourceSingleLine is the plan-B core: a full drag
// inside one wrapped cell copies the original source text as a single line —
// no renderer wrap newlines, no broken words.
func TestSelectedCopyTableCellSourceSingleLine(t *testing.T) {
	const width = 80
	lines, resolver := sourceResolverLines(t, cellSourceTable, width)
	rails, _ := tableRailColumns(lines[0].text)
	cl, ch, _ := cellInterior(rails, 2) // 内容 column
	first := tableLineIndex(t, lines, "本单元格内容包含逗号")
	last := tableLineIndex(t, lines, "解析和展示。")
	if first == last {
		t.Fatal("long cell did not wrap at width 80")
	}

	got := selectedCopyText(lines, selPos{line: first, col: cl}, selPos{line: last, col: ch}, resolver)
	if strings.Contains(ansi.Strip(got), "\n") {
		t.Errorf("whole-cell copy must be a single line:\n%s", got)
	}
	if ansi.Strip(got) != cellSourceWant {
		t.Errorf("whole-cell copy:\n got  %q\n want %q", ansi.Strip(got), cellSourceWant)
	}
}

// TestSelectedCopyTableCellSourceMultiRow: dragging the same column across
// two logical rows copies each row's cell source, joined with "\n".
func TestSelectedCopyTableCellSourceMultiRow(t *testing.T) {
	const width = 80
	lines, resolver := sourceResolverLines(t, cellSourceTable, width)
	rails, _ := tableRailColumns(lines[0].text)
	cl, ch, _ := cellInterior(rails, 2)
	row1 := tableLineIndex(t, lines, "本单元格内容包含逗号")
	row2 := tableLineIndex(t, lines, "第二行单元格内容")

	got := selectedCopyText(lines, selPos{line: row1, col: cl}, selPos{line: row2, col: ch}, resolver)
	if got != cellSourceWant+"\n第二行单元格内容。" {
		t.Errorf("multi-row same-column copy:\n got  %q", got)
	}
}

// TestSelectedCopyTableCellSourcePartial: a partial drag of one cell keeps
// the geometric per-line extraction — no source reconstruction.
func TestSelectedCopyTableCellSourcePartial(t *testing.T) {
	const width = 80
	lines, resolver := sourceResolverLines(t, cellSourceTable, width)
	rails, _ := tableRailColumns(lines[0].text)
	cl, ch, _ := cellInterior(rails, 2)
	first := tableLineIndex(t, lines, "本单元格内容包含逗号")
	last := tableLineIndex(t, lines, "解析和展示。")

	got := selectedCopyText(lines, selPos{line: first, col: cl + 3}, selPos{line: last, col: ch - 3}, resolver)
	plain := ansi.Strip(got)
	if plain == cellSourceWant {
		t.Errorf("partial drag must not reconstruct the full source")
	}
	if !strings.Contains(plain, "\n") {
		t.Errorf("partial drag must keep per-line pieces, got single line:\n%s", got)
	}
}

// TestSelectedCopyTableCellSourceSecondTable: two tables in one block — the
// resolver must hit the second table's AST cells via its ordinal.
func TestSelectedCopyTableCellSourceSecondTable(t *testing.T) {
	const width = 80
	md := cellSourceTable + "\n\n| 甲 | 乙 |\n|---|---|\n| 1 | 2 |\n"
	lines, resolver := sourceResolverLines(t, md, width)
	t2 := tableLineIndex(t, lines, "甲")
	rails, ok := tableRailColumns(lines[t2].text)
	if !ok {
		t.Fatalf("second table border not found near %q", "甲")
	}
	// second table's cells: find the data row "2"
	rowLine := tableLineIndex(t, lines, "2")
	if rowLine <= t2 {
		t.Fatal("did not find the second table's data row")
	}
	cl, ch, ok := cellInterior(rails, 1)
	if !ok {
		t.Fatal("second table column 1 interior missing")
	}
	got := selectedCopyText(lines, selPos{line: rowLine, col: cl}, selPos{line: rowLine, col: ch}, resolver)
	if ansi.Strip(got) != "2" {
		t.Errorf("second-table cell copy = %q, want \"2\"", ansi.Strip(got))
	}
}

// TestSelectedCopyTableCellSourceFallback: when the block has no markdown
// source (fixed kind), the resolver refuses and the geometric path takes
// over — no crash, no reconstruction.
func TestSelectedCopyTableCellSourceFallback(t *testing.T) {
	const width = 80
	rendered := newMarkdownRenderer(width).Render(cellSourceTable)
	raw := strings.Split(strings.TrimRight(rendered, "\n"), "\n")
	lines := make([]copyTranscriptLine, len(raw))
	for i, l := range raw {
		lines[i] = copyTranscriptLine{text: l}
	}
	resolver := buildTableCellResolver(raw,
		[]string{strings.TrimRight(rendered, "\n")},
		[]transcriptSource{{kind: transcriptSourceFixed}},
		width)
	rails, _ := tableRailColumns(lines[0].text)
	cl, ch, _ := cellInterior(rails, 2)
	first := tableLineIndex(t, lines, "本单元格内容包含逗号")
	last := tableLineIndex(t, lines, "解析和展示。")

	got := selectedCopyText(lines, selPos{line: first, col: cl}, selPos{line: last, col: ch}, resolver)
	plain := ansi.Strip(got)
	if plain == cellSourceWant {
		t.Errorf("fixed block must not resolve cell sources")
	}
	if !strings.Contains(plain, "\n") {
		t.Errorf("fallback must keep per-line pieces, got single line:\n%s", got)
	}
	if strings.Contains(plain, "│") {
		t.Errorf("fallback leaked rails:\n%s", got)
	}
}

// TestTableRowIndex pins the border-rule row counting: header is row 0, each
// separator advances the row.
func TestTableRowIndex(t *testing.T) {
	lines := []string{
		"┌──────┬──────┐", // top border
		"│ 姓名 │ 部门 │",     // header content (row 0)
		"├──────┼──────┤", // header separator
		"│ 张三 │ 研发 │",     // row 1
		"│ 李四 │ 测试 │",     // row 1 (wrapped continuation)
		"├──────┼──────┤", // separator
		"│ 王五 │ 运维 │",     // row 2
		"└──────┴──────┘", // bottom border
	}
	region := tableBlockRegion{startLine: 0, endLine: len(lines) - 1}
	for line, want := range map[int]int{1: 0, 3: 1, 4: 1, 6: 2} {
		if got := tableRowIndex(lines, region, line); got != want {
			t.Errorf("tableRowIndex(line %d) = %d, want %d", line, got, want)
		}
	}
}

// TestBuildTableCellResolverBlockBoundary maps lines across a block boundary:
// the paragraph block refuses, the markdown table block resolves.
func TestBuildTableCellResolverBlockBoundary(t *testing.T) {
	const width = 80
	para := "前言段落。"
	paraRendered := newMarkdownRenderer(width).Render(para)
	tableRendered := newMarkdownRenderer(width).Render(cellSourceTable)
	blocks := []string{
		strings.TrimRight(paraRendered, "\n"),
		strings.TrimRight(tableRendered, "\n"),
	}
	flat := []string{}
	for _, b := range blocks {
		flat = append(flat, strings.Split(b, "\n")...)
	}
	sources := []transcriptSource{
		{kind: transcriptSourceFixed},
		{kind: transcriptSourceMarkdown, raw: cellSourceTable},
	}
	resolver := buildTableCellResolver(flat, blocks, sources, width)
	if resolver == nil {
		t.Fatal("resolver must build")
	}
	if text, row, ok := resolver(0, 0); ok {
		t.Errorf("paragraph line resolved as cell: %q row=%d", text, row)
	}
	// the row-2 cell line of the table block (its last content line)
	target := -1
	for i, l := range flat {
		if strings.Contains(l, "第二行单元格内容") {
			target = i
		}
	}
	if target < 0 {
		t.Fatal("second-row cell line not found")
	}
	if text, row, ok := resolver(target, 2); !ok {
		t.Errorf("table line %d not resolved", target)
	} else if row != 2 {
		t.Errorf("row = %d, want 2", row)
	} else if text != "第二行单元格内容。" {
		t.Errorf("cell text = %q, want the second row cell", text)
	}
}
