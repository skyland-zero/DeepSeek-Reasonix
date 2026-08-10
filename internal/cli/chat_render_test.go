package cli

import (
	"strings"
	"testing"
	"time"

	"charm.land/bubbles/v2/textarea"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"

	"reasonix/internal/config"
	"reasonix/internal/event"
	"reasonix/internal/provider"
)

// newTestChatTUI builds a chatTUI with just the pieces the streaming/commit and
// completion paths need, for unit tests that don't run the bubbletea loop.
func newTestChatTUI() chatTUI {
	commit := []string{}
	ti := textarea.New()
	configureChatTextarea(&ti)
	ti.SetWidth(80)
	shellIdx := map[string]int{}
	shellOut := map[string]string{}
	shellExp := map[string]bool{}
	return chatTUI{
		input:                ti,
		width:                80,
		statusLineCount:      2,
		submittedInputCursor: -1,
		queueEditCursor:      -1,
		nextPasteID:          1,
		reasoningLineIdx:     -1,
		reasoningTextIdx:     -1,
		answerIdx:            -1,
		toolStreamIdx:        -1,
		reasoning:            &strings.Builder{},
		pending:              &strings.Builder{},
		pendingCommit:        &commit,
		shellOutputs:         shellOut,
		shellExpanded:        shellExp,
		shellTranscriptIdx:   shellIdx,
		toolCardIdx:          map[string]int{},
		toolLineCountByID:    map[string]int{},
	}
}

func TestCacheRateLabelKeepsTwoDecimals(t *testing.T) {
	if got := cacheRateLabel("turn hit %s", 998, 1000); got != "turn hit 99.80%" {
		t.Fatalf("cacheRateLabel = %q, want turn hit 99.80%%", got)
	}
	if got := cacheRateLabel("avg %s", 1, 3); got != "avg 33.33%" {
		t.Fatalf("cacheRateLabel = %q, want avg 33.33%%", got)
	}
	if got := cacheRateLabel("avg %s", 1, 0); got != "" {
		t.Fatalf("cacheRateLabel with zero denominator = %q, want empty", got)
	}
}

// TestIngestSeparatesReasoningFromAnswer proves the thinking marker plus its live
// text appear as reasoning streams, collapse to a "thought for Ns" summary (the
// streamed text removed) when the answer begins, and the answer streams live
// under the separator, freezing as its own distinct entry at turn end.
func TestIngestSeparatesReasoningFromAnswer(t *testing.T) {
	m := newTestChatTUI()

	m.ingestEvent(event.Event{Kind: event.Reasoning, Text: "…reasoning…"}) // thinking → marker + live text
	if len(m.transcript) != 2 || !strings.Contains(m.transcript[0], "thinking") {
		t.Fatalf("thinking marker should appear at once, transcript=%v", m.transcript)
	}
	if !strings.Contains(m.transcript[1], "…reasoning…") {
		t.Fatalf("reasoning text should stream live below the marker, transcript=%v", m.transcript)
	}

	m.ingestEvent(event.Event{Kind: event.Text, Text: "Hello answer"}) // answer begins → block collapses
	if len(m.transcript) != 3 || !strings.Contains(m.transcript[0], "thought for") {
		t.Fatalf("block should collapse to a duration summary plus answer separator, transcript=%v", m.transcript)
	}
	if strings.TrimSpace(m.transcript[1]) != "" {
		t.Fatalf("reasoning/answer separator = %q, want one blank block", m.transcript[1])
	}
	if plain := ansi.Strip(m.transcript[2]); !strings.HasPrefix(plain, "  Hello answer") {
		t.Fatalf("answer should stream live under the separator, got %q", plain)
	}
	if strings.Contains(strings.Join(m.transcript, "\n"), "…reasoning…") {
		t.Fatalf("collapsed reasoning text should be removed, transcript=%v", m.transcript)
	}
	if m.pending.String() != "Hello answer" {
		t.Errorf("answer should be live in pending, got %q", m.pending.String())
	}
	if m.reasoning.Len() != 0 {
		t.Errorf("reasoning buffer should be cleared after commit")
	}

	m.commitPending() // turn end
	if len(m.transcript) != 3 || !strings.Contains(m.transcript[2], "Hello") {
		t.Fatalf("answer should stay as its own entry, transcript=%v", m.transcript)
	}
	if plain := ansi.Strip(m.transcript[2]); !strings.HasPrefix(plain, "  Hello answer") {
		t.Fatalf("answer should have an explicit assistant identity and indented body, got %q", plain)
	}
}

