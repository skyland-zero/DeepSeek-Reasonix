package cli

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/yuin/goldmark/ast"
	extast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"
)

// copySpan is a [lo, hi) display-column span to extract from one visual line.
type copySpan struct{ lo, hi int }

// tableSnap is the snapped geometry of one visual line inside a table region:
// the rail columns (identical across every line of a rendered table) and the
// anchored cell-index range the selection covers.
type tableSnap struct {
	rails    []int
	c0, c1   int
	anchored bool
}

// tableBlockRegion is a maximal run of geometrically consistent table lines
// inside one transcript block — one rendered GFM table.
type tableBlockRegion struct {
	startLine, endLine int
	rails              []int
}

// tableRailColumns returns the display columns of every vertical box-drawing
// rail in line (│ content rails plus ┌┬┐├┼┤└┴┘ border corners and junctions)
// and whether the line looks like part of a table (at least two rails). ANSI
// styling is skipped and CJK content pushes rails by its true display width,
// so the positions match what the terminal shows.
func tableRailColumns(line string) ([]int, bool) {
	var cols []int
	col := 0
	for _, r := range ansi.Strip(line) {
		switch r {
		case '│', '┌', '┬', '┐', '├', '┼', '┤', '└', '┴', '┘':
			cols = append(cols, col)
		}
		col += visibleWidth(string(r))
	}
	return cols, len(cols) >= 2
}

// hasTableContent reports whether line is a table content row — its rails are
// │ separators. Border rows (┌┬┐├┼┤└┴┘ only) carry no copyable text.
func hasTableContent(line string) bool {
	return strings.ContainsRune(ansi.Strip(line), '│')
}

// tableSelectionGeometry maps every line in the selection window to the rail
// geometry used to snap that line's window. Lines of one rendered table share
// identical rail columns, so a run of geometrically consistent lines forms a
// region; the run starting on the selection's first line also anchors the cell
// range [c0..c1] the selection covers, keeping the same columns on every
// wrapped continuation line.
func tableSelectionGeometry(lines []string, start, end selPos) map[int]tableSnap {
	if start.line < 0 || start.line >= len(lines) {
		return nil
	}
	var startRails []int
	if r, ok := tableRailColumns(lines[start.line]); ok {
		startRails = r
	}
	snaps := make(map[int]tableSnap)
	cur := startRails
	for idx := start.line; idx <= end.line && idx < len(lines); idx++ {
		rails, ok := tableRailColumns(lines[idx])
		if !ok {
			cur = nil
			continue
		}
		if !equalInts(rails, cur) {
			cur = rails // new region run (or the first one when the start line is not a table)
		}
		anchored := len(startRails) > 0 && equalInts(rails, startRails)
		sn := tableSnap{rails: rails, anchored: anchored}
		if anchored {
			sn.c0, sn.c1 = anchoredCellRange(rails, start.col, end.col)
		}
		snaps[idx] = sn
	}
	return snaps
}

// anchoredCellRange maps the selection's visual columns [sc, hc) — resolved
// on the start line's geometry, which every line of the table shares — to the
// inclusive cell-index range it covers.
func anchoredCellRange(rails []int, sc, hc int) (c0, c1 int) {
	c0 = cellIndexAt(rails, sc)
	c1 = cellIndexAt(rails, max(hc-1, sc))
	return
}

// cellIndexAt returns the cell index whose span contains visual column c. A
// column exactly on a rail resolves to the cell on its right.
func cellIndexAt(rails []int, c int) int {
	if c < rails[0] {
		return 0
	}
	for i := 1; i < len(rails); i++ {
		if c < rails[i] {
			return i - 1
		}
	}
	return len(rails) - 2
}

// cellInterior returns the display-column span of cell i's text: the padding
// space on each side of the rails is excluded, so copying a cell never carries
// rails or padding.
func cellInterior(rails []int, i int) (lo, hi int, ok bool) {
	if i < 0 || i >= len(rails)-1 {
		return 0, 0, false
	}
	lo, hi = rails[i]+2, rails[i+1]-1
	return lo, hi, lo < hi
}

