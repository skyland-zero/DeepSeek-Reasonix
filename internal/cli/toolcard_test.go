package cli

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"reasonix/internal/event"
)

func TestToolCard(t *testing.T) {
	cases := []struct {
		name string
		args string
		want []string
		deny []string
	}{
		{"bash", `{"command":"npm test"}`, []string{"$", "Bash", "npm test"}, nil},
		{"read_file", `{"path":"pkg/a.go"}`, []string{"→", "Read", "pkg/a.go"}, nil},
		{"read_file", `{"path":"pkg/a.go","offset":10,"limit":120}`, []string{"→", "Read", "pkg/a.go", "[limit=120, offset=10]"}, nil},
		{"grep", `{"pattern":"TODO","path":"."}`, []string{"✱", "Search", "TODO", "[path=.]"}, nil},
		{"wait", `{"job_ids":["bash-1","bash-2"],"timeout_seconds":300}`, []string{"⚙", "Wait", "bash-1", "bash-2", "[timeout_seconds=300]"}, nil},
		{"web_fetch", `{"url":"https://x.dev"}`, []string{"%", "Fetch", "https://x.dev"}, nil},
		{"write_file", `{"path":"a.go","content":"line1\nline2"}`, []string{"←", "Write", "a.go"}, []string{"content", "line1"}},
		{"use_capability", `{"action":"call","capability_id":"mcp-tool:github/search_issues","arguments":{"query":"bug"}}`, []string{"⚙", "MCP", "mcp-tool:github/search_issues"}, []string{`"arguments"`, `"query"`, "bug", "action"}},
		{"use_capability", `{"action":"list"}`, []string{"⚙", "MCP", "list"}, []string{"action"}},
	}
	for _, c := range cases {
		got := toolCard(c.name, c.args, "", 120)
		for _, w := range c.want {
			if !strings.Contains(got, w) {
				t.Errorf("%s: %q missing %q", c.name, got, w)
			}
		}
		for _, d := range c.deny {
			if strings.Contains(got, d) {
				t.Errorf("%s: %q should not contain raw arg %q", c.name, got, d)
			}
		}
	}
}

func TestToolCardUnknownFallsBackToName(t *testing.T) {
	if got := toolCard("frobnicate", `{}`, "", 80); !strings.Contains(got, "frobnicate") || !strings.Contains(got, "⚙") {
		t.Errorf("unknown tool should show its raw name behind the generic gear, got %q", got)
	}
}

// TestToolCardStates locks in the opencode-style three-state rendering: the
// running card shows "~ pending" in the category colour, the completed card
// dims the full "→ Verb arg [k=v]" line, and the failure card keeps the
// semantic icon in the error red.
func TestToolCardStates(t *testing.T) {
	if got := toolPendingLine("read_file", `{"path":"pkg/a.go","limit":120}`, "", 80); !strings.Contains(ansi.Strip(got), "~ Read pkg/a.go [limit=120]") {
		t.Errorf("pending line = %q, want the running form with the card body", got)
	}
	if got := toolPendingLine("frobnicate", `{}`, "", 80); !strings.Contains(ansi.Strip(got), "~ frobnicate") {
		t.Errorf("unknown tool pending line = %q, want the bare name fallback", got)
	}
	if got := toolCardCompleted("read_file", `{"path":"pkg/a.go","limit":120}`, "", 80); !strings.Contains(ansi.Strip(got), "→ Read pkg/a.go [limit=120]") {
		t.Errorf("completed card = %q", got)
	}
	if got := toolCardFailed("read_file", "permission denied", 80); !strings.Contains(ansi.Strip(got), "→ Read ⊘ permission denied") {
		t.Errorf("failed card = %q", got)
	}
	// task switches its icon on completion (│ → ✓).
	if got := toolCard("task", `{"description":"run tests"}`, "", 80); !strings.Contains(ansi.Strip(got), "│ Task run tests") {
		t.Errorf("running task card = %q", got)
	}
	if got := toolCardCompleted("task", `{"description":"run tests"}`, "", 80); !strings.Contains(ansi.Strip(got), "✓ Task run tests") {
		t.Errorf("completed task card = %q", got)
	}
	// Parallel tasks tag their call id so subagents stay distinguishable;
	// the tag shows on both the running and the completed form.
	if got := toolCard("task", `{"description":"run tests"}`, "call_2", 80); !strings.Contains(ansi.Strip(got), "│ Task run tests [call_2]") {
		t.Errorf("running task card with id = %q", got)
	}
	if got := toolCardCompleted("task", `{"description":"run tests"}`, "call_2", 80); !strings.Contains(ansi.Strip(got), "✓ Task run tests [call_2]") {
		t.Errorf("completed task card with id = %q", got)
	}
	// Non-task tools never show the tag.
	if got := toolCard("read_file", `{"path":"x"}`, "call_2", 80); strings.Contains(ansi.Strip(got), "call_2") {
		t.Errorf("read card must not show the call id, got %q", got)
	}
	// Over-long ids truncate to their trailing 12 runes.
	if got := toolCard("task", `{"description":"run tests"}`, "sa_20260801_1234567890abcdef", 80); !strings.Contains(ansi.Strip(got), "[567890abcdef]") || strings.Contains(ansi.Strip(got), "sa_20260801_1234567890abcdef]") {
		t.Errorf("long task id should truncate to its tail, got %q", got)
	}
	// A long pending body wraps instead of clamping: every line stays inside
	// width and continuation lines hang at outputIndent (the body indent).
	got := toolPendingLine("task", `{"description":"`+strings.Repeat("x", 200)+`"}`, "", 40)
	lines := strings.Split(ansi.Strip(got), "\n")
	if len(lines) < 2 {
		t.Fatalf("long pending body should wrap, got %q", got)
	}
	for i, l := range lines {
		if ansi.StringWidth(l) > 40 {
			t.Errorf("pending line %d overflows width: %d > 40: %q", i, ansi.StringWidth(l), l)
		}
		if i > 0 && !strings.HasPrefix(l, "    ") {
			t.Errorf("continuation line %d lost the hanging indent: %q", i, l)
		}
	}
}