// TestThinkingCompactStarMarker proves [ui] thinking_mode=compact replaces the
// live thinking text block with an animated star marker: the marker carries the
// star frame, elapsed seconds, and output tokens, and collapses to a frozen
// "✶ thought for Ns" summary without any text block.
func TestThinkingCompactStarMarker(t *testing.T) {
	m := newTestChatTUI()
	m.thinkingCompact = true

	// Pin the colour profile so the warn-yellow marker can be asserted.
	prevColor := activeColorProfile
	activeColorProfile = colorprofile.ANSI256
	defer func() { activeColorProfile = prevColor }()

	m.ingestEvent(event.Event{Kind: event.Reasoning, Text: "hidden reasoning"})
	if len(m.transcript) != 1 {
		t.Fatalf("compact thinking should open only the marker, transcript=%v", m.transcript)
	}
	plain := ansi.Strip(m.transcript[0])
	if strings.Contains(plain, "▎") {
		t.Errorf("compact marker must not carry the ▎ rule: %q", plain)
	}
	if !strings.Contains(plain, "✶") || !strings.Contains(plain, "thinking") {
		t.Errorf("compact marker should start with the first star frame + label: %q", plain)
	}
	if strings.Contains(strings.Join(m.transcript, "\n"), "hidden reasoning") {
		t.Fatalf("compact thinking must not stream the text, transcript=%v", m.transcript)
	}

	// Backdate the think start and add streamed characters, then tick: the
	// marker must pick up the elapsed seconds, the character count, and a later
	// star frame.
	m.thinkStart = time.Now().Add(-3 * time.Second)
	m.thinkChars = 1_500
	m.tickThinking()
	m.tickThinking()
	plain = ansi.Strip(m.transcript[0])
	if !strings.Contains(plain, "3.0s") || !strings.Contains(plain, "¶1500") {
		t.Errorf("compact marker should carry elapsed seconds and streamed characters: %q", plain)
	}
	if !strings.ContainsAny(plain, "✸✹✺✷") {
		t.Errorf("marker frame should advance after ticks: %q", plain)
	}
	if !strings.Contains(m.transcript[0], "\033[38;5;179m") {
		t.Errorf("live compact marker should be warn-yellow: %q", m.transcript[0])
	}

	m.ingestEvent(event.Event{Kind: event.Text, Text: "answer"}) // closes thinking
	if len(m.transcript) != 3 {
		t.Fatalf("compact thinking should collapse to summary + separator + answer, transcript=%v", m.transcript)
	}
	plain = ansi.Strip(m.transcript[0])
	if !strings.Contains(plain, "✶ thought for 3s") || !strings.Contains(plain, "¶1500") {
		t.Errorf("collapsed marker should freeze the star + duration + characters: %q", plain)
	}
	if !strings.Contains(m.transcript[0], "\033[38;5;179m") {
		t.Errorf("collapsed compact marker should keep the thinking yellow: %q", m.transcript[0])
	}
	if strings.Contains(strings.Join(m.transcript, "\n"), "hidden reasoning") {
		t.Fatalf("compact thinking text must stay hidden after commit, transcript=%v", m.transcript)
	}
}

// TestThinkingMarkerCountsStreamedChars proves the thinking marker's "¶N"
// readout tracks the live reasoning stream's character count — not Usage-event
// tokens (which providers only emit after the request finishes) — and resets
// between think rounds.
func TestThinkingMarkerCountsStreamedChars(t *testing.T) {
	m := newTestChatTUI()
	m.thinkingCompact = true

	// An unrelated Usage event (e.g. the planner model) must not leak into the
	// marker: the readout is fed by the streamed text alone.
	m.ingestEvent(event.Event{Kind: event.Usage, Usage: &provider.Usage{CompletionTokens: 2_800}})

	m.ingestEvent(event.Event{Kind: event.Reasoning, Text: "deep"})
	plain := ansi.Strip(m.transcript[0])
	if !strings.Contains(plain, "¶4") {
		t.Fatalf("marker should count streamed characters (4 runes) live: %q", plain)
	}
	if strings.Contains(plain, "↓") || strings.Contains(plain, "2.8K") {
		t.Fatalf("marker must not show Usage-event tokens: %q", plain)
	}

	m.ingestEvent(event.Event{Kind: event.Reasoning, Text: "思考细节"})
	plain = ansi.Strip(m.transcript[0])
	if !strings.Contains(plain, "¶8") { // 4 ASCII + 4 CJK runes
		t.Errorf("marker should accumulate runes across chunks: %q", plain)
	}

	m.ingestEvent(event.Event{Kind: event.Text, Text: "answer"}) // commit
	if len(m.transcript) != 3 {
		t.Fatalf("compact thinking should collapse to summary + separator + answer, transcript=%v", m.transcript)
	}
	if plain := ansi.Strip(m.transcript[0]); !strings.Contains(plain, "thought for") || !strings.Contains(plain, "¶8") {
		t.Errorf("collapsed summary should freeze the final character count: %q", ansi.Strip(m.transcript[0]))
	}

	// The next think round starts fresh: the count must not carry over.
	m.ingestEvent(event.Event{Kind: event.Reasoning, Text: "x"})
	plain = ansi.Strip(m.transcript[len(m.transcript)-1])
	if !strings.Contains(plain, "¶1") {
		t.Errorf("a new think round should reset the marker count: %q", plain)
	}

	// Expanded (non-compact) thinking: the ▎ marker carries the same readout.
	m2 := newTestChatTUI()
	m2.ingestEvent(event.Event{Kind: event.Reasoning, Text: "展开思考"})
	if len(m2.transcript) != 2 {
		t.Fatalf("expanded thinking should open marker + text block, transcript=%v", m2.transcript)
	}
	if plain := ansi.Strip(m2.transcript[0]); !strings.Contains(plain, "▎") || !strings.Contains(plain, "¶4") {
		t.Errorf("expanded marker should show the streamed character count: %q", plain)
	}
}

