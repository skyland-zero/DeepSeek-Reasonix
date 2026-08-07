package control

import (
	"context"
	"strings"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/evidence"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

// planToolRegistry wires the real todo_write builtin so scripted plan turns can
// call it like the model does.
func planToolRegistry(t *testing.T) *tool.Registry {
	t.Helper()
	reg := tool.NewRegistry()
	todoWrite, ok := tool.LookupBuiltin("todo_write")
	if !ok {
		t.Fatal("todo_write builtin not registered")
	}
	reg.Add(todoWrite)
	return reg
}

func planTodoTurn(toolArgs string, text string) [][]provider.Chunk {
	var turns [][]provider.Chunk
	if toolArgs != "" {
		turns = append(turns, []provider.Chunk{{
			Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{
				ID: "plan-todo", Name: "todo_write", Arguments: toolArgs,
			},
		}, {Type: provider.ChunkDone}})
	}
	if text != "" {
		turns = append(turns, []provider.Chunk{{Type: provider.ChunkText, Text: text}, {Type: provider.ChunkDone}})
	}
	return turns
}

// TestPlanTurnArchivesNewTodos proves the non-blocking plan flow's closing act:
// when the model laid out a fresh task list during the plan turn, the gate
// emits an all-completed synthetic todo_write so the pinned panel clears, and
// the executor's canonical state follows. Plan mode itself stays on.
func TestPlanTurnArchivesNewTodos(t *testing.T) {
	prov := &scriptedTurns{turns: planTodoTurn(`{"todos":[
		{"content":"Add the config loader","status":"in_progress"},
		{"content":"Wire it into boot","status":"pending"},
		{"content":"Add tests","status":"pending"}]}`, "Plan:\n1. Add the config loader\n2. Wire it into boot\n3. Add tests")}
	ag := agent.New(prov, planToolRegistry(t), agent.NewSession(""), agent.Options{}, event.Discard)

	var archivedArgs []string
	c := New(Options{
		Runner:   ag,
		Executor: ag,
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.ToolResult && e.Tool.ID == "plan-seed" && e.Tool.Name == "todo_write" && e.Tool.Err == "" {
				archivedArgs = append(archivedArgs, e.Tool.Args)
			}
		}),
	})
	c.SetPlanMode(true)

	if err := c.runTurnWithRaw(context.Background(), "Plan this feature", "Plan this feature"); err != nil {
		t.Fatalf("runTurnWithRaw: %v", err)
	}
	if !c.PlanMode() {
		t.Fatal("non-blocking plan turn should keep plan mode on")
	}
	if len(archivedArgs) != 1 {
		t.Fatalf("plan-seed archive results = %d, want 1: %#v", len(archivedArgs), archivedArgs)
	}
	last := archivedArgs[len(archivedArgs)-1]
	if strings.Contains(last, `"in_progress"`) || strings.Contains(last, `"pending"`) {
		t.Fatalf("archived plan todos should be completed so the panel hides: %s", last)
	}
	if !strings.Contains(last, `"completed"`) {
		t.Fatalf("archived plan todos should contain completed items: %s", last)
	}
	for _, todo := range ag.CanonicalTodoState() {
		if strings.TrimSpace(todo.Status) != "completed" {
			t.Fatalf("executor canonical state should be archived as completed, got %+v", ag.CanonicalTodoState())
		}
	}
}

// TestPlanTurnLeavesStaleTodosAlone proves the archive is scoped to lists the
// model produced this turn: a task list left over from a previous execution
// turn (no todo_write in the plan turn) stays untouched — archiving it would
// falsely mark unfinished work as done.
func TestPlanTurnLeavesStaleTodosAlone(t *testing.T) {
	prov := &scriptedTurns{turns: planTodoTurn("", "Plan:\n1. Investigate\n2. Fix")}
	ag := agent.New(prov, planToolRegistry(t), agent.NewSession(""), agent.Options{}, event.Discard)
	ag.SeedTodoState([]evidence.TodoItem{{Content: "old execution step", Status: "in_progress"}})

	var archived int
	c := New(Options{
		Runner:   ag,
		Executor: ag,
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.ToolResult && e.Tool.ID == "plan-seed" && e.Tool.Name == "todo_write" {
				archived++
			}
		}),
	})
	c.SetPlanMode(true)

	if err := c.runTurnWithRaw(context.Background(), "Plan this feature", "Plan this feature"); err != nil {
		t.Fatalf("runTurnWithRaw: %v", err)
	}
	if archived != 0 {
		t.Fatalf("stale execution todos were archived: %d plan-seed results", archived)
	}
	got := ag.CanonicalTodoState()
	if len(got) != 1 || got[0].Content != "old execution step" || strings.TrimSpace(got[0].Status) != "in_progress" {
		t.Fatalf("stale execution todos should be preserved, got %+v", got)
	}
}

