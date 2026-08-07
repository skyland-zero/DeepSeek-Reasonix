package control

import (
	"encoding/json"
	"strings"

	"reasonix/internal/evidence"
	"reasonix/internal/event"
)

// Local plan-archive helpers (fork-owned). They complement upstream's
// plan_todos.go: upstream seeds/completes plan task lists through the
// approval-gated execution path, while this fork's non-blocking plan mode
// archives a freshly produced plan list at plan-turn end.

// planTodosSnapshot returns the executor's canonical task list at turn start,
// so the plan gate can tell a freshly produced plan list from a stale one.
func (c *Controller) planTodosSnapshot() []evidence.TodoItem {
	if c.executor == nil {
		return nil
	}
	return c.executor.CanonicalTodoState()
}

// finishPlanTurnTodos archives a plan produced during the just-finished plan
// turn: when the model laid out a new task list (different from the pre-turn
// snapshot), emit a synthetic all-completed todo_write so the pinned task
// panel clears after the proposal is delivered instead of hanging on pending
// planning steps. A stale list from a previous execution turn is left alone —
// that work may still be unfinished. A running goal is left alone too: goals
// own their task list and their FSM's completion intercept reads it. Like
// completePlanTodos, it bypasses the todo_write tool and touches neither the
// transcript nor the evidence ledger, so the model's own list stays the
// execution baseline.
func (c *Controller) finishPlanTurnTodos(before []evidence.TodoItem) {
	if c.executor == nil || c.GoalStatus() == GoalStatusRunning {
		return
	}
	after := c.executor.CanonicalTodoState()
	if len(after) == 0 || todoStatesEqual(before, after) {
		return
	}
	args := completedTodoArgs(after)
	if args == "" {
		return
	}
	t := event.Tool{ID: "plan-seed", Name: "todo_write", Args: args, ReadOnly: true}
	c.sink.Emit(event.Event{Kind: event.ToolDispatch, Tool: t})
	t.Output = "plan produced; task list archived"
	c.sink.Emit(event.Event{Kind: event.ToolResult, Tool: t})
	c.replaceAgentTodoState(args)
}

// todoStatesEqual compares two canonical task lists ignoring nothing: content,
// status, and level all matter, because any change means the model produced a
// new list this turn.
func todoStatesEqual(a, b []evidence.TodoItem) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Content != b[i].Content || strings.TrimSpace(a[i].Status) != strings.TrimSpace(b[i].Status) || a[i].Level != b[i].Level {
			return false
		}
	}
	return true
}

// completedTodoArgs serializes a canonical task list as an all-completed
// todo_write payload so the plan panel can be archived without touching the
// model's own execution baseline.
func completedTodoArgs(todos []evidence.TodoItem) string {
	if len(todos) == 0 {
		return ""
	}
	items := make([]seedTodo, len(todos))
	for i, t := range todos {
		items[i] = seedTodo{Content: t.Content, Status: "completed", Level: t.Level}
	}
	b, err := json.Marshal(map[string]any{"todos": items})
	if err != nil {
		return ""
	}
	return string(b)
}