// TestThinkingCompactVerboseToggle proves toggling verbose mid-think in compact
// mode opens the live reasoning text block immediately (buffered content
// included) and removes it again when verbose turns off.
func TestThinkingCompactVerboseToggle(t *testing.T) {
	m := newTestChatTUI()
	m.thinkingCompact = true

	m.ingestEvent(event.Event{Kind: event.Reasoning, Text: "secret plan"})
	if len(m.transcript) != 1 {
		t.Fatalf("compact thinking should open only the marker, transcript=%v", m.transcript)
	}

	m.toggleVerboseReasoning(false) // verbose on mid-think
	if len(m.transcript) != 2 {
		t.Fatalf("verbose should open the live text block, transcript=%v", m.transcript)
	}
	if !strings.Contains(ansi.Strip(m.transcript[1]), "secret plan") {
		t.Errorf("verbose block should carry the buffered reasoning, transcript=%v", m.transcript)
	}

	m.ingestEvent(event.Event{Kind: event.Reasoning, Text: " more"})
	if !strings.Contains(ansi.Strip(m.transcript[1]), "more") {
		t.Errorf("verbose block should keep streaming, transcript=%v", m.transcript)
	}

	m.toggleVerboseReasoning(false) // verbose off mid-think
	if len(m.transcript) != 1 {
		t.Fatalf("verbose off should remove the text block, transcript=%v", m.transcript)
	}
}

func TestAssistantAnswerWithoutReasoningHasNoLeadingSpacer(t *testing.T) {
	m := newTestChatTUI()
	m.ingestEvent(event.Event{Kind: event.Text, Text: "Direct answer"})
	m.ingestEvent(event.Event{Kind: event.Message})

	if len(m.transcript) != 1 {
		t.Fatalf("direct answer should remain one compact block, got %d: %v", len(m.transcript), m.transcript)
	}
	if plain := ansi.Strip(m.transcript[0]); !strings.HasPrefix(plain, "  Direct answer") {
		t.Fatalf("direct answer block = %q", plain)
	}
}

func TestTurnReceiptLeavesOneBlankRowAfterAssistantAnswer(t *testing.T) {
	m := newTestChatTUI()
	m.cfg = &config.Config{UI: config.UIConfig{ShowTurnReceipt: true}}
	m.ingestEvent(event.Event{Kind: event.Text, Text: "Answer"})
	m.ingestEvent(event.Event{Kind: event.Message})
	m.ingestEvent(event.Event{Kind: event.Usage, Usage: &provider.Usage{
		PromptTokens: 10, CompletionTokens: 2, TotalTokens: 12,
	}})

	if len(m.transcript) != 3 {
		t.Fatalf("answer + spacer + receipt should be three blocks, got %d: %v", len(m.transcript), m.transcript)
	}
	if strings.TrimSpace(m.transcript[1]) != "" {
		t.Fatalf("answer/receipt separator = %q, want one blank block", m.transcript[1])
	}
	if !strings.Contains(ansi.Strip(m.transcript[2]), "TURN") {
		t.Fatalf("last block should be the turn receipt, got %q", m.transcript[2])
	}
}

// TestVerboseReasoningInsertsTextUnderSummary proves /verbose mode keeps the full
// thinking text, placed beneath the collapsed duration summary.
func TestVerboseReasoningInsertsTextUnderSummary(t *testing.T) {
	m := newTestChatTUI()
	m.showReasoning = true

	m.ingestEvent(event.Event{Kind: event.Reasoning, Text: "step one "})
	m.ingestEvent(event.Event{Kind: event.Reasoning, Text: "step two"})
	m.ingestEvent(event.Event{Kind: event.Text, Text: "Answer"}) // closes the block

	if len(m.transcript) != 4 {
		t.Fatalf("verbose block should be summary + text + separator + live answer, transcript=%v", m.transcript)
	}
	if !strings.Contains(m.transcript[0], "thought for") {
		t.Errorf("first line should be the duration summary, got %q", m.transcript[0])
	}
	if !strings.Contains(m.transcript[1], "step one") || !strings.Contains(m.transcript[1], "step two") {
		t.Errorf("verbose text should appear under the summary, got %q", m.transcript[1])
	}
	if strings.TrimSpace(m.transcript[2]) != "" {
		t.Errorf("verbose reasoning/answer separator = %q, want blank block", m.transcript[2])
	}
	if plain := ansi.Strip(m.transcript[3]); !strings.HasPrefix(plain, "  Answer") {
		t.Errorf("answer should stream live under the separator, got %q", plain)
	}
}

// TestIngestEventFlushesAnswer confirms an event line (e.g. a tool dispatch)
// finalizes the answer streamed before it, preserving order in scrollback.
func TestIngestEventFlushesAnswer(t *testing.T) {
	m := newTestChatTUI()
	m.ingestEvent(event.Event{Kind: event.Text, Text: "partial answer "})
	m.ingestEvent(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{Name: "read_file", Args: `{"path":"x"}`}})
	// answer, then a blank spacer, then the tool line.
	if n := len(*m.pendingCommit); n != 3 {
		t.Fatalf("answer + spacer + event line should be three commits, got %d: %v", n, *m.pendingCommit)
	}
	if !strings.Contains((*m.pendingCommit)[0], "partial answer") {
		t.Errorf("first commit should be the buffered answer, got %q", (*m.pendingCommit)[0])
	}
	if strings.TrimSpace((*m.pendingCommit)[1]) != "" {
		t.Errorf("second commit should be a blank spacer, got %q", (*m.pendingCommit)[1])
	}
	if !strings.Contains((*m.pendingCommit)[2], "~ Read x") {
		t.Errorf("third commit should be the pending tool line, got %q", (*m.pendingCommit)[2])
	}
	if m.pending.Len() != 0 {
		t.Errorf("answer buffer should be drained after the event line")
	}
}