// snappedSpans resolves one visual line's [lo, hi) window into the cell
// interior segments to copy. Lines strictly between the selection's ends keep
// the full anchored cell text (opencode-style), so a drag inside one cell
// across its wrapped continuation lines stays in that cell; only the first
// and last lines clip the first and last cell at lo/hi.
func snappedSpans(sn tableSnap, lo, hi int, isFirst, isLast, content bool) []copySpan {
	if !content {
		return nil // border row: nothing to copy
	}
	c0, c1 := sn.c0, sn.c1
	if !sn.anchored {
		c0 = cellIndexAt(sn.rails, lo)
		c1 = cellIndexAt(sn.rails, max(hi-1, lo))
	}
	if c1 >= len(sn.rails)-1 {
		c1 = len(sn.rails) - 2
	}
	if c0 > c1 {
		return nil
	}
	var out []copySpan
	for c := c0; c <= c1; c++ {
		cl, ch, ok := cellInterior(sn.rails, c)
		if !ok {
			continue
		}
		if c == c0 && isFirst {
			cl = max(cl, lo)
		}
		if c == c1 && isLast {
			ch = min(ch, hi)
		}
		if cl < ch {
			out = append(out, copySpan{lo: cl, hi: ch})
		}
	}
	return out
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// tableBlockRegions finds the table regions inside one block's wrapped lines:
// maximal runs of consecutive lines whose rail columns match (a rendered GFM
// table uses one fixed geometry for every line).
func tableBlockRegions(lines []string) []tableBlockRegion {
	var regions []tableBlockRegion
	start := -1
	var rails []int
	flush := func(end int) {
		if start >= 0 {
			regions = append(regions, tableBlockRegion{startLine: start, endLine: end, rails: rails})
		}
	}
	for i, line := range lines {
		r, ok := tableRailColumns(line)
		switch {
		case !ok:
			flush(i - 1)
			start, rails = -1, nil
		case start < 0:
			start, rails = i, r
		case !equalInts(r, rails):
			flush(i - 1)
			start, rails = i, r
		}
	}
	flush(len(lines) - 1)
	return regions
}

// tableRowIndex returns the logical row of a content line within its region:
// row k is the k-th content run after the k-th horizontal rule (top border,
// header separator, inter-row separators). Row 0 is the header.
func tableRowIndex(lines []string, region tableBlockRegion, line int) int {
	row := -1
	for i := region.startLine; i < line; i++ {
		if !hasTableContent(lines[i]) {
			row++
		}
	}
	return row
}

// tableParse holds a goldmark parse of one block's raw markdown plus the
// tables found in render order, so repeated cell lookups on the same block
// reparse nothing.
type tableParse struct {
	r      *mdRenderer
	src    []byte
	tables []*extast.Table
}

func parseTableBlock(raw string) *tableParse {
	r := newMarkdownRenderer(0)
	src := []byte(raw)
	doc := r.md.Parser().Parse(text.NewReader(src))
	p := &tableParse{r: r, src: src}
	collectTables(doc, &p.tables)
	return p
}

// cell returns the original single-line text of one table cell — the source
// as the model wrote it, with whitespace collapsed exactly like the display
// (normalizeCell), so the copy is the unwrapped form of what is on screen.
func (p *tableParse) cell(tableOrdinal, row, col int) (string, bool) {
	if tableOrdinal < 0 || tableOrdinal >= len(p.tables) {
		return "", false
	}
	idx := 0
	for c := p.tables[tableOrdinal].FirstChild(); c != nil; c = c.NextSibling() {
		var cells []*extast.TableCell
		for cc := c.FirstChild(); cc != nil; cc = cc.NextSibling() {
			if cell, ok := cc.(*extast.TableCell); ok {
				cells = append(cells, cell)
			}
		}
		if idx == row {
			if col < 0 || col >= len(cells) {
				return "", false
			}
			text := normalizeCell(strings.TrimSpace(p.r.collectInline(cells[col], p.src)))
			if text == "" {
				return "", false
			}
			return text, true
		}
		idx++
	}
	return "", false
}

// extractTableCellText parses raw markdown and returns the text of one cell;
// tableOrdinal counts tables in render order (0-based), row 0 is the header.
func extractTableCellText(raw string, tableOrdinal, row, col int) (string, bool) {
	return parseTableBlock(raw).cell(tableOrdinal, row, col)
}

// collectTables appends every GFM table node in renderBlocks order. Walking
// every child is equivalent: only block containers can hold nested tables,
// and goldmark never nests tables inside table cells.
func collectTables(n ast.Node, out *[]*extast.Table) {
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		if t, ok := c.(*extast.Table); ok {
			*out = append(*out, t)
		}
		collectTables(c, out)
	}
}

// buildTableCellResolver maps a visual transcript line and cell column back
// to the cell's original source text (plus its logical row). It is built
// from the rendered blocks and their semantic sources; nil when the
// per-block line mapping cannot be established (the caller then falls back
// to the geometric per-line extraction).
func buildTableCellResolver(flatLines []string, blocks []string, sources []transcriptSource, contentWidth int) func(line, col int) (string, int, bool) {
	starts := make([]int, len(blocks))
	total := 0
	for i, b := range blocks {
		starts[i] = total
		total += transcriptBlockLineCount(b, contentWidth)
	}
	if total != len(flatLines) {
		return nil
	}
	blockLines := make([][]string, len(blocks))
	regions := make([][]tableBlockRegion, len(blocks))
	for i, b := range blocks {
		count := transcriptBlockLineCount(b, contentWidth)
		blockLines[i] = flatLines[starts[i] : starts[i]+count]
		regions[i] = tableBlockRegions(blockLines[i])
	}
	parsed := make(map[int]*tableParse)
	return func(line, col int) (string, int, bool) {
		bi := 0
		for bi < len(starts)-1 && starts[bi+1] <= line {
			bi++
		}
		if line < starts[bi] || line >= starts[bi]+len(blockLines[bi]) {
			return "", 0, false
		}
		if bi >= len(sources) || sources[bi].kind != transcriptSourceMarkdown {
			return "", 0, false
		}
		local := line - starts[bi]
		ordinal := -1
		for ri, rg := range regions[bi] {
			if local >= rg.startLine && local <= rg.endLine {
				ordinal = ri
				break
			}
		}
		if ordinal < 0 || !hasTableContent(blockLines[bi][local]) {
			return "", 0, false
		}
		p, ok := parsed[bi]
		if !ok {
			p = parseTableBlock(sources[bi].raw)
			parsed[bi] = p
		}
		row := tableRowIndex(blockLines[bi], regions[bi][ordinal], local)
		text, ok := p.cell(ordinal, row, col)
		return text, row, ok
	}
}
