package cli

import (
	"encoding/base64"
	"fmt"
	"math"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/atotto/clipboard"
	"github.com/charmbracelet/x/ansi"

	"reasonix/internal/i18n"
	"reasonix/internal/provider"
)

type transcriptSourceKind uint8

const (
	transcriptSourceFixed transcriptSourceKind = iota
	transcriptSourceMarkdown
	transcriptSourceUser
	transcriptSourceReasoning
	transcriptSourceToolCard
	transcriptSourceBanner
	transcriptSourceReplayBundle
	transcriptSourceTurnReceipt
	transcriptSourceSubagentProgress
)

// transcriptSource retains only the semantic inputs needed to reproduce a
// width-dependent transcript block. It deliberately sits beside []string
// instead of replacing it: the rendered slice remains the fast path for every
// frame and preserves the many index-based live tool/reasoning updates.
type transcriptSource struct {
	kind      transcriptSourceKind
	raw       string
	aux       string
	id        string // tool call id, for the task card's short-id tag
	planMode  bool
	streaming bool // table bottom border omitted until commitPending finalises
	maxLines  int
	history   []provider.Message
	completed bool // tool card finished: render the dim completed form
	lineCount int  // folded "N lines" suffix for a completed non-streaming card
}

func (m *chatTUI) ensureTranscriptSources() {
	if len(m.transcriptSources) > len(m.transcript) {
		m.transcriptSources = m.transcriptSources[:len(m.transcript)]
	}
	for len(m.transcriptSources) < len(m.transcript) {
		m.transcriptSources = append(m.transcriptSources, transcriptSource{kind: transcriptSourceFixed})
	}
}

func (m *chatTUI) appendTranscriptBlock(rendered string, source transcriptSource) {
	m.ensureTranscriptSources()
	m.transcript = append(m.transcript, rendered)
	m.transcriptSources = append(m.transcriptSources, source)
}

func (m *chatTUI) setTranscriptBlock(index int, rendered string, source transcriptSource) {
	if index < 0 || index >= len(m.transcript) {
		return
	}
	m.ensureTranscriptSources()
	m.transcript[index] = rendered
	m.transcriptSources[index] = source
	// Keep the viewport wrap cache in sync: the next sync only re-wraps the
	// suffix from this block instead of the full history.
	m.invalidateWrapFrom(index)
}

func (m *chatTUI) removeTranscriptBlock(index int) {
	if index < 0 || index >= len(m.transcript) {
		return
	}
	m.ensureTranscriptSources()
	m.transcript = append(m.transcript[:index], m.transcript[index+1:]...)
	m.transcriptSources = append(m.transcriptSources[:index], m.transcriptSources[index+1:]...)
	m.invalidateWrapFrom(index)
}

func (m *chatTUI) truncateTranscriptBlocks(length int) {
	length = min(max(length, 0), len(m.transcript))
	m.ensureTranscriptSources()
	m.transcript = m.transcript[:length]
	m.transcriptSources = m.transcriptSources[:length]
}

func (m *chatTUI) renderTranscriptSource(source transcriptSource, terminalWidth int) string {
	contentWidth := transcriptContentWidth(terminalWidth, m.nativeScrollback)
	switch source.kind {
	case transcriptSourceMarkdown:
		return renderAssistantMarkdownStreaming(source.raw, contentWidth, source.streaming)
	case transcriptSourceUser:
		return renderUserBubble(source.raw, contentWidth, source.planMode)
	case transcriptSourceReasoning:
		return reasoningBlock(source.raw, contentWidth, source.maxLines)
	case transcriptSourceToolCard:
		// A completed card re-renders its dim opencode-style line (with the
		// output's line count folded on); a running one shows the "~ pending"
		// form carrying the same verb + argument body, except bash which
		// displays its "$ Bash command" line immediately.
		if source.completed {
			line := toolCardCompleted(source.raw, source.aux, source.id, contentWidth)
			if source.lineCount > 0 {
				line += " " + dim(fmt.Sprintf("%d lines", source.lineCount))
			}
			return line
		}
		if toolStreamsOutput(source.raw) {
			return toolCard(source.raw, source.aux, source.id, contentWidth)
		}
		return toolPendingLine(source.raw, source.aux, source.id, contentWidth)
	case transcriptSourceBanner:
		return strings.TrimRight(m.renderTUIBanner(m.label, source.raw, contentWidth), "\n")
	case transcriptSourceReplayBundle:
		return m.renderReplayBundle(source, contentWidth, func(raw string, width int) string {
			return renderAssistantMarkdownStreaming(raw, width, false)
		})
	case transcriptSourceTurnReceipt:
		return renderTurnReceiptBand(source.raw, contentWidth)
	case transcriptSourceSubagentProgress:
		if sp := m.subagentProgress[source.raw]; sp != nil {
			return m.subagentProgressBlock(source.raw, sp)
		}
		return ""
	default:
		return ""
	}
}

