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
	// A long pending body clamps to width, not width+2 (the "  " prefix and
	// "~ " icon slot both count against the budget).
	if got := toolPendingLine("task", `{"description":"`+strings.Repeat("x", 200)+`"}`, "", 40); ansi.StringWidth(got) > 40 {
		t.Errorf("pending line overflows width: %d > 40: %q", ansi.StringWidth(got), got)
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

// TestToolCardFlattensMultilineArg locks in the single-line card contract for
// multiline arguments: a bash command with embedded newlines (a git commit -m
// body, say) must render as one line. The "  " prefix only applies to the
// first line, so any raw \n in the arg would leave continuation lines flush
// against the left edge — the regression seen as a missing left margin.
func TestToolCardFlattensMultilineArg(t *testing.T) {
	// Width enough to keep the whole flattened arg: the continuation text
	// must appear space-joined inside the card, with no raw newline.
	got := toolCard("bash", `{"command":"line1\nline2"}`, "", 120)
	if strings.Contains(got, "\n") {
		t.Errorf("toolCard must be a single line, got raw newline: %q", got)
	}
	if !strings.Contains(got, "line1 line2") {
		t.Errorf("multiline arg should flatten to spaces, got %q", got)
	}

	// Over-wide multiline command: the ansi.Truncate clamp path must not leak
	// a newline either.
	got = toolCard("bash", `{"command":"cd /x && git commit -m \"subject\"\n\nlong body line one\nlong body line two"}`, "", 40)
	if strings.Contains(got, "\n") {
		t.Errorf("clamped toolCard must stay a single line, got raw newline: %q", got)
	}
}
