// Formats a tool call as an opencode-style card line: a semantic icon
// ("→ Read pkg/a.go [limit=120]", "← Update x.go", "✱ Glob \"*.go\"") with an
// opencode-style pending state ("~ Read pkg/a.go [limit=120]", the same body
// as the completed form) while the tool runs, plus the plain "outputBlock"
// indentation for folded content (bash live output, streamed reasoning) that
// renders under its card as quiet code.
package cli

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"reasonix/internal/event"
	"reasonix/internal/shellrun"
	"reasonix/internal/tool"
)

// connector is kept as an upstream-compatible name so upstream chat_tui.go's
// clamp math keeps working; it now denotes the local plain-indent margin.
const connector = outputIndent

// connectorBlock is the upstream-compatible name for outputBlock: plain
// indented lines with no "⎿" gutter. Upstream chat_tui.go routes reasoning,
// streaming tool output, collapse summaries, subagent progress and the
// working tick through it, so replacing its implementation here switches all
// of them to local style without editing chat_tui.go.
func connectorBlock(lines []string) string { return outputBlock(lines) }

// outputIndent is the left margin of a content block under a tool card or the
// "▎ thinking…" marker (bash live output, its collapsed "N lines" marker,
// streamed reasoning). Blocks render as plain indented lines with no per-line
// connector, so they read as quiet code under their header rather than a tree
// of continuations.
const outputIndent = "    "

// outputBlock renders lines as a plain indented block under a tool card: every
// line gets the same outputIndent margin, first line included, and no "⎿"
// gutter. Returns "" for no lines.
func outputBlock(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	var b strings.Builder
	for i, ln := range lines {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(outputIndent)
		b.WriteString(ln)
	}
	return b.String()
}

// toolVerb maps a tool's snake_case id to the verb shown in its card.
var toolVerb = map[string]string{
	"bash":           "Bash",
	"bash_output":    "Output",
	"kill_shell":     "Kill",
	"wait":           "Wait",
	"read_file":      "Read",
	"write_file":     "Write",
	"edit_file":      "Update",
	"multi_edit":     "Update",
	"move_file":      "Move",
	"delete_range":   "Update",
	"delete_symbol":  "Update",
	"notebook_edit":  "Update",
	"glob":           "Glob",
	"grep":           "Search",
	"ls":             "List",
	"web_fetch":      "Fetch",
	"web_search":     "Search",
	"task":           "Task",
	"use_capability": "MCP",
}

// toolArgKey is the JSON field shown as the card's primary argument for each
// tool (wait is special-cased — it carries a job_ids array, not a scalar).
var toolArgKey = map[string]string{
	"bash":          "command",
	"bash_output":   "job_id",
	"kill_shell":    "job_id",
	"read_file":     "path",
	"write_file":    "path",
	"edit_file":     "path",
	"multi_edit":    "path",
	"move_file":     "source_path",
	"delete_range":  "path",
	"delete_symbol": "name",
	"notebook_edit": "path",
	"glob":          "pattern",
	"grep":          "pattern",
	"ls":            "path",
	"web_fetch":     "url",
	"web_search":    "query",
	"task":          "description",
}

// toolIconMap maps a tool id to the opencode-style semantic icon shown in its
// card. The fallback for unknown tools (and use_capability) is the generic
// gear; task switches between "│" (running) and "✓" (completed) in toolIcon.
var toolIconMap = map[string]string{
	"read_file": "→", "ls": "→", "bash_output": "→",
	"write_file": "←", "edit_file": "←", "multi_edit": "←", "move_file": "←",
	"delete_range": "←", "delete_symbol": "←", "notebook_edit": "←",
	"glob": "✱", "grep": "✱",
	"web_fetch": "%", "web_search": "◈",
	"bash": "$",
	"wait": "⚙", "kill_shell": "⚙", "use_capability": "⚙",
}

// toolIcon returns the semantic icon for a tool; unknown ids get the generic
// gear. task is the one tool whose icon changes on completion (│ → ✓).
func toolIcon(name string, completed bool) string {
	if name == "task" {
		if completed {
			return "✓"
		}
		return "│"
	}
	if v, ok := toolIconMap[name]; ok {
		return v
	}
	return "⚙"
}

// toolCategory maps a tool id to its colour category.
var toolCategory = map[string]string{
	"read_file": "read", "ls": "read", "glob": "read", "grep": "read",
	"web_fetch": "read", "web_search": "read", "bash_output": "read",
	"write_file": "write", "edit_file": "write", "multi_edit": "write",
	"move_file": "write", "delete_range": "write", "delete_symbol": "write", "notebook_edit": "write",
	"bash": "exec",
	"wait": "proc", "kill_shell": "proc",
}