// TestStreamAnswerRendersFullBufferInPlace proves the whole pending buffer —
// including the still-streaming tail — renders in place on every Text event
// (character-level streaming, matching opencode), and turn end freezes the
// final markdown block and resets the streaming state.
func TestStreamAnswerRendersFullBufferInPlace(t *testing.T) {
	m := newTestChatTUI()

	m.ingestEvent(event.Event{Kind: event.Text, Text: "First paragraph.\n\nSecond para "})
	if m.answerIdx < 0 {
		t.Fatalf("the first Text event should open a streamed answer block")
	}
	joined := strings.Join(m.transcript, "\n")
	if !strings.Contains(joined, "First paragraph.") {
		t.Errorf("completed paragraph should be on screen, transcript=%v", m.transcript)
	}
	if !strings.Contains(joined, "Second para") {
		t.Errorf("the still-streaming tail should also render in place, transcript=%v", m.transcript)
	}

	m.ingestEvent(event.Event{Kind: event.Text, Text: "is done now."})
	m.ingestEvent(event.Event{Kind: event.Message})
	final := strings.Join(m.transcript, "\n")
	if !strings.Contains(final, "First paragraph.") || !strings.Contains(final, "Second para is done now.") {
		t.Errorf("turn end should flush the whole answer, transcript=%v", m.transcript)
	}
	if m.pending.Len() != 0 || m.answerIdx != -1 {
		t.Errorf("answer state should reset after commit, pending=%d idx=%d", m.pending.Len(), m.answerIdx)
	}
}

// TestStreamedTableFinalizesWhenClosed proves a table followed by more output
// gets water-filled and its bottom border drawn while the answer is still
// streaming, and commitPending leaves it byte-identical (no layout jump).
func TestStreamedTableFinalizesWhenClosed(t *testing.T) {
	m := newTestChatTUI()

	m.ingestEvent(event.Event{Kind: event.Text, Text: "| a | b |\n|----|----|\n| x | y |\n\nAfter the table "})
	joined := strings.Join(m.transcript, "\n")
	if !strings.Contains(joined, "└") {
		t.Fatalf("closed table should get its bottom border while streaming:\n%s", joined)
	}
	before := tableBlock(joined)
	if before == "" {
		t.Fatalf("no table block found while streaming:\n%s", joined)
	}

	m.ingestEvent(event.Event{Kind: event.Text, Text: "keeps streaming."})
	m.commitPending()
	after := tableBlock(strings.Join(m.transcript, "\n"))
	if after != before {
		t.Errorf("table layout changed at commitPending:\n--- streamed ---\n%s\n--- committed ---\n%s", before, after)
	}
}

// TestFlushableContentKeepsTrailingLine proves that flushableContent returns
// everything up to the last \n — every complete line is flushed row by row so
// tables and code blocks render in-place. The trailing incomplete line stays
// buffered for the next event.
func TestFlushableContentKeepsTrailingLine(t *testing.T) {
	if got := flushableContent("intro line\nsecond line"); got != "intro line" {
		t.Errorf("incomplete trailing line: flushable prefix = %q, want %q", got, "intro line")
	}

	if got := flushableContent("```go\ncode\nmore\n"); got != "```go\ncode\nmore" {
		t.Errorf("last char is \\n: flushable prefix = %q, want %q", got, "```go\ncode\nmore")
	}

	if got := flushableContent("| a | b |\n|----|----|\n| v1 | v2 |\n"); got != "| a | b |\n|----|----|\n| v1 | v2 |" {
		t.Errorf("table: flushable prefix = %q, want full table minus trailing \\n", got)
	}

	if got := flushableContent("no newline at all"); got != "" {
		t.Errorf("no \\n should flush nothing, got %q", got)
	}
}