// TestPlanTurnMidTurnToggleDoesNotArchiveExecutionList proves the gate uses
// the turn-boundary plan-mode snapshot: flipping plan mode on mid-turn must
// not archive (mark completed) an execution turn's unfinished task list.
func TestPlanTurnMidTurnToggleDoesNotArchiveExecutionList(t *testing.T) {
	prov := &scriptedTurns{turns: [][]provider.Chunk{
		{{
			Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{
				ID: "exec-todo", Name: "todo_write",
				Arguments: `{"todos":[{"content":"Fix the parser","status":"in_progress"}]}`,
			},
		}, {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "Done."}, {Type: provider.ChunkDone}},
	}}
	ag := agent.New(prov, planToolRegistry(t), agent.NewSession(""), agent.Options{}, event.Discard)
	toggler := &midTurnPlanToggler{inner: ag}

	var archived int
	c := New(Options{
		Runner:   toggler,
		Executor: ag,
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.ToolResult && e.Tool.ID == "plan-seed" && e.Tool.Name == "todo_write" {
				archived++
			}
		}),
	})
	toggler.c = c
	c.SetPlanMode(false)

	if err := c.runTurnWithRaw(context.Background(), "fix it", "fix it"); err != nil {
		t.Fatalf("runTurnWithRaw: %v", err)
	}
	if archived != 0 {
		t.Fatalf("mid-turn plan toggle archived the execution list: %d plan-seed results", archived)
	}
	got := ag.CanonicalTodoState()
	if len(got) != 1 || strings.TrimSpace(got[0].Status) != "in_progress" {
		t.Fatalf("execution todo should stay in_progress, got %+v", got)
	}
}

// TestFinishPlanTurnTodosSkipsRunningGoal proves the archive is disabled while
// a goal owns the task list — the goal FSM's completion intercept reads the
// canonical todos, and archiving them completed would let an empty goal
// "complete".
func TestFinishPlanTurnTodosSkipsRunningGoal(t *testing.T) {
	ag := agent.New(nil, planToolRegistry(t), agent.NewSession(""), agent.Options{}, event.Discard)
	ag.SeedTodoState([]evidence.TodoItem{{Content: "goal step", Status: "in_progress"}})

	var archived int
	c := New(Options{
		Runner:   ag,
		Executor: ag,
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.ToolResult && e.Tool.ID == "plan-seed" && e.Tool.Name == "todo_write" {
				archived++
			}
		}),
	})
	c.SetGoal("pursue the goal")
	c.SetPlanMode(true)

	c.finishPlanTurnTodos(nil)
	if archived != 0 {
		t.Fatalf("archive ran while a goal was running: %d plan-seed results", archived)
	}
	got := ag.CanonicalTodoState()
	if len(got) != 1 || strings.TrimSpace(got[0].Status) != "in_progress" {
		t.Fatalf("goal todos should stay untouched, got %+v", got)
	}
}

// midTurnPlanToggler wraps the agent runner and flips plan mode on right
// before the underlying turn runs, simulating a frontend toggling Shift+Tab
// while a turn is already executing.
type midTurnPlanToggler struct {
	inner agent.Runner
	c     *Controller
}

func (t midTurnPlanToggler) Run(ctx context.Context, input string) error {
	if t.c != nil {
		t.c.SetPlanMode(true)
	}
	return t.inner.Run(ctx, input)
}