func (m chatTUI) renderReplayBundle(
	source transcriptSource,
	contentWidth int,
	renderAssistant func(string, int) string,
) string {
	var b strings.Builder
	b.WriteString(m.renderTUIBanner(m.label, source.raw, contentWidth))
	for _, section := range replaySectionsForWithAssistantRenderer(
		source.history,
		contentWidth,
		renderAssistant,
	) {
		b.WriteString(section)
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m chatTUI) renderReplayBundleCopy(
	source transcriptSource,
	contentWidth int,
	prefix string,
) string {
	assistantIndex := 0
	return m.renderReplayBundle(source, contentWidth, func(raw string, width int) string {
		messagePrefix := prefix + "-" + strconv.Itoa(assistantIndex)
		assistantIndex++
		return renderAssistantMarkdownCopy(raw, width, messagePrefix)
	})
}

const assistantTranscriptIndent = "  "

// renderAssistantMarkdown gives assistant prose the same explicit transcript
// identity that user, reasoning, tool, and receipt blocks already have. The
// body keeps a restrained two-cell gutter instead of using a heavy card, and
// rendering at the reduced width keeps every indented row inside the viewport.
// streaming=true omits the table bottom border so new rows appear in-place while
// the model is still producing output.
// renderAssistantMarkdown is the upstream-compatible two-argument form; local
// streaming call sites use renderAssistantMarkdownStreaming.
func renderAssistantMarkdown(raw string, contentWidth int) string {
	return renderAssistantMarkdownStreaming(raw, contentWidth, false)
}

// renderAssistantMarkdownStreaming mirrors renderAssistantMarkdown but keeps
// the streaming flag so live tables can omit their bottom border while the
// model is still producing output.
func renderAssistantMarkdownStreaming(raw string, contentWidth int, streaming bool) string {
	contentWidth = max(contentWidth, 1)
	indent := assistantTranscriptIndent
	if contentWidth <= visibleWidth(indent) {
		indent = ""
	}
	bodyWidth := max(contentWidth-visibleWidth(indent), 1)
	renderer := newMarkdownRenderer(bodyWidth)
	renderer.streaming = streaming
	rendered := renderer.Render(raw)
	if rendered == "" {
		rendered = raw
	}
	body := strings.TrimRight(rendered, "\n")
	if body == "" {
		return indent
	}
	return indentTranscriptBlock(body, indent)
}

// renderAssistantMarkdownCopy mirrors renderAssistantMarkdown's visible output
// and adds zero-width math markers for on-demand clipboard reconstruction.
func renderAssistantMarkdownCopy(raw string, contentWidth int, prefix string) string {
	contentWidth = max(contentWidth, 1)
	indent := assistantTranscriptIndent
	if contentWidth <= visibleWidth(indent) {
		indent = ""
	}
	bodyWidth := max(contentWidth-visibleWidth(indent), 1)
	renderer := newMarkdownRenderer(bodyWidth)
	rendered := renderer.RenderCopy(raw, prefix)
	if rendered == "" {
		rendered = raw
	}
	body := strings.TrimRight(rendered, "\n")
	if body == "" {
		return indent
	}
	return indentTranscriptBlock(body, indent)
}

func indentTranscriptBlock(block, indent string) string {
	if indent == "" || block == "" {
		return block
	}
	lines := strings.Split(block, "\n")
	for i, line := range lines {
		if line != "" {
			lines[i] = indent + line
		}
	}
	return strings.Join(lines, "\n")
}

func renderTurnReceiptBand(receipt string, contentWidth int) string {
	if strings.TrimSpace(ansi.Strip(receipt)) == "" {
		return ""
	}
	contentWidth = max(contentWidth, 1)
	indent := statusFooterIndent
	innerWidth := contentWidth - visibleWidth(indent)
	if innerWidth <= 0 {
		return ""
	}

	// Remove the indent that renderTurnReceipt prepends so we can re-layout
	body := strings.TrimPrefix(receipt, indent)
	bodyWidth := visibleWidth(ansi.Strip(body))

	// Left: 6 dashes + space
	const leftDash = "──────"
	leftWidth := visibleWidth(leftDash) + 1

	// Right: dashes filling to contentWidth
	rightWidth := innerWidth - leftWidth - bodyWidth - 1 // -1 right space
	if rightWidth < 0 {
		rightWidth = 0
	}
	rightDash := strings.Repeat("─", rightWidth)

	// Dashes share the subtle/label colour so they sit at the same visual
	// weight as the semantic prefix (e.g. "TURN" / "本轮").
	line := indent + themeFg(activeCLITheme.subtle, leftDash) + " " + body + " " + themeFg(activeCLITheme.subtle, rightDash)
	return line
}

func (m *chatTUI) reflowTranscript(terminalWidth int) {
	m.ensureTranscriptSources()
	for i, source := range m.transcriptSources {
		if source.kind == transcriptSourceFixed {
			continue
		}
		m.transcript[i] = m.renderTranscriptSource(source, terminalWidth)
	}
}

func (m *chatTUI) commitTranscriptSource(source transcriptSource) {
	rendered := m.renderTranscriptSource(source, m.width)
	*m.pendingCommit = append(*m.pendingCommit, rendered)
	m.appendTranscriptBlock(rendered, source)
}

const (
	copyMathStartPrefix = "\x1b]1337;skycode-copy-math="
	copyMathEndPrefix   = "\x1b]1337;skycode-copy-math-end="
	copyMathTerminator  = "\x07"
)

func copyMathStartMarker(id, source string) string {
	encoded := base64.RawURLEncoding.EncodeToString([]byte(source))
	return copyMathStartPrefix + id + ";" + encoded + copyMathTerminator
}

func copyMathEndMarker(id string) string {
	return copyMathEndPrefix + id + copyMathTerminator
}

// buildCopyTranscript renders semantic Markdown only when a copy is requested.
// The visible text stays byte-for-byte equivalent after ANSI stripping, while
// math markers retain the source needed to map display cells back to LaTeX.
func (m chatTUI) buildCopyTranscript(contentWidth int) (string, int, bool) {
	if len(m.transcriptSources) != len(m.transcript) {
		return "", 0, false
	}
	var b strings.Builder
	markers := 0
	for i, source := range m.transcriptSources {
		if i > 0 {
			b.WriteByte('\n')
		}
		switch source.kind {
		case transcriptSourceMarkdown:
			rendered := renderAssistantMarkdownCopy(source.raw, contentWidth, strconv.Itoa(i))
			markers += strings.Count(rendered, copyMathStartPrefix)
			b.WriteString(rendered)
		case transcriptSourceReplayBundle:
			rendered := m.renderReplayBundleCopy(source, contentWidth, strconv.Itoa(i))
			markers += strings.Count(rendered, copyMathStartPrefix)
			b.WriteString(rendered)
		default:
			b.WriteString(m.transcript[i])
		}
	}
	return b.String(), markers, true
}

// transcriptResizeAnchor identifies the transcript block at the top of the
// viewport plus the relative row within it. Reflow can change a block's line
// count, so preserving a raw Y offset would jump to unrelated content.
type transcriptResizeAnchor struct {
	block    int
	fraction float64
	valid    bool
}

func captureTranscriptResizeAnchor(blocks []string, width, yOffset int) transcriptResizeAnchor {
	if width <= 0 || len(blocks) == 0 {
		return transcriptResizeAnchor{}
	}
	remaining := max(yOffset, 0)
	for i, block := range blocks {
		lines := transcriptBlockLineCount(block, width)
		if remaining < lines {
			fraction := 0.0
			if lines > 1 {
				fraction = float64(remaining) / float64(lines-1)
			}
			return transcriptResizeAnchor{block: i, fraction: fraction, valid: true}
		}
		remaining -= lines
	}
	return transcriptResizeAnchor{block: len(blocks) - 1, fraction: 1, valid: true}
}

func (a transcriptResizeAnchor) yOffset(blocks []string, width int) int {
	if !a.valid || len(blocks) == 0 || width <= 0 {
		return 0
	}
	block := min(max(a.block, 0), len(blocks)-1)
	offset := 0
	for i := 0; i < block; i++ {
		offset += transcriptBlockLineCount(blocks[i], width)
	}
	lines := transcriptBlockLineCount(blocks[block], width)
	if lines > 1 {
		offset += int(math.Round(a.fraction * float64(lines-1)))
	}
	return offset
}

func transcriptBlockLineCount(block string, width int) int {
	return strings.Count(wrapTranscript(block, width), "\n") + 1
}

// wrapFixedContent wraps multi-line content inside a fixed-width container:
// every source line keeps its own line break and its leading whitespace, and
// only genuinely overlong lines wrap — with the leading whitespace carried
// onto each continuation line, so the container's margins never depend on the
// content. Tabs expand to the next tab stop first (matching expandTabs), and
// overlong bodies wrap through lipgloss so SGR styles stay closed at each line
// end. Lines that already fit pass through byte-for-byte, which the transcript
// copy/geometry layers rely on.
func wrapFixedContent(s string, width int) string {
	if width <= 0 {
		return s
	}
	s = expandTabs(s)
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lead := line[:len(line)-len(strings.TrimLeft(line, " "))]
		body := line[len(lead):]
		inner := width - visibleWidth(lead)
		if inner < 1 {
			inner = 1
		}
		if visibleWidth(body) <= inner {
			continue // fits: keep the source line untouched
		}
		// lipgloss pads wrapped lines to `inner` columns; drop the padding
		// before re-attaching the source line's leading whitespace, so the
		// container margin comes from the source alone.
		wrapped := lipgloss.NewStyle().Width(inner).Render(body)
		parts := strings.Split(wrapped, "\n")
		for j, p := range parts {
			parts[j] = strings.TrimRight(p, " ")
		}
		lines[i] = lead + strings.Join(parts, "\n"+lead)
	}
	return strings.Join(lines, "\n")
}

