package builtin

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"reasonix/internal/evidence"
	"reasonix/internal/planmode"
)

// planCtx stamps a plan-first-workflow context for todo_write tests.
func planCtx(base context.Context) context.Context {
	return planmode.WithActive(evidence.WithLedger(base, evidence.NewLedger()), true)
}

// TestTodoWritePlanModeAcceptsPendingOnlyList proves the proposal surface: a
// plan with no in_progress item is fine while planning, because nothing is
// being executed yet. Outside plan mode the same list stays rejected.
func TestTodoWritePlanModeAcceptsPendingOnlyList(t *testing.T) {
	args := json.RawMessage(`{"todos":[
		{"content":"Add the config loader","status":"pending"},
		{"content":"Wire it into boot","status":"pending"},
		{"content":"Add tests","status":"pending"}]}`)
	if _, err := (todoWrite{}).Execute(planCtx(context.Background()), args); err != nil {
		t.Fatalf("plan-mode pending-only list should be accepted: %v", err)
	}
	if _, err := (todoWrite{}).Execute(context.Background(), args); err == nil {
		t.Fatal("execution-mode pending-only list should still be rejected")
	}
}

// TestTodoWritePlanModeAcceptsCompletedWithoutReceipt proves that completed
// items need no complete_step receipt while planning — complete_step is
// phase-blocked in plan mode, so requiring receipts would deadlock planning.
func TestTodoWritePlanModeAcceptsCompletedWithoutReceipt(t *testing.T) {
	ledger := evidence.NewLedger()
	ledger.Record(evidence.Receipt{
		ToolName: "todo_write",
		Success:  true,
		Todos:    []evidence.TodoItem{{Content: "Research the API", Status: "in_progress"}},
	})
	plan := planmode.WithActive(evidence.WithLedger(context.Background(), ledger), true)
	args := json.RawMessage(`{"todos":[
		{"content":"Research the API","status":"completed"},
		{"content":"Draft the plan","status":"in_progress"}]}`)
	if _, err := (todoWrite{}).Execute(plan, args); err != nil {
		t.Fatalf("plan-mode completed transition should not need a complete_step receipt: %v", err)
	}
	if _, err := (todoWrite{}).Execute(evidence.WithLedger(context.Background(), ledger), args); err == nil {
		t.Fatal("execution-mode completed transition without receipt should still be rejected")
	}
}

// TestTodoWritePlanModeAllowsReplacingCurrentItem proves the current-item
// continuity guard is suspended while planning: the model freely rewrites the
// proposal, even dropping the previous in_progress item.
func TestTodoWritePlanModeAllowsReplacingCurrentItem(t *testing.T) {
	ledger := evidence.NewLedger()
	ledger.Record(evidence.Receipt{
		ToolName: "todo_write",
		Success:  true,
		Todos:    []evidence.TodoItem{{Content: "Old execution step", Status: "in_progress"}},
	})
	plan := planmode.WithActive(evidence.WithLedger(context.Background(), ledger), true)
	args := json.RawMessage(`{"todos":[
		{"content":"Inspect the new request","status":"in_progress"},
		{"content":"Draft a revised plan","status":"pending"}]}`)
	if _, err := (todoWrite{}).Execute(plan, args); err != nil {
		t.Fatalf("plan-mode replacement of the current item should be accepted: %v", err)
	}
	if _, err := (todoWrite{}).Execute(evidence.WithLedger(context.Background(), ledger), args); err == nil {
		t.Fatal("execution-mode replacement of the current item should still be rejected")
	}
}

// TestTodoWritePlanModeStillRejectsBrokenStructure proves structural shape
// checks stay on in plan mode: orphan sub-steps and invalid statuses are
// invalid proposals either way.
func TestTodoWritePlanModeStillRejectsBrokenStructure(t *testing.T) {
	for _, tc := range []struct {
		name string
		args string
		want string
	}{
		{
			name: "orphan sub-step",
			args: `{"todos":[{"content":"sub","status":"in_progress","level":1}]}`,
			want: "no phase above",
		},
		{
			name: "invalid status",
			args: `{"todos":[{"content":"a","status":"done"}]}`,
			want: "invalid status",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := (todoWrite{}).Execute(planCtx(context.Background()), json.RawMessage(tc.args))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("plan-mode todo_write error = %v, want %q", err, tc.want)
			}
		})
	}
}

// TestTodoWritePlanModeAcceptsDuplicateCurrentItems proves plan mode tolerates
// a sloppy proposal with two in_progress items (normalized before validation)
// — the strict serial shape is an execution-phase rule.
func TestTodoWritePlanModeAcceptsDuplicateCurrentItems(t *testing.T) {
	args := json.RawMessage(`{"todos":[
		{"content":"a","status":"in_progress"},
		{"content":"b","status":"in_progress"}]}`)
	if _, err := (todoWrite{}).Execute(planCtx(context.Background()), args); err != nil {
		t.Fatalf("plan-mode duplicate current items should be tolerated: %v", err)
	}
}