// TestHostToolFailureCardHidesHostError keeps the failure card for the
// host-evidence tools down to just the red "● Verb" marker: their rejection
// text is host-side evidence/task-list noise (a complete_step refusal lists
// every command that ran this session), which is meaningless to the user.
// Other tools keep showing "⊘ reason". complete_step itself renders nothing
// at all — no dispatch card, so no failure marker either.
func TestHostToolFailureCardHidesHostError(t *testing.T) {
	longErr := "no matching successful receipt — cite the command exactly as it ran in the session; commands that ran: [\"a\" \"b\" \"c\"] — pick the matching one and retry complete_step; todo 1 remains in_progress"
	for _, tc := range []struct {
		name string
		ev   event.Event
	}{
		{"todo_write", event.Event{Kind: event.ToolResult, Tool: event.Tool{Name: "todo_write", Err: "todo_write blocked: latest successful todo_write still has incomplete items"}}},
	} {
		m := newTestChatTUI()
		m.ingestEvent(tc.ev)
		got := *m.pendingCommit
		if len(got) != 1 {
			t.Fatalf("%s: committed=%v, want a single line", tc.name, got)
		}
		if strings.Contains(got[0], "⊘") {
			t.Errorf("%s: card must not show the error detail, got %q", tc.name, got[0])
		}
		if !strings.Contains(got[0], "⚙") {
			t.Errorf("%s: card should keep the semantic failure icon, got %q", tc.name, got[0])
		}
	}

	// complete_step is fully silent: dispatch renders no card, and a failure
	// renders no marker either.
	m := newTestChatTUI()
	m.ingestEvent(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{ID: "s1", Name: "complete_step", Args: `{"summary":"run go test"}`}})
	m.ingestEvent(event.Event{Kind: event.ToolResult, Tool: event.Tool{ID: "s1", Name: "complete_step", Err: longErr}})
	if got := *m.pendingCommit; len(got) != 0 {
		t.Errorf("complete_step must not render any TUI output, committed=%v", got)
	}
	if joined := strings.Join(m.transcript, "\n"); strings.Contains(joined, "Step") {
		t.Errorf("complete_step must not leave a card in the transcript, got:\n%s", joined)
	}

	// Other tools keep the full "⊘ reason" detail.
	m = newTestChatTUI()
	m.ingestEvent(event.Event{Kind: event.ToolResult, Tool: event.Tool{Name: "bash", Err: "blocked by permission policy"}})
	got := *m.pendingCommit
	if len(got) != 1 || !strings.Contains(got[0], "⊘ blocked by permission policy") {
		t.Errorf("non-host tools must keep the failure reason, got %v", got)
	}
}