// wrapTranscript wraps the joined transcript to width for the viewport, keeping
// SGR balanced across wrap points. ansi.Hardwrap leaves a style that spans a
// break open at the line end (e.g. a wrapped dim link tail), which bleeds the
// attribute into the padding and the next row on stricter terminals (Warp).
// lipgloss closes the active style at each line end and reopens it at the next.
// Lines are then padded to exactly `width` columns (lipgloss's alignment
// behaviour), which renderTranscript and the ghost writer rely on.
func wrapTranscript(s string, width int) string {
	if width <= 0 {
		return s
	}
	wrapped := wrapFixedContent(s, width)
	lines := strings.Split(wrapped, "\n")
	for i, l := range lines {
		if pad := width - visibleWidth(l); pad > 0 {
			lines[i] = l + strings.Repeat(" ", pad)
		}
	}
	return strings.Join(lines, "\n")
}

type clipboardCopyMsg struct {
	text       string
	err        error
	osc52      bool
	statusHint bool
	seq        int
}

var writeNativeClipboardText = clipboard.WriteAll

func remoteClipboardSession() bool {
	return os.Getenv("SSH_CONNECTION") != "" || os.Getenv("SSH_CLIENT") != "" || os.Getenv("SSH_TTY") != ""
}

// copyToClipboard prefers the operating system clipboard in a local session,
// where success can be verified (pbcopy on macOS, the selected Wayland/X11
// utility on Linux, and the Win32 clipboard on Windows). SSH cannot reliably
// reach the user's local desktop clipboard, so it deliberately falls back to
// OSC 52. A failed local write also falls back, but the UI labels that path as
// an unverified terminal request rather than claiming a successful copy.
func copyToClipboard(text string) tea.Cmd {
	return copyToClipboardWithStatus(text, 0, false)
}

func copyToClipboardWithStatus(text string, seq int, statusHint bool) tea.Cmd {
	return func() tea.Msg {
		if remoteClipboardSession() {
			return clipboardCopyMsg{text: text, osc52: true, statusHint: statusHint, seq: seq}
		}
		return clipboardCopyMsg{
			text:       text,
			err:        writeNativeClipboardText(text),
			statusHint: statusHint,
			seq:        seq,
		}
	}
}

// copyNoticeTTL is how long the "copied to clipboard" status-line hint stays
// visible after a selection copy (mouse drag, right-click, or Ctrl+C) before
// copyNoticeExpireMsg clears it.
const copyNoticeTTL = 1500 * time.Millisecond

// copyNoticeExpireMsg clears the transient copy notice — but only if seq still
// matches m.copyNoticeSeq, so an older copy's timer can't stomp a newer notice
// (e.g. drag-copy immediately followed by a right-click re-copy).
type copyNoticeExpireMsg struct{ seq int }

// copySelectionWithNotice copies text to the clipboard and arms the status-line
// "copied to clipboard" hint, bumping copyNoticeSeq so any in-flight expiry tick
// from a prior copy is superseded rather than racing this one.
func (m *chatTUI) copySelectionWithNotice(text string) tea.Cmd {
	m.copyNoticeSeq++
	seq := m.copyNoticeSeq
	return copyToClipboardWithStatus(text, seq, true)
}

func copyNoticeExpire(seq int) tea.Cmd {
	return tea.Tick(copyNoticeTTL, func(time.Time) tea.Msg {
		return copyNoticeExpireMsg{seq: seq}
	})
}