// toolCategoryColor returns the running-state colour for a tool's category:
// reads are accent-tinted, writes green, bash amber, process controls blue,
// and everything else (task, MCP, unknown) uses the accent colour.
func toolCategoryColor(name string) cliColor {
	switch toolCategory[name] {
	case "read":
		return activeCLITheme.toolRead
	case "write":
		return activeCLITheme.success
	case "exec":
		return activeCLITheme.warn
	case "proc":
		return activeCLITheme.toolProc
	default:
		return activeCLITheme.accent
	}
}

// toolDisplayName returns the card verb for a tool: a mapped builtin verb, the
// short name for an MCP tool (mcp__server__tool), or the raw id as a fallback.
func toolDisplayName(name string) string {
	if _, short, ok := tool.SplitMCPName(name); ok {
		return short
	}
	if v, ok := toolVerb[name]; ok {
		return v
	}
	return name
}

// shellToolDisplayName prefers the actual interpreter label when structured
// execution metadata is present (Git Bash / Windows PowerShell / PowerShell 7+).
func shellToolDisplayName(name string, ex *event.ShellExecution) string {
	if name == "bash" && ex != nil && ex.Shell != "" {
		return shellrun.DisplayName(&tool.ShellExecution{Shell: ex.Shell, ShellVersion: ex.ShellVersion})
	}
	return toolDisplayName(name)
}

// shellFailureDetail appends exit code, failure phase, and mutation-risk hints
// for failed shell results. Empty when there is no structured metadata.
func shellFailureDetail(ex *event.ShellExecution) string {
	if ex == nil {
		return ""
	}
	var parts []string
	if ex.ExitCode != nil {
		parts = append(parts, fmt.Sprintf("exit %d", *ex.ExitCode))
	}
	if ex.FailurePhase != "" {
		parts = append(parts, ex.FailurePhase)
	}
	switch ex.FailurePhase {
	case tool.ShellPhasePreflight, tool.ShellPhaseAuthorization, tool.ShellPhaseDependency, tool.ShellPhaseLaunch:
		parts = append(parts, "not executed")
	default:
		if ex.MutationRisk == tool.ShellMutationMayBePartial {
			parts = append(parts, "may be partial")
		}
	}
	return strings.Join(parts, " · ")
}

// toolArg pulls the primary argument shown in the card.
func toolArg(name, args string) string {
	var m map[string]any
	if json.Unmarshal([]byte(args), &m) != nil {
		return ""
	}
	if name == "wait" {
		return argList(m["job_ids"])
	}
	if name == "use_capability" {
		if id, ok := m["capability_id"].(string); ok && strings.TrimSpace(id) != "" {
			return strings.TrimSpace(id)
		}
		if action, ok := m["action"].(string); ok {
			return strings.TrimSpace(action)
		}
		return ""
	}
	v, ok := m[toolArgKey[name]]
	if !ok {
		return ""
	}
	switch x := v.(type) {
	case string:
		return strings.TrimSpace(x)
	case []any:
		return argList(x)
	case float64:
		return strconv.Itoa(int(x))
	default:
		return ""
	}
}

func argList(v any) string {
	arr, ok := v.([]any)
	if !ok {
		return ""
	}
	parts := make([]string, 0, len(arr))
	for _, e := range arr {
		if s, ok := e.(string); ok {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, ", ")
}

// toolStreamsOutput reports whether a tool streams live output via
// ToolProgress. Only bash registers a progress callback (its progressWriter);
// every other tool — read_file, glob, grep, edit_file, task, MCP calls —
// delivers its result in one ToolResult, so its card stays a single line with
// the output folded into a "N lines" suffix. The tool id cannot drive this:
// parallel bash calls use "call_<n>" ids, not "shell-".
func toolStreamsOutput(name string) bool {
	return name == "bash"
}

// outputLineCount counts the completed lines in a tool's result output; 0
// means no output. Mirrors collapseShellSlot's counting so the "N lines"
// suffix on a non-streaming card matches what a streamed tool would report.
func outputLineCount(out string) int {
	if out == "" {
		return 0
	}
	return len(strings.Split(strings.TrimRight(out, "\n"), "\n"))
}

// toolCardLine renders the full opencode-style card line:
// "  → Read pkg/a.go [limit=120]", with the icon in the tool's category
// colour, the verb bold, and the arguments dim. completed switches task's
// icon (│ → ✓); id appends a short call-id tag to task cards so parallel
// subagents stay distinguishable. The line is clamped to width.
func toolCardLine(name, args, id string, completed bool, width int) string {
	body := toolBody(name, args)
	if tail := toolIDTail(name, id); tail != "" {
		body += " " + dim(tail)
	}
	avail := max(width-3, 1) // "  " prefix + icon + space
	return "  " + themeFg(toolCategoryColor(name), toolIcon(name, completed)) + " " + clampPlain(body, avail)
}

// toolBody builds "Verb 主参数 [k=v, ...]" for every tool, bash included, so
// all cards share one shape: semantic icon + verb + primary argument + extra
// options. The primary argument's newlines flatten to spaces so a multiline
// value (a git commit -m body, say) cannot render continuation lines flush
// against the left edge: the "  " card prefix applies to the first line only,
// so any raw \n would leave later lines without a margin.
func toolBody(name, args string) string {
	body := bold(toolDisplayName(name))
	if arg := toolArg(name, args); arg != "" {
		body += " " + dim(flattenArg(arg))
	}
	if extra := toolExtraArgs(name, args); extra != "" {
		body += " " + dim("["+extra+"]")
	}
	return body
}

// flattenArg collapses CR/newlines in a card argument to spaces, keeping the
// single-line card contract intact.
func flattenArg(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "\r", ""), "\n", " ")
}