// TestTrimUnclosedMathTail proves a half-written $...$ / $$...$$ span is
// withheld from the streaming render (raw LaTeX never flashes) while prose and
// closed formulas keep streaming. Code fences, escaped dollars, and currency
// amounts must not be mistaken for unclosed math.
func TestTrimUnclosedMathTail(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "no math passes through",
			in:   "First paragraph.\n\nSecond para ",
			want: "First paragraph.\n\nSecond para ",
		},
		{
			name: "closed inline math passes through",
			in:   `Euler's identity is $e^{i\pi} + 1 = 0$, a classic.`,
			want: `Euler's identity is $e^{i\pi} + 1 = 0$, a classic.`,
		},
		{
			name: "unclosed inline math withheld to the line start",
			in:   `Euler's identity is $e^{i\`,
			want: "",
		},
		{
			name: "unclosed inline math on a later line keeps earlier lines",
			in:   "Done so far.\n\nStill open $e^{i\\",
			want: "Done so far.\n",
		},
		{
			name: "display math open with content withheld",
			in:   "Intro.\n$$\n\\sum_{n=1}^{\\infty} \\frac{1}{n^2}",
			want: "Intro.",
		},
		{
			name: "display math closed passes through",
			in:   "$$\n\\sum_{n=1}^{\\infty} \\frac{1}{n^2}\n$$\n\nThat is all.",
			want: "$$\n\\sum_{n=1}^{\\infty} \\frac{1}{n^2}\n$$\n\nThat is all.",
		},
		{
			name: "math inside a code fence is ignored",
			in:   "```\n$e^{i\\pi}$ stays raw in code\n```\n\nOpen $e^{i\\",
			want: "```\n$e^{i\\pi}$ stays raw in code\n```\n",
		},
		{
			name: "escaped dollar is not a delimiter",
			in:   "Cost is \\$5 and \\$10, ok.",
			want: "Cost is \\$5 and \\$10, ok.",
		},
		{
			name: "currency amounts are not math",
			in:   "Price $5, $10, and $20 total.",
			want: "Price $5, $10, and $20 total.",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := trimUnclosedMathTail(tt.in); got != tt.want {
				t.Errorf("trimUnclosedMathTail(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestToolProgressStreamsThenCollapses proves a running tool's output streams
// live under its card as an indented output block (no ⎿ connector), then
// collapses to a line-count summary when the result lands.
func TestToolProgressStreamsThenCollapses(t *testing.T) {
	m := newTestChatTUI()
	m.ingestEvent(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{ID: "b1", Name: "bash", Args: `{"command":"go test ./..."}`}})
	m.ingestEvent(event.Event{Kind: event.ToolProgress, Tool: event.Tool{ID: "b1", Output: "ok pkg/a\n"}})
	m.ingestEvent(event.Event{Kind: event.ToolProgress, Tool: event.Tool{ID: "b1", Output: "ok pkg/b\n"}})

	joined := strings.Join(m.transcript, "\n")
	if !strings.Contains(joined, "ok pkg/a") || !strings.Contains(joined, "ok pkg/b") {
		t.Fatalf("live output should be visible while running:\n%s", joined)
	}
	if !strings.Contains(joined, "    ok pkg/a") || strings.Contains(joined, "⎿") {
		t.Fatalf("live output should render as an indented block, not a ⎿ tree:\n%s", joined)
	}

	m.ingestEvent(event.Event{Kind: event.ToolResult, Tool: event.Tool{ID: "b1", Name: "bash", Output: "ok pkg/a\nok pkg/b\n"}})
	joined = strings.Join(m.transcript, "\n")
	// No accumulated output (non-"shell-" id), so the result's first line
	// previews the run with a remaining-line count.
	if !strings.Contains(joined, "    ok pkg/a") || !strings.Contains(joined, "1 more lines") {
		t.Fatalf("collapsed block should preview the first output line and count the rest:\n%s", joined)
	}
	if strings.Contains(joined, "2 lines") || strings.Contains(joined, "Ctrl+B") {
		t.Fatalf("collapsed block must not show a bare line count or a shortcut hint:\n%s", joined)
	}
}

// TestToolWorkingLineThenClears proves a dispatched bash that streams no output
// shows a live "working · Ns" line so it doesn't look frozen, and that the
// line clears on the result instead of collapsing to "0 lines". Bash is the
// only tool that gets this block (it's the one streaming tool); non-streaming
// tools are covered by TestNonStreamingToolCardStaysSingleLine.
func TestToolWorkingLineThenClears(t *testing.T) {
	m := newTestChatTUI()
	m.ingestEvent(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{ID: "c1", Name: "bash", Args: `{"command":"go test ./..."}`}})

	m.tickToolRunning() // one elapsed tick fills the placeholder
	joined := strings.Join(m.transcript, "\n")
	if strings.Contains(joined, "⎿") || !strings.Contains(joined, "working") {
		t.Fatalf("a running tool should show an indented 'working' progress line:\n%s", joined)
	}

	m.ingestEvent(event.Event{Kind: event.ToolResult, Tool: event.Tool{ID: "c1", Name: "bash"}})
	joined = strings.Join(m.transcript, "\n")
	if strings.Contains(joined, "working") {
		t.Fatalf("working line should clear after the result:\n%s", joined)
	}
	if strings.Contains(joined, "0 lines") {
		t.Fatalf("a no-output tool must not collapse to '0 lines':\n%s", joined)
	}
	if m.toolStreamIdx != -1 {
		t.Fatalf("tool block should be closed after the result, idx=%d", m.toolStreamIdx)
	}
}

// TestNonStreamingToolCardStaysSingleLine proves read-style tools render one
// card line: a "~ Read x" pending row while running, then the dim
// "→ Read x 123 lines" completed line with the result's line count folded
// onto it: no separate indented "N lines" row after.
func TestNonStreamingToolCardStaysSingleLine(t *testing.T) {
	m := newTestChatTUI()
	m.ingestEvent(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{ID: "r1", Name: "read_file", Args: `{"path":"x"}`}})
	m.tickToolRunning() // must be a no-op for non-streaming tools
	joined := strings.Join(m.transcript, "\n")
	if strings.Contains(joined, "⎿") || strings.Contains(joined, "working") {
		t.Fatalf("a non-streaming tool must not open a working block:\n%s", joined)
	}
	if !strings.Contains(joined, "~ Read x") {
		t.Fatalf("dispatch should render the pending line, got:\n%s", joined)
	}
	if len(m.transcript) != 1 {
		t.Fatalf("transcript should hold exactly the pending line, got %d slots:\n%s", len(m.transcript), joined)
	}

	m.ingestEvent(event.Event{Kind: event.ToolResult, Tool: event.Tool{ID: "r1", Name: "read_file", Output: "a\nb\nc\n"}})
	joined = strings.Join(m.transcript, "\n")
	if !strings.Contains(joined, "→ Read x") || !strings.Contains(joined, "3 lines") {
		t.Fatalf("card should fold the line count onto itself, got:\n%s", joined)
	}
	if strings.Contains(joined, "⎿") {
		t.Fatalf("no separate connector row should remain:\n%s", joined)
	}
	if len(m.transcript) != 1 {
		t.Fatalf("transcript should still hold one slot, got %d:\n%s", len(m.transcript), joined)
	}

	// A result with no output appends nothing to the card.
	m2 := newTestChatTUI()
	m2.ingestEvent(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{ID: "r2", Name: "read_file", Args: `{"path":"y"}`}})
	m2.ingestEvent(event.Event{Kind: event.ToolResult, Tool: event.Tool{ID: "r2", Name: "read_file"}})
	joined = strings.Join(m2.transcript, "\n")
	if strings.Contains(joined, "lines") {
		t.Fatalf("a no-output result must not append a line count:\n%s", joined)
	}
}

// TestConsecutiveToolCallsKeepMarkersUnderOwnCard is a regression test for
// back-to-back Bash tool calls. Before the fix, the late ToolProgress for
// the first tool (already superseded in the controller by a second
// ToolDispatch) appended a fresh live block at the end of the transcript
// under the *second* tool's card. Both indented markers then stacked at the
// end, hiding which run produced which output. The fix threads the
// transcript slot through shellTranscriptIdx so each tool's live block
// stays directly under its own card regardless of the dispatch/progress
// arrival order.
func TestConsecutiveToolCallsKeepMarkersUnderOwnCard(t *testing.T) {
	m := newTestChatTUI()
	// First bash: dispatched and gets one progress chunk before the second
	// bash is dispatched, mirroring the model's parallel-tool-call pattern.
	// The "shell-" prefix ensures streamToolOutput accumulates into
	// shellOutputs, which collapseShellSlot uses to recover the line count
	// after the live state has been reset by the second beginToolRunning.
	m.ingestEvent(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{ID: "shell-1", Name: "bash", Args: `{"command":"git status"}`}})
	m.ingestEvent(event.Event{Kind: event.ToolProgress, Tool: event.Tool{ID: "shell-1", Output: "On branch main-v2\n"}})
	// Second bash dispatched before the first finishes; this switches
	// m.toolStreamID to "shell-2" and resets the live streaming state.
	m.ingestEvent(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{ID: "shell-2", Name: "bash", Args: `{"command":"git branch -a"}`}})
	// The second bash also streams one chunk of output so its collapse
	// produces a real indented marker (not the zero-output blank fallback).
	m.ingestEvent(event.Event{Kind: event.ToolProgress, Tool: event.Tool{ID: "shell-2", Output: "* main-v2\n"}})
	// Late progress for the FIRST bash — the path that previously stacked
	// its marker under the second card.
	m.ingestEvent(event.Event{Kind: event.ToolProgress, Tool: event.Tool{ID: "shell-1", Output: "nothing to commit\n"}})
	// Now finish both; each should collapse in place under its own card.
	m.ingestEvent(event.Event{Kind: event.ToolResult, Tool: event.Tool{ID: "shell-1", Name: "bash", Output: "On branch main-v2\nnothing to commit\n"}})
	m.ingestEvent(event.Event{Kind: event.ToolResult, Tool: event.Tool{ID: "shell-2", Name: "bash", Output: "* main-v2\n"}})

	// Locate each tool's card. With the fix the transcript is exactly
	// [card1, marker1, "", card2, marker2] — 5 lines, one marker per
	// card. Without the fix the late progress overwrites the last slot
	// in place (or appends), so the first card's slot is left holding
	// only the first live chunk, and both markers end up at the tail.
	transcript := m.transcript
	idx1, idx2 := -1, -1
	for i, ln := range transcript {
		if idx1 == -1 && strings.Contains(ln, "git status") {
			idx1 = i
		}
		if idx2 == -1 && strings.Contains(ln, "git branch -a") {
			idx2 = i
		}
	}
	if idx1 < 0 || idx2 < 0 || idx2 <= idx1 {
		t.Fatalf("expected two bash cards in dispatch order, got idx1=%d idx2=%d\n%s", idx1, idx2, strings.Join(transcript, "\n"))
	}

	// Each card must be followed by its own indented marker slot —
	// not just "some marker somewhere after the second card".
	for _, pair := range []struct {
		card string
		idx  int
	}{
		{card: "git status", idx: idx1},
		{card: "git branch -a", idx: idx2},
	} {
		next := transcript[pair.idx+1]
		if !strings.HasPrefix(ansi.Strip(next), outputIndent) {
			t.Fatalf("%q's marker should be at transcript[%d] as an indented output block, got %q\nfull transcript:\n%s",
				pair.card, pair.idx+1, next, strings.Join(transcript, "\n"))
		}
	}

	// The first card's marker must reflect the full output of the first
	// run ("On branch main-v2" AND "nothing to commit"), not just the
	// first chunk. The bug left only the pre-late-progress chunk in
	// transcript[idx1+1], so the second line would be missing.
	marker1 := transcript[idx1+1]
	if !strings.Contains(marker1, "On branch main-v2") || !strings.Contains(marker1, "nothing to commit") {
		t.Fatalf("first card's marker should preview the full output of shell-1, got %q", marker1)
	}
}

// TestRepeatedShellCommandDoesNotAccumulateOutput is the regression test for a
// re-run of the same "!" command (e.g. !pwd three times). RunShell derives a
// stable id from the command text ("shell-pwd"), so streamToolOutput kept
// appending each run's output onto the previous run's in m.shellOutputs[id];
// beginToolRunning now clears the entry so each run starts from a clean slate.
func TestRepeatedShellCommandDoesNotAccumulateOutput(t *testing.T) {
	m := newTestChatTUI()
	const id = "shell-pwd"
	const out = "/home/user/project\n"

	for range 3 {
		m.ingestEvent(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{ID: id, Name: "bash", Args: `{"command":"pwd"}`}})
		m.ingestEvent(event.Event{Kind: event.ToolProgress, Tool: event.Tool{ID: id, Output: out}})
		m.ingestEvent(event.Event{Kind: event.ToolResult, Tool: event.Tool{ID: id, Name: "bash", Output: out}})
	}

	if got := m.shellOutputs[id]; got != out {
		t.Fatalf("a re-run must not accumulate prior output: shellOutputs[%q] = %q, want %q", id, got, out)
	}
}

// TestCollapsedShellShowsPreviewWithoutShortcutHint proves the collapsed
// shell preview keeps its remaining-line count but never advertises the
// Ctrl+B toggle — the hint is display noise the user opted out of.
func TestCollapsedShellShowsPreviewWithoutShortcutHint(t *testing.T) {
	m := newTestChatTUI()
	const id = "shell-long"
	lines := make([]string, shellPreviewLines+2)
	for i := range lines {
		lines[i] = "line"
	}
	output := strings.Join(lines, "\n") + "\n"
	m.shellOutputs[id] = output
	m.transcript = []string{""}

	m.collapseShellSlot(id, 0, output)

	got := m.transcript[0]
	if !strings.Contains(got, "more lines") {
		t.Fatalf("collapsed shell preview should keep the remaining-line count, got %q", got)
	}
	if strings.Contains(got, "Ctrl+B") {
		t.Fatalf("collapsed shell preview must not advertise Ctrl+B, got %q", got)
	}
	if strings.Contains(got, "click/") {
		t.Fatalf("collapsed shell preview must not advertise mouse click in default TUI mode, got %q", got)
	}
}

// TestConsecutiveNonShellToolsDoNotRenderNegativeLineCount is the regression
// test for the review-blocking case. The original fix to back-to-back shell
// tools records every dispatched id in shellTranscriptIdx so a late
// ToolProgress/Result can land in the correct slot. But for non-shell-
// prefixed tools (e.g. read_file) the streaming state belongs to whichever
// id is current and the accumulator (shellOutputs) is never populated, so
// the late path's "n" stayed at -1 and the final else branch rendered
// "⎿ -1 lines". The fix in collapseShellSlot guards n < 0 by clearing the
// slot — a deliberate blank-line fallback rather than a misleading
// negative count.
func TestConsecutiveNonShellToolsDoNotRenderNegativeLineCount(t *testing.T) {
	m := newTestChatTUI()
	// Two back-to-back read_file tools; the first result lands AFTER
	// the second dispatch (the model dispatched them in parallel and
	// the first one finished last). This is the path the PR reviewer
	// identified as the blocker.
	m.ingestEvent(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{ID: "read_file-1", Name: "read_file", Args: `{"path":"a.txt"}`}})
	m.ingestEvent(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{ID: "read_file-2", Name: "read_file", Args: `{"path":"b.txt"}`}})
	// Late ToolResult for the FIRST tool — this used to render "-1 lines"
	// under the first card.
	m.ingestEvent(event.Event{Kind: event.ToolResult, Tool: event.Tool{ID: "read_file-1", Name: "read_file", Output: "a.txt contents"}})
	m.ingestEvent(event.Event{Kind: event.ToolResult, Tool: event.Tool{ID: "read_file-2", Name: "read_file", Output: "b.txt contents"}})

	transcript := m.transcript
	// The "-1 lines" bug surfaced literally as that text, so assert its
	// absence first as a clear regression marker.
	if joined := strings.Join(transcript, "\n"); strings.Contains(joined, "-1 lines") {
		t.Fatalf("transcript must not contain a negative line count:\n%s", joined)
	}
	// And the more general contract: no slot under a card should claim
	// a non-positive line count either.
	for _, line := range transcript {
		if strings.Contains(line, "0 lines") || strings.Contains(line, "-1 lines") {
			t.Fatalf("non-shell tool marker should be blank, got %q\nfull transcript:\n%s",
				line, strings.Join(transcript, "\n"))
		}
	}
}

func TestTodoPanelKeepsLastSuccessfulTodoWrite(t *testing.T) {
	m := newTestChatTUI()
	initial := `{"todos":[{"content":"Sync main-v2","status":"in_progress"},{"content":"Push origin","status":"pending"}]}`
	failed := `{"todos":[{"content":"Sync main-v2","status":"completed"},{"content":"Push origin","status":"in_progress"}]}`

	m.ingestEvent(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{ID: "todo-1", Name: "todo_write", Args: initial}})
	m.ingestEvent(event.Event{Kind: event.ToolResult, Tool: event.Tool{ID: "todo-1", Name: "todo_write", Args: initial, Output: "Todos updated"}})
	if m.todoArgs != initial {
		t.Fatalf("todoArgs after successful result = %q, want initial args", m.todoArgs)
	}

	m.ingestEvent(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{ID: "todo-2", Name: "todo_write", Args: failed}})
	m.ingestEvent(event.Event{Kind: event.ToolResult, Tool: event.Tool{ID: "todo-2", Name: "todo_write", Args: failed, Err: "missing complete_step"}})
	if m.todoArgs != initial {
		t.Fatalf("failed todo_write must not replace the panel: got %q, want %q", m.todoArgs, initial)
	}
}

// TestToolProgressTailCap proves the live block only keeps the last
// toolStreamTailLines lines so a chatty build doesn't flood scrollback.
func TestToolProgressTailCap(t *testing.T) {
	m := newTestChatTUI()
	m.ingestEvent(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{ID: "b1", Name: "bash", Args: `{"command":"x"}`}})
	for i := 0; i < toolStreamTailLines+5; i++ {
		m.ingestEvent(event.Event{Kind: event.ToolProgress, Tool: event.Tool{ID: "b1", Output: "line" + string(rune('A'+i)) + "\n"}})
	}
	block := m.transcript[m.toolStreamIdx]
	if got := strings.Count(block, "\n") + 1; got > toolStreamTailLines {
		t.Fatalf("live block kept %d lines, want <= %d:\n%s", got, toolStreamTailLines, block)
	}
	if strings.Contains(block, "lineA") {
		t.Fatalf("oldest line should have scrolled out of the tail:\n%s", block)
	}
}

// TestReasoningViewBounded proves the live thinking view stays bounded under a
// long stream — the fix for the O(n²)/multi-GB re-render of the full thought.
func TestReasoningViewBounded(t *testing.T) {
	m := newTestChatTUI()
	for i := 0; i < 5000; i++ {
		m.ingestEvent(event.Event{Kind: event.Reasoning, Text: "some thinking text token "})
	}
	if len(m.reasoningView) > reasoningViewMax {
		t.Fatalf("reasoningView unbounded: %d > %d", len(m.reasoningView), reasoningViewMax)
	}
	if c := strings.Count(m.transcript[m.reasoningTextIdx], "\n") + 1; c > reasoningTailLines {
		t.Fatalf("live reasoning block kept %d lines, want <= %d", c, reasoningTailLines)
	}
}

// TestConsecutiveToolCardsSitFlush proves back-to-back tool dispatches render
// their single-line cards with no blank spacer row between them ("→ Read a"
// directly followed by "→ Read b"), while the spacer still separates the
// first card from preceding content such as an assistant answer.
func TestConsecutiveToolCardsSitFlush(t *testing.T) {
	m := newTestChatTUI()
	m.ingestEvent(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{ID: "r1", Name: "read_file", Args: `{"path":"a"}`}})
	m.ingestEvent(event.Event{Kind: event.ToolResult, Tool: event.Tool{ID: "r1", Name: "read_file", Output: "a\n"}})
	m.ingestEvent(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{ID: "r2", Name: "read_file", Args: `{"path":"b"}`}})
	m.ingestEvent(event.Event{Kind: event.ToolResult, Tool: event.Tool{ID: "r2", Name: "read_file", Output: "b\n"}})

	if len(m.transcript) != 2 {
		t.Fatalf("two flush cards should be exactly two blocks, got %d: %v", len(m.transcript), m.transcript)
	}
	if got := ansi.Strip(m.transcript[0]); !strings.Contains(got, "→ Read a") {
		t.Fatalf("first block should be the completed first card, got %q", got)
	}
	if got := ansi.Strip(m.transcript[1]); !strings.Contains(got, "→ Read b") {
		t.Fatalf("second block should be the completed second card, got %q", got)
	}

	// A preceding assistant answer still gets its blank spacer before the
	// first card — only the card-to-card gap is removed.
	m2 := newTestChatTUI()
	m2.ingestEvent(event.Event{Kind: event.Text, Text: "Answer"})
	m2.ingestEvent(event.Event{Kind: event.Message})
	m2.ingestEvent(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{ID: "r3", Name: "read_file", Args: `{"path":"c"}`}})
	m2.ingestEvent(event.Event{Kind: event.ToolResult, Tool: event.Tool{ID: "r3", Name: "read_file", Output: "c\n"}})
	if len(m2.transcript) != 3 {
		t.Fatalf("answer + spacer + card should be three blocks, got %d: %v", len(m2.transcript), m2.transcript)
	}
	if strings.TrimSpace(m2.transcript[1]) != "" {
		t.Fatalf("answer/card separator = %q, want one blank block", m2.transcript[1])
	}
	if got := ansi.Strip(m2.transcript[2]); !strings.Contains(got, "→ Read c") {
		t.Fatalf("card after the spacer should be the completed line, got %q", got)
	}
}

// TestReflowKeepsCompletedCardForm proves a terminal resize re-renders a
// completed tool card from its semantic source without reverting it to the
// running "~ pending" form — the state lives on transcriptSource, not in the
// rendered text.
func TestReflowKeepsCompletedCardForm(t *testing.T) {
	m := newTestChatTUI()
	m.ingestEvent(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{ID: "r1", Name: "read_file", Args: `{"path":"x"}`}})
	m.ingestEvent(event.Event{Kind: event.ToolResult, Tool: event.Tool{ID: "r1", Name: "read_file", Output: "a\nb\n"}})
	if got := ansi.Strip(m.transcript[0]); !strings.Contains(got, "→ Read x") {
		t.Fatalf("card should be completed before reflow, got %q", got)
	}

	m.width = 60
	m.reflowTranscript(m.width)
	if got := ansi.Strip(m.transcript[0]); !strings.Contains(got, "→ Read x") || !strings.Contains(got, "2 lines") {
		t.Fatalf("reflow must keep the completed card and its line count, got %q", got)
	}
	got := ansi.Strip(m.transcript[0])
	if strings.Contains(got, "~ Read x") {
		t.Fatalf("reflow must not revert a completed card to the pending form, got %q", got)
	}
}