// autoScrollMsg drives one step of edge-drag scrolling while a selection is held
// against the top or bottom of the transcript.
type autoScrollMsg struct{}

func autoScrollTick() tea.Cmd {
	return tea.Tick(80*time.Millisecond, func(time.Time) tea.Msg { return autoScrollMsg{} })
}

// edgeScrollDir reports the auto-scroll direction for a drag at screen row y in
// a viewport of `height` rows: -1 at the top edge, +1 at the bottom, 0 between.
func edgeScrollDir(y, height int) int {
	switch {
	case y <= 0:
		return -1
	case y >= height-1:
		return 1
	default:
		return 0
	}
}

// selPos is a caret position in the wrapped transcript: a content-line index
// (absolute, scroll-independent) and a visual column.
type selPos struct{ line, col int }

// selection is the live left-drag text selection over the transcript. anchor is
// where the drag began, head where it currently is; active gates rendering and
// copy. Coordinates are absolute content lines so scrolling never moves them.
type selection struct {
	active       bool
	anchor, head selPos
}

func (s selection) ordered() (start, end selPos) {
	if s.anchor.line > s.head.line || (s.anchor.line == s.head.line && s.anchor.col > s.head.col) {
		return s.head, s.anchor
	}
	return s.anchor, s.head
}

func (s selection) empty() bool { return s.anchor == s.head }

var (
	selStyle         = lipgloss.NewStyle().Reverse(true)
	scrollThumbStyle lipgloss.Style
	scrollTrackStyle lipgloss.Style
)

// renderTranscript draws the viewport's visible window with a scrollbar in the
// last column and the active selection reverse-highlighted. The content lines
// (m.wrappedLines) are already padded to cw by wrapTranscript, so this stays
// cheap per frame — important because a drag re-renders on every mouse move.
func (m chatTUI) renderTranscript() string {
	h := m.viewport.Height()
	if h <= 0 {
		return ""
	}
	cw := m.viewport.Width() // content width; the scrollbar occupies one more column
	lines := m.wrappedLines
	total := len(lines)
	yoff := m.viewport.YOffset()
	start, end := m.sel.ordered()
	thumbStart, thumbSize := scrollbarThumb(h, yoff, total)
	blank := strings.Repeat(" ", cw)

	rows := make([]string, h)
	bar := make([]string, h)
	var snaps map[int]tableSnap
	if m.sel.active && !m.sel.empty() {
		snaps = tableSelectionGeometry(lines, start, end)
	}
	for r := 0; r < h; r++ {
		idx := yoff + r
		line := blank // off-content rows fill to width
		if idx >= 0 && idx < total {
			line = lines[idx] // already cw-wide from wrapTranscript
		}
		if m.sel.active && !m.sel.empty() {
			if lo, hi, ok := selSpan(idx, start, end, cw); ok {
				if sn, table := snaps[idx]; table {
					// Highlight what the copy will actually take: the anchored
					// cells' interiors, so a wrapped drag never lights up the
					// rails or the neighbouring columns.
					for _, seg := range snappedSpans(sn, lo, hi, idx == start.line, idx == end.line, hasTableContent(line)) {
						line = lipgloss.StyleRanges(line, lipgloss.NewRange(seg.lo, seg.hi, selStyle))
					}
				} else {
					line = lipgloss.StyleRanges(line, lipgloss.NewRange(lo, hi, selStyle))
				}
			}
		}
		rows[r] = line
		bar[r] = scrollbarCell(r, total, h, thumbStart, thumbSize)
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, strings.Join(rows, "\n"), strings.Join(bar, "\n"))
}

// selSpan returns the [lo, hi) visual-column span of the selection on content
// line idx (false when the line is outside the selection). cw bounds the span
// so a multi-line selection highlights through the right edge.
func selSpan(idx int, start, end selPos, cw int) (lo, hi int, ok bool) {
	if idx < start.line || idx > end.line {
		return 0, 0, false
	}
	lo, hi = 0, cw
	if idx == start.line {
		lo = start.col
	}
	if idx == end.line {
		hi = end.col
	}
	if hi > cw {
		hi = cw
	}
	if lo >= hi {
		return 0, 0, false
	}
	return lo, hi, true
}

type copyMathSpan struct {
	start  int
	end    int
	id     string
	source string
}

type copyTranscriptLine struct {
	text string
	math []copyMathSpan
}

type activeCopyMath struct {
	id     string
	source string
	start  int
}

func parseCopyTranscript(wrapped string) ([]copyTranscriptLine, int, bool) {
	rawLines := strings.Split(wrapped, "\n")
	lines := make([]copyTranscriptLine, 0, len(rawLines))
	var active *activeCopyMath
	parsedMarkers := 0

	for _, raw := range rawLines {
		var clean strings.Builder
		var spans []copyMathSpan
		column := 0
		position := 0

		for position < len(raw) {
			startAt := strings.Index(raw[position:], copyMathStartPrefix)
			endAt := strings.Index(raw[position:], copyMathEndPrefix)
			if startAt >= 0 {
				startAt += position
			}
			if endAt >= 0 {
				endAt += position
			}

			markerAt := -1
			isStart := false
			switch {
			case startAt >= 0 && (endAt < 0 || startAt < endAt):
				markerAt, isStart = startAt, true
			case endAt >= 0:
				markerAt = endAt
			}
			if markerAt < 0 {
				chunk := raw[position:]
				clean.WriteString(chunk)
				column += ansi.StringWidth(chunk)
				break
			}

			chunk := raw[position:markerAt]
			clean.WriteString(chunk)
			column += ansi.StringWidth(chunk)

			prefix := copyMathEndPrefix
			if isStart {
				prefix = copyMathStartPrefix
			}
			payloadStart := markerAt + len(prefix)
			terminatorAt := strings.Index(raw[payloadStart:], copyMathTerminator)
			if terminatorAt < 0 {
				return nil, 0, false
			}
			terminatorAt += payloadStart
			payload := raw[payloadStart:terminatorAt]
			position = terminatorAt + len(copyMathTerminator)

			if isStart {
				parts := strings.SplitN(payload, ";", 2)
				if len(parts) != 2 || active != nil {
					return nil, 0, false
				}
				decoded, err := base64.RawURLEncoding.DecodeString(parts[1])
				if err != nil {
					return nil, 0, false
				}
				active = &activeCopyMath{id: parts[0], source: string(decoded), start: column}
				parsedMarkers++
				continue
			}

			if active == nil || active.id != payload {
				return nil, 0, false
			}
			spans = append(spans, copyMathSpan{
				start: active.start, end: column, id: active.id, source: active.source,
			})
			active = nil
		}

		if active != nil {
			spans = append(spans, copyMathSpan{
				start: active.start, end: column, id: active.id, source: active.source,
			})
			active.start = 0
		}
		lines = append(lines, copyTranscriptLine{text: clean.String(), math: spans})
	}
	if active != nil {
		return nil, 0, false
	}
	return lines, parsedMarkers, true
}