// toolIDTail returns the short call-id tag shown on a task card so parallel
// subagents stay distinguishable; every other tool identifies by its primary
// argument alone. Returns "" when nothing should be shown.
func toolIDTail(name, id string) string {
	if name != "task" || id == "" {
		return ""
	}
	return "[" + shortToolID(id) + "]"
}

// shortToolID trims a tool-call id for card display: ids up to 12 runes stay
// whole, longer ones keep their trailing 12 runes.
func shortToolID(id string) string {
	r := []rune(id)
	if len(r) <= 12 {
		return id
	}
	return string(r[len(r)-12:])
}

// toolExtraArgs renders the opencode-style "[k=v, ...]" suffix from the
// primitive (string/number/boolean) arguments, excluding the primary argument
// field and bulky fields (file contents, nested MCP arguments, commands).
// Keys sort alphabetically so the output is deterministic for tests.
func toolExtraArgs(name, args string) string {
	var m map[string]any
	if json.Unmarshal([]byte(args), &m) != nil {
		return ""
	}
	omit := map[string]bool{toolArgKey[name]: true}
	switch name {
	case "bash", "task":
		omit["command"] = true
	case "write_file", "edit_file", "multi_edit", "notebook_edit":
		omit["content"] = true
		omit["old_string"] = true
		omit["new_string"] = true
	case "use_capability":
		omit["capability_id"] = true
		omit["action"] = true
		omit["arguments"] = true
	case "wait":
		omit["job_ids"] = true
	case "bash_output", "kill_shell":
		omit["job_id"] = true
	}
	keys := make([]string, 0, len(m))
	for k, v := range m {
		if omit[k] {
			continue
		}
		switch v.(type) {
		case string, float64, bool:
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		switch x := m[k].(type) {
		case string:
			// Flatten newlines so a multiline value cannot break the card's
			// single-line contract (the "  " prefix only applies to line 1).
			flat := strings.ReplaceAll(strings.ReplaceAll(x, "\r", ""), "\n", " ")
			parts = append(parts, k+"="+flat)
		case float64:
			parts = append(parts, k+"="+strconv.Itoa(int(x)))
		case bool:
			parts = append(parts, k+"="+strconv.FormatBool(x))
		}
	}
	return strings.Join(parts, ", ")
}

// toolCard renders the dispatch-state line for streaming tools (bash shows
// its "$ Bash command" line immediately, since its live output block follows
// below). Non-streaming tools render toolPendingLine while running instead.
func toolCard(name, args, id string, width int) string {
	return toolCardLine(name, args, id, false, width)
}

// toolPendingLine renders the opencode-style running state for a dispatched
// non-streaming tool: "  ~ Read pkg/a.go [limit=120]" — the same card body as
// the completed form, with the icon slot replaced by "~". It is static —
// bash's indented "working" tick is the only card animation. The body clamps
// to width-4 so "  " + "~ " + body lands exactly at width.
func toolPendingLine(name, args, id string, width int) string {
	body := toolBody(name, args)
	if tail := toolIDTail(name, id); tail != "" {
		body += " " + dim(tail)
	}
	return "  " + themeFg(toolCategoryColor(name), "~ "+clampPlain(body, max(width-4, 1)))
}

// toolCardCompleted renders a finished tool line, dimmed like opencode's
// muted completed rows: "  → Read pkg/a.go [limit=120]". The caller appends
// the "N lines" suffix from the tool's output.
func toolCardCompleted(name, args, id string, width int) string {
	return dim(toolCardLine(name, args, id, true, width))
}

// toolCardFailed renders the failure line: the tool's semantic icon and the
// "⊘ reason" detail in the error red. An empty reason (host-evidence tools)
// keeps just the icon + verb marker.
func toolCardFailed(name, err string, width int) string {
	label := bold(toolDisplayName(name))
	line := "  " + red(toolIcon(name, false)) + " " + label
	if err == "" {
		return line
	}
	avail := max(width-3-visibleWidth(label)-3, 1) // icon/space + " ⊘ " prefix
	return line + " " + red("⊘ "+clampPlain(err, avail))
}