// TestToolCardWrapsLongArg locks in the wrap contract for card bodies: a
// multiline argument still flattens to spaces, and an over-long command wraps
// with continuation lines hanging at outputIndent — the full command stays
// visible instead of being truncated, and no raw newline leaks a flush-left
// row (the "  " prefix only applies to the first line).
func TestToolCardWrapsLongArg(t *testing.T) {
	// Short arg: flattened single line, no raw newline.
	got := toolCard("bash", `{"command":"line1\nline2"}`, "", 120)
	if strings.Contains(got, "\n") {
		t.Errorf("short card must stay a single line, got raw newline: %q", got)
	}
	if !strings.Contains(got, "line1 line2") {
		t.Errorf("multiline arg should flatten to spaces, got %q", got)
	}

	// Over-wide multiline command: wraps, every line inside width, the full
	// text survives (no truncation), and continuations hang at outputIndent.
	got = toolCard("bash", `{"command":"cd /x && git commit -m \"subject\"\n\nlong body line one\nlong body line two"}`, "", 40)
	plain := ansi.Strip(got)
	lines := strings.Split(plain, "\n")
	if len(lines) < 2 {
		t.Fatalf("long command should wrap, got %q", got)
	}
	// The command's head and tail both survive — wrap never truncates.
	if !strings.Contains(plain, "cd /x && git commit") || !strings.Contains(plain, "body line two") {
		t.Errorf("wrapped card lost content: %q", got)
	}
	for i, l := range lines {
		if ansi.StringWidth(l) > 40 {
			t.Errorf("line %d overflows width %d: %q", i, ansi.StringWidth(l), l)
		}
		if i > 0 && !strings.HasPrefix(l, "    ") {
			t.Errorf("continuation line %d lost the hanging indent: %q", i, l)
		}
	}
}

// TestFailedToolCardReplacesPending proves a failed tool swaps its pending
// card in place: the scrollback holds exactly one card row (the red ⊘ form),
// never the "~ pending" row followed by a second failure row.
func TestFailedToolCardReplacesPending(t *testing.T) {
	m := newTestChatTUI()
	m.ingestEvent(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{ID: "f1", Name: "read_file", Args: `{"path":"x"}`}})
	m.ingestEvent(event.Event{Kind: event.ToolResult, Tool: event.Tool{ID: "f1", Name: "read_file", Err: "permission denied"}})

	joined := strings.Join(m.transcript, "\n")
	if strings.Count(joined, "Read") != 1 {
		t.Fatalf("failed call must leave exactly one card, got:\n%s", joined)
	}
	if !strings.Contains(joined, "⊘ permission denied") {
		t.Fatalf("card should show the failure detail, got:\n%s", joined)
	}
	if strings.Contains(joined, "~ Read") {
		t.Fatalf("pending row must not linger after a failure, got:\n%s", joined)
	}
	// Re-rendering (a width change) keeps the failure form, not the pending one.
	m.reflowTranscript(80)
	if got := strings.Join(m.transcript, "\n"); !strings.Contains(got, "⊘ permission denied") || strings.Contains(got, "~ Read") {
		t.Fatalf("reflow must keep the failure card, got:\n%s", got)
	}
}

// TestToolOutputLineNormalizes proves tool output renders as uniform dim
// text: the tool's own ANSI colour is stripped (raw colours would fight the
// dim), over-long lines clamp with a "…" tail, and the result stays inside
// the width budget.
func TestToolOutputLineNormalizes(t *testing.T) {
	got := toolOutputLine("\x1b[32mgreen text\x1b[0m", 80)
	if strings.Contains(got, "\x1b[32m") {
		t.Errorf("tool colour should be stripped, got %q", got)
	}
	if plain := ansi.Strip(got); !strings.Contains(plain, "green text") {
		t.Errorf("content lost: %q", got)
	}

	long := strings.Repeat("x", 100)
	got = toolOutputLine(long, 40)
	plain := ansi.Strip(got)
	if ansi.StringWidth(plain) > 40 {
		t.Errorf("line exceeds width: %d: %q", ansi.StringWidth(plain), plain)
	}
	if !strings.HasSuffix(plain, "…") {
		t.Errorf("truncated line should signal with a … tail: %q", plain)
	}
	if !strings.Contains(plain, "xxx") {
		t.Errorf("content lost: %q", plain)
	}
}

// TestAfterCRKeepsLatestFrame proves carriage-return progress frames render
// as their latest frame, not a mashed line.
func TestAfterCRKeepsLatestFrame(t *testing.T) {
	if got := afterCR(" 25%\r 50%\r 75%"); got != " 75%" {
		t.Errorf("afterCR = %q, want the last frame", got)
	}
	if got := afterCR("plain line"); got != "plain line" {
		t.Errorf("afterCR must pass through lines without \r, got %q", got)
	}
	if got := afterCR("done\r"); got != "" {
		t.Errorf("trailing \r should clear the line, got %q", got)
	}
}