func (m chatTUI) copyTranscriptLines() ([]copyTranscriptLine, bool) {
	contentWidth := m.viewport.Width()
	marked, expectedMarkers, ok := m.buildCopyTranscript(contentWidth)
	if !ok {
		return nil, false
	}
	lines, parsedMarkers, ok := parseCopyTranscript(wrapTranscript(marked, contentWidth))
	if !ok || parsedMarkers != expectedMarkers || len(lines) != len(m.wrappedLines) {
		return nil, false
	}
	for i := range lines {
		if ansi.Strip(lines[i].text) != ansi.Strip(m.wrappedLines[i]) {
			return nil, false
		}
	}
	return lines, true
}

func selectedDisplayText(lines []string, start, end selPos) string {
	snaps := tableSelectionGeometry(lines, start, end)
	var out []string
	for idx := start.line; idx <= end.line && idx < len(lines); idx++ {
		lo, hi := 0, ansi.StringWidth(lines[idx])
		if idx == start.line {
			lo = start.col
		}
		if idx == end.line {
			hi = end.col
		}
		if sn, ok := snaps[idx]; ok {
			var parts []string
			for _, seg := range snappedSpans(sn, lo, hi, idx == start.line, idx == end.line, hasTableContent(lines[idx])) {
				if s := strings.TrimSpace(ansi.Strip(ansi.Cut(lines[idx], seg.lo, seg.hi))); s != "" {
					parts = append(parts, s)
				}
			}
			if len(parts) > 0 {
				out = append(out, strings.Join(parts, "\t"))
			}
			continue
		}
		out = append(out, strings.TrimRight(ansi.Strip(ansi.Cut(lines[idx], lo, hi)), " "))
	}
	return strings.Join(out, "\n")
}

// selectedCopyText is the plain text of a display-cell selection. Table
// regions snap the rectangular window to cell boundaries (opencode-style):
// each visual line keeps the anchored cell range, so a drag inside one cell
// across its wrapped continuation lines copies that cell alone — never the
// rails or the neighbouring columns that happen to share the same visual
// columns on a wrapped line. Cells on one line join with tabs, lines with
// newlines.
//
// When the selection fully covers a single cell, resolveCell reconstructs
// the cell's original source text (single line, no renderer wrap breaks);
// rows of the same selection join with "\n".
func selectedCopyText(lines []copyTranscriptLine, start, end selPos, resolveCell func(line, col int) (string, int, bool)) string {
	seen := make(map[string]bool)
	texts := make([]string, len(lines))
	for i := range lines {
		texts[i] = lines[i].text
	}
	snaps := tableSelectionGeometry(texts, start, end)
	whole := resolveCell != nil && wholeCellSelection(snaps, start, end, texts)
	lastRow := -1
	var out []string
	for idx := start.line; idx <= end.line && idx < len(lines); idx++ {
		line := lines[idx]
		lo, hi := 0, ansi.StringWidth(line.text)
		if idx == start.line {
			lo = start.col
		}
		if idx == end.line {
			hi = end.col
		}

		if whole {
			if sn, ok := snaps[idx]; ok {
				if text, row, ok2 := resolveCell(idx, sn.c0); ok2 {
					if row != lastRow {
						lastRow = row
						out = append(out, text)
					}
					continue
				}
			}
			// Unresolvable line (malformed source, non-markdown block): fall
			// through to the geometric extraction for this line.
		}

		if sn, ok := snaps[idx]; ok {
			segs := snappedSpans(sn, lo, hi, idx == start.line, idx == end.line, hasTableContent(line.text))
			var parts []string
			for _, seg := range segs {
				var selected strings.Builder
				appendSelectedText(&selected, line, seg.lo, seg.hi, seen)
				if s := strings.TrimSpace(selected.String()); s != "" {
					parts = append(parts, s)
				}
			}
			if len(parts) > 0 {
				out = append(out, strings.Join(parts, "\t"))
			}
			continue
		}

		var selected strings.Builder
		touchedMath := appendSelectedText(&selected, line, lo, hi, seen)
		if selected.Len() == 0 && touchedMath {
			continue
		}
		out = append(out, strings.TrimRight(selected.String(), " "))
	}
	return strings.Join(out, "\n")
}

// wholeCellSelection reports whether the selection is confined to one table
// cell, fully covering its interior — the case where the copy can be rebuilt
// from the cell's original source text instead of the wrapped visual lines.
func wholeCellSelection(snaps map[int]tableSnap, start, end selPos, texts []string) bool {
	sn, ok := snaps[start.line]
	if !ok || !sn.anchored || sn.c0 != sn.c1 {
		return false
	}
	cl, ch, ok := cellInterior(sn.rails, sn.c0)
	if !ok || start.col > cl || end.col < ch {
		return false
	}
	for idx := start.line; idx <= end.line && idx < len(texts); idx++ {
		if s, ok := snaps[idx]; !ok || !s.anchored {
			return false
		}
	}
	return true
}

// appendSelectedText writes the plain text of line's [lo, hi) display-column
// span, substituting math span sources on first use (their visible glyphs are
// zero-width markers in line.text). Reports whether any math span was touched.
func appendSelectedText(b *strings.Builder, line copyTranscriptLine, lo, hi int, seen map[string]bool) bool {
	touchedMath := false
	cursor := lo
	for _, span := range line.math {
		if span.end <= lo || span.start >= hi {
			continue
		}
		touchedMath = true
		if span.start > cursor {
			b.WriteString(ansi.Strip(ansi.Cut(line.text, cursor, min(span.start, hi))))
		}
		if !seen[span.id] {
			b.WriteString(span.source)
			seen[span.id] = true
		}
		cursor = max(cursor, min(span.end, hi))
	}
	if cursor < hi {
		b.WriteString(ansi.Strip(ansi.Cut(line.text, cursor, hi)))
	}
	return touchedMath
}

// selectedText is the plain text of the active display-cell selection. Math is
// reconstructed on demand from semantic transcript sources; if the marked copy
// rendition ever diverges from the visible transcript, the safe fallback keeps
// the exact displayed text rather than applying mismatched coordinates.
func (m chatTUI) selectedText() string {
	if !m.sel.active || m.sel.empty() {
		return ""
	}
	start, end := m.sel.ordered()
	if lines, ok := m.copyTranscriptLines(); ok {
		texts := make([]string, len(lines))
		for i := range lines {
			texts[i] = lines[i].text
		}
		resolveCell := buildTableCellResolver(texts, m.transcript, m.transcriptSources, m.viewport.Width())
		return selectedCopyText(lines, start, end, resolveCell)
	}
	return selectedDisplayText(m.wrappedLines, start, end)
}

// scrollbarThumb returns the thumb's [start, start+size) row span for a viewport
// of `height` rows showing `total` content lines scrolled to `yoff`.
func scrollbarThumb(height, yoff, total int) (start, size int) {
	if total <= height {
		return 0, 0 // no overflow → no thumb
	}
	size = height * height / total
	if size < 1 {
		size = 1
	}
	maxYoff := total - height
	start = yoff * (height - size) / maxYoff
	if start > height-size {
		start = height - size
	}
	return start, size
}

func scrollbarYOffset(height, row, total, grabOffset int) int {
	if total <= height {
		return 0
	}
	_, thumbSize := scrollbarThumb(height, 0, total)
	maxTop := height - thumbSize
	if maxTop <= 0 {
		return 0
	}
	top := row - grabOffset
	if top < 0 {
		top = 0
	}
	if top > maxTop {
		top = maxTop
	}
	maxYoff := total - height
	return (top*maxYoff + maxTop/2) / maxTop
}

func scrollbarCell(row, total, height, thumbStart, thumbSize int) string {
	if total <= height {
		return " "
	}
	if row >= thumbStart && row < thumbStart+thumbSize {
		return scrollThumbStyle.Render("▓")
	}
	return " "
}

func (m chatTUI) inScrollbar(x, y int) bool {
	if m.nativeScrollback {
		return false
	}
	h := m.viewport.Height()
	return h > 0 && y >= 0 && y < h && x == m.viewport.Width() && len(m.wrappedLines) > h
}

func (m chatTUI) scrollbarGrabRowOffset(row int) int {
	thumbStart, thumbSize := scrollbarThumb(m.viewport.Height(), m.viewport.YOffset(), len(m.wrappedLines))
	if row >= thumbStart && row < thumbStart+thumbSize {
		return row - thumbStart
	}
	return thumbSize / 2
}

func (m *chatTUI) dragScrollbar(row int) {
	m.viewport.SetYOffset(scrollbarYOffset(m.viewport.Height(), row, len(m.wrappedLines), m.scrollbarGrabOffset))
	// Sync immediately so a streaming event between drag motions cannot see a
	// stale followTail and yank the reader back to the bottom (#6430/#6978).
	m.syncScrollModeAfterGesture()
}

// transcriptCaret maps a screen cell (x, y) in the transcript region to an
// absolute content position, clamping to the visible window.
func (m chatTUI) transcriptCaret(x, y int) selPos {
	h := m.viewport.Height()
	if y < 0 {
		y = 0
	}
	if y > h-1 {
		y = h - 1
	}
	if x < 0 {
		x = 0
	}
	if cw := m.viewport.Width(); x > cw {
		x = cw
	}
	return selPos{line: m.viewport.YOffset() + y, col: x}
}

// moreLinesHintRE matches the folded "… N more lines" summary suffix so the
// transcript can treat it as a non-prose boundary (e.g. for math-span
// withholding and table copy).
var moreLinesHintRE = regexp.MustCompile(`… \d+ more lines\s*$`)

// flushableContent returns the longest prefix of buf that ends on a complete
// line (has a trailing \n). The last incomplete line stays buffered. This is
// deliberately more aggressive than flushableMarkdownPrefix — a table row, list
// item, or code line is flushed as soon as it's complete so goldmark can parse
// and render it in place, even before a blank-line paragraph boundary.
func flushableContent(buf string) string {
	idx := strings.LastIndex(buf, "\n")
	if idx < 0 {
		return ""
	}
	return buf[:idx]
}

// trimUnclosedMathTail returns the longest prefix of buf that holds no unclosed
// $...$ or $$...$$ math span (ignoring code fences). A model streaming LaTeX
// can pause mid-formula; rendering the half-written span would flash raw
// LaTeX (e.g. "\frac") until the closing delimiter arrives. Ordinary prose —
// and closed formulas, which already render as Unicode — keeps the
// character-level streaming behaviour.
func trimUnclosedMathTail(buf string) string {
	lines := strings.Split(buf, "\n")
	fenced := false
	displayStart := -1 // first line of an unclosed $$…$$ span
	cut := len(lines)
	for i, ln := range lines {
		trimmed := strings.TrimSpace(ln)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			fenced = !fenced
			continue
		}
		if fenced {
			continue
		}
		if displayStart >= 0 {
			if strings.Count(ln, "$$")%2 == 1 {
				displayStart = -1 // closed
			}
			continue
		}
		if strings.Count(ln, "$$")%2 == 1 {
			displayStart = i
			continue
		}
		if mathDollarCount(ln)%2 == 1 {
			cut = i
			break
		}
	}
	if displayStart >= 0 {
		cut = displayStart
	}
	if cut >= len(lines) {
		return buf
	}
	return strings.Join(lines[:cut], "\n")
}