// TestToolProgressNormalizesOutput proves streamed bash output strips the
// tool's ANSI colour, clamps over-long lines with a "…" tail, and drops
// stale \r progress frames.
func TestToolProgressNormalizesOutput(t *testing.T) {
	m := newTestChatTUI()
	m.ingestEvent(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{ID: "b1", Name: "bash", Args: `{"command":"go test"}`}})
	m.ingestEvent(event.Event{Kind: event.ToolProgress, Tool: event.Tool{ID: "b1", Output: "\x1b[32mok\x1b[0m " + strings.Repeat("x", 120) + "\n"}})
	m.ingestEvent(event.Event{Kind: event.ToolProgress, Tool: event.Tool{ID: "b1", Output: " 50%\r 90%\r"}})

	joined := strings.Join(m.transcript, "\n")
	if strings.Contains(joined, "\x1b[32m") {
		t.Errorf("tool colour leaked into the transcript: %q", joined)
	}
	plain := ansi.Strip(joined)
	if strings.Contains(plain, "50%") {
		t.Errorf("stale \r frame should be dropped, got %q", plain)
	}
	if !strings.Contains(plain, "…") {
		t.Errorf("over-long line should clamp with a … tail, got %q", plain)
	}
}

// TestFailedBashKeepsOutputPreview proves a failing bash call keeps both the
// collapsed output preview and the in-place failure card: the card slot is
// replaced, the live-output slot is collapsed as usual, and no "~ pending"
// or duplicate card lingers.
func TestFailedBashKeepsOutputPreview(t *testing.T) {
	m := newTestChatTUI()
	m.ingestEvent(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{ID: "shell-test", Name: "bash", Args: `{"command":"go test"}`}})
	m.ingestEvent(event.Event{Kind: event.ToolProgress, Tool: event.Tool{ID: "shell-test", Output: "ok pkg/a\n"}})
	m.ingestEvent(event.Event{Kind: event.ToolResult, Tool: event.Tool{ID: "shell-test", Name: "bash", Err: "exit status 1"}})

	joined := strings.Join(m.transcript, "\n")
	if strings.Count(joined, "Bash") != 1 {
		t.Fatalf("failed bash must leave exactly one card, got:\n%s", joined)
	}
	if !strings.Contains(joined, "⊘ exit status 1") {
		t.Fatalf("card should show the failure detail, got:\n%s", joined)
	}
	if strings.Contains(joined, "~ Bash") {
		t.Fatalf("pending row must not linger, got:\n%s", joined)
	}
	if !strings.Contains(joined, "ok pkg/a") {
		t.Fatalf("collapsed output preview should survive the failure, got:\n%s", joined)
	}
}

// TestCompletedCardReachesViewport proves a successful tool result re-renders
// the card in the viewport payload, not just in the transcript model: the
// wrap cache must be invalidated so the next sync re-wraps the card block.
// The regression: a bare transcript[idx] assignment left the stale "~ pending"
// row cached forever — the model said "→ Read" while the screen kept "~ Read".
func TestCompletedCardReachesViewport(t *testing.T) {
	m := newTestChatTUI()
	contentW := transcriptContentWidth(m.width, m.nativeScrollback)

	m.ingestEvent(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{ID: "r1", Name: "read_file", Args: `{"path":"x"}`}})
	m.syncWrappedLines(contentW, false)
	if got := m.wrappedContentString(); !strings.Contains(got, "~ Read x") {
		t.Fatalf("viewport should show the pending card before the result, got:\n%s", got)
	}

	m.ingestEvent(event.Event{Kind: event.ToolResult, Tool: event.Tool{ID: "r1", Name: "read_file", Output: "a\nb\nc\n"}})
	m.syncWrappedLines(contentW, false)
	got := m.wrappedContentString()
	if strings.Contains(got, "~ Read") {
		t.Fatalf("viewport still shows the stale pending row, got:\n%s", got)
	}
	if !strings.Contains(got, "→ Read x") || !strings.Contains(got, "3 lines") {
		t.Fatalf("viewport should show the completed card with its line count, got:\n%s", got)
	}
}

// TestFailedCardReachesViewport proves a failed tool result swaps the pending
// card for the red ⊘ form in the viewport payload too (renderToolFailure goes
// through setTranscriptBlock, which invalidates the wrap cache).
func TestFailedCardReachesViewport(t *testing.T) {
	m := newTestChatTUI()
	contentW := transcriptContentWidth(m.width, m.nativeScrollback)

	m.ingestEvent(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{ID: "f1", Name: "read_file", Args: `{"path":"x"}`}})
	m.ingestEvent(event.Event{Kind: event.ToolResult, Tool: event.Tool{ID: "f1", Name: "read_file", Err: "permission denied"}})
	m.syncWrappedLines(contentW, false)

	got := m.wrappedContentString()
	if strings.Contains(got, "~ Read") {
		t.Fatalf("viewport still shows the stale pending row, got:\n%s", got)
	}
	if !strings.Contains(got, "⊘ permission denied") {
		t.Fatalf("viewport should show the failure card, got:\n%s", got)
	}
}