// mathDollarCount counts unescaped $ markers in a line that can open or close
// inline math (mirroring the mathParser's trigger): a backslash before the
// marker escapes it, and a marker followed by a digit or space is prose —
// currency amounts like "$5 and $10" must never be mistaken for an unclosed
// formula (that would withhold the whole line forever).
func mathDollarCount(ln string) int {
	n := 0
	for i := 0; i < len(ln); i++ {
		if ln[i] != '$' {
			continue
		}
		if i > 0 && ln[i-1] == '\\' {
			continue
		}
		if i+1 < len(ln) && (ln[i+1] == ' ' || ln[i+1] >= '0' && ln[i+1] <= '9') {
			continue
		}
		n++
	}
	return n
}

// reasoningBlock renders raw thinking text as dim, width-wrapped lines in a
// plain indented block (outputBlock) under the "▎ thinking…" marker above it.
// A positive maxLines keeps only the trailing visual lines (the live view); 0
// renders all (verbose collapse).
func reasoningBlock(raw string, width, maxLines int) string {
	w := width - len([]rune(outputIndent))
	if w < 8 {
		w = 8
	}
	var lines []string
	for _, wl := range strings.Split(wrapFixedContent(strings.TrimRight(raw, "\n"), w), "\n") {
		lines = append(lines, dim(wl))
	}
	if maxLines > 0 && len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
	}
	return outputBlock(lines)
}

// renderUserBubble renders the just-submitted prompt as a transcript line. Keep
// it visually lighter than the real bottom composer so a fresh session does not
// look like it has a second input box in the transcript.
func renderUserBubble(line string, width int, planMode bool) string {
	line = displayLineForImageRefs(line)
	prefix := "› "
	if planMode {
		prefix = "› [plan] "
	}
	pad := lipgloss.Width(prefix)

	textWidth := width - pad
	if !colorOn() {
		textWidth = width - pad - 2 // account for left border
	}
	if textWidth < 10 {
		textWidth = 10
	}

	wrapped := wrapFixedContent(line, textWidth)
	lines := strings.Split(wrapped, "\n")
	for i, l := range lines {
		if i == 0 {
			lines[i] = prefix + l
		} else {
			lines[i] = strings.Repeat(" ", pad) + l
		}
	}

	text := strings.Join(lines, "\n")
	if !colorOn() {
		return lipgloss.NewStyle().
			Border(lipgloss.NormalBorder(), false, false, false, true).
			PaddingLeft(1).
			Width(width).
			Render(text)
	}

	return accent(text)
}

// beginToolRunning opens an empty live block under a just-dispatched tool card,
// but only for tools that stream output (bash): non-streaming tools keep a
// single-line card, and their ToolResult folds the output's line count onto
// the card line instead. The live block is keyed by the call id;
// tickToolRunning fills it with a "working · Ns" line each second; if the tool
// later streams output, streamToolOutput reuses the same block;
// collapseToolOutput closes it on the result.
func (m *chatTUI) beginToolRunning(id, name string) {
	if id == "" {
		return
	}
	m.toolStreamID = id
	m.toolTail = m.toolTail[:0]
	m.toolPartial = ""
	m.toolLineCount = 0
	// Clear accumulated output for this tool ID so a re-run (e.g. repeated
	// !pwd with the same "shell-pwd" id) doesn't append to old output.
	delete(m.shellOutputs, id)
	m.toolStreamStart = time.Now()
	m.toolStreamFrame = 0
	if m.nativeScrollback || !toolStreamsOutput(name) {
		m.toolStreamIdx = -1
		return
	}
	m.toolStreamIdx = len(m.transcript)
	m.commitLine(outputBlock([]string{dim(fmt.Sprintf(i18n.M.ChatToolWorkingFmt, toolWorkingFrames[0], formatElapsed(0)))}))
	// Remember the transcript slot for this id so a late ToolProgress for a
	// previously dispatched (and possibly already collapsed) tool can reuse
	// it instead of appending a fresh slot at the end of the transcript. For
	// back-to-back tool calls this keeps each tool's live block directly
	// under its own card.
	m.shellTranscriptIdx[id] = m.toolStreamIdx
}

func formatElapsed(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return fmt.Sprintf("%.1fs", d.Seconds())
}

// streamAnswer renders the entire pending buffer in-place on every event so
// tables, lists, and code blocks appear word by word as the model streams.
// goldmark tolerates incomplete trailing lines (only a partially-written table
// cell renders as-is), so the full buffer is used directly — except that a
// half-written $...$ / $$...$$ math span is withheld (trimUnclosedMathTail)
// so raw LaTeX never flashes before the closing delimiter arrives. The table
// bottom-border is withheld while streaming and drawn by commitPending.
func (m *chatTUI) streamAnswer() {
	if m.nativeScrollback {
		return
	}
	prefix := trimUnclosedMathTail(m.pending.String())
	if len(prefix) <= m.answerFlushed {
		return
	}
	source := transcriptSource{kind: transcriptSourceMarkdown, raw: prefix, streaming: true}
	m.answerFlushed = len(prefix)
	if m.answerIdx < 0 {
		m.answerIdx = len(m.transcript)
		m.commitTranscriptSource(source)
	} else {
		block := m.renderTranscriptSource(source, m.width)
		m.setTranscriptBlock(m.answerIdx, block, source)
	}
	m.transcriptDirty = true
}

// collapseToolOutput replaces a finished tool's live block with a dim
// indented "N lines" summary, so the scrollback keeps a marker of the run without the
// full output (which the model already received). For shell commands ("shell-"
// prefix), it shows the first shellPreviewLines with a remaining-line count.
// No-op when id isn't streaming. resultOutput (the ToolResult's final output)
// is the last-resort line-count source when the live state was already reset.
func (m *chatTUI) collapseToolOutput(id, resultOutput string) {
	if m.nativeScrollback {
		if id == "" || m.toolStreamID != id {
			return
		}
		n := m.toolLineCount
		if m.toolPartial != "" {
			n++
		}
		if n > 0 {
			if full, ok := m.shellOutputs[id]; ok {
				lines := strings.Split(strings.TrimRight(full, "\n"), "\n")
				total := len(lines)
				if total > shellPreviewLines {
					preview := make([]string, shellPreviewLines+1)
					for i := 0; i < shellPreviewLines; i++ {
						preview[i] = dim(clampPlain(lines[i], m.width-len([]rune(outputIndent))))
					}
					preview[shellPreviewLines] = dim(fmt.Sprintf("… %d more lines", total-shellPreviewLines))
					m.commitLine(outputBlock(preview))
				} else {
					rendered := make([]string, total)
					for i, ln := range lines {
						rendered[i] = dim(clampPlain(ln, m.width-len([]rune(outputIndent))))
					}
					m.commitLine(outputBlock(rendered))
				}
				m.shellTranscriptIdx[id] = len(m.transcript) - 1
			} else {
				m.commitLine(outputBlock([]string{dim(fmt.Sprintf("%d lines", n))}))
			}
		}
		m.toolStreamIdx = -1
		m.toolStreamID = ""
		m.toolTail = m.toolTail[:0]
		m.toolPartial = ""
		m.toolLineCount = 0
		return
	}
	if m.toolStreamIdx < 0 || id == "" || m.toolStreamID != id {
		// Slot no longer active (another tool took over, or this id never
		// streamed). If beginToolRunning recorded a transcript index, collapse
		// in place so a late ToolResult doesn't leave raw streamed text behind.
		if idx, ok := m.shellTranscriptIdx[id]; ok && idx >= 0 && idx < len(m.transcript) {
			m.collapseShellSlot(id, idx, resultOutput)
		}
		return
	}
	m.collapseShellSlot(id, m.toolStreamIdx, resultOutput)
	m.toolStreamIdx = -1
	m.toolStreamID = ""
	m.toolTail = m.toolTail[:0]
	m.toolPartial = ""
	m.toolLineCount = 0
}

// collapseShellSlot finalises a tool's live block at idx. Used both by the
// active-tool path (idx == toolStreamIdx, streaming state intact) and the
// late-result path (idx recorded in shellTranscriptIdx at dispatch). Line-count
// sources, in order: live streaming state, shellOutputs ("shell-" ids only),
// the per-id count stashed by streamToolOutput, then the ToolResult's output.
func (m *chatTUI) collapseShellSlot(id string, idx int, resultOutput string) {
	m.transcriptDirty = true
	n := -1
	if id == m.toolStreamID {
		// Prefer the larger of the live count and resultOutput: resultOutput
		// is the authoritative end-state, the live state may lag behind it.
		n = m.toolLineCount
		if m.toolPartial != "" {
			n++
		}
		if resultOutput != "" {
			fromResult := len(strings.Split(strings.TrimRight(resultOutput, "\n"), "\n"))
			if fromResult > n {
				n = fromResult
			}
		}
	}
	if n < 0 {
		if full, ok := m.shellOutputs[id]; ok {
			n = len(strings.Split(strings.TrimRight(full, "\n"), "\n"))
		} else if c, ok := m.toolLineCountByID[id]; ok {
			n = c
		} else if resultOutput != "" {
			n = len(strings.Split(strings.TrimRight(resultOutput, "\n"), "\n"))
		}
	}
	if n < 0 {
		// Nothing applies (e.g. a late result for a non-"shell-" id that never
		// streamed): treat as zero rather than fabricate a "-1 lines" count.
		n = 0
	}
	if n == 0 {
		// Tool finished with no output: clear the "working…" placeholder but
		// keep the slot (shellTranscriptIdx still points here for late progress).
		m.rewriteTranscriptBlock(idx, "")
		return
	}
	if full, ok := m.shellOutputs[id]; ok {
		// Shell command: show first N lines + remaining-line count.
		lines := strings.Split(strings.TrimRight(full, "\n"), "\n")
		total := len(lines)
		if total > shellPreviewLines {
			preview := make([]string, shellPreviewLines+1)
			for i := 0; i < shellPreviewLines; i++ {
				preview[i] = dim(clampPlain(lines[i], m.width-len([]rune(outputIndent))))
			}
			preview[shellPreviewLines] = dim(fmt.Sprintf("… %d more lines", total-shellPreviewLines))
			m.rewriteTranscriptBlock(idx, outputBlock(preview))
		} else {
			rendered := make([]string, total)
			for i, ln := range lines {
				rendered[i] = dim(clampPlain(ln, m.width-len([]rune(outputIndent))))
			}
			m.rewriteTranscriptBlock(idx, outputBlock(rendered))
		}
	} else {
		// No accumulated output (model-invoked bash, "call_<n>" ids): preview
		// the result's first line instead of a bare "N lines" count, which
		// said nothing about what the command actually did.
		if preview := shellFirstLinePreview(strings.Split(strings.TrimRight(resultOutput, "\n"), "\n"), m.width); len(preview) > 0 {
			m.rewriteTranscriptBlock(idx, outputBlock(preview))
		} else {
			m.rewriteTranscriptBlock(idx, outputBlock([]string{dim(fmt.Sprintf("%d lines", n))}))
		}
	}
	m.shellTranscriptIdx[id] = idx
}

// shellFirstLinePreview renders the collapsed summary for a bash run that had
// no accumulated output: the first non-empty output line plus "… N more
// lines", so the user sees what the command did instead of a bare line count.
// Returns nil when there is nothing to show.
func shellFirstLinePreview(lines []string, width int) []string {
	first := ""
	rest := 0
	for _, ln := range lines {
		if strings.TrimSpace(ln) == "" {
			continue
		}
		if first == "" {
			first = ln
		} else {
			rest++
		}
	}
	if first == "" {
		return nil
	}
	out := []string{dim(clampPlain(first, width-len([]rune(outputIndent))))}
	if rest > 0 {
		out = append(out, dim(fmt.Sprintf("… %d more lines", rest)))
	}
	return out
}
