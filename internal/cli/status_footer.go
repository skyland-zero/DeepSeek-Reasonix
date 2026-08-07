package cli

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"reasonix/internal/event"
	"reasonix/internal/i18n"
	"reasonix/internal/provider"
)

const (
	statusFooterIndent   = "  "
	statusFooterGroupGap = 2
)

// statusModeTag renders the local foreground-bold mode pill ("Auto", "YOLO",
// "Plan", "Shell") that the upstream chat_tui.go View requests through this
// hook, so all status-line styling stays in local-owned files.
func (m chatTUI) statusModeTag(shellMode bool) string {
	if shellMode {
		return lipgloss.NewStyle().
			Foreground(themeLipColor(statusShellTagColor())).
			Bold(true).
			PaddingRight(1).
			Render("Shell")
	}
	color := statusAutoTagColor()
	switch {
	case m.ctrl.AutoApproveTools():
		color = statusYoloTagColor()
	case m.planMode:
		color = statusPlanTagColor()
	}
	return lipgloss.NewStyle().
		Foreground(themeLipColor(color)).
		Bold(true).
		PaddingRight(1).
		Render(m.modeTagText())
}

func statusAutoTagColor() cliColor  { return activeCLITheme.accent }
func statusPlanTagColor() cliColor  { return activeCLITheme.info }
func statusYoloTagColor() cliColor  { return activeCLITheme.danger }
func statusShellTagColor() cliColor { return activeCLITheme.success }

func footerLabel(label string) string {
	return themeFg(activeCLITheme.subtle, label)
}

func footerHint(hint string) string {
	return themeFg(activeCLITheme.subtle, hint)
}

func footerValue(value string) string {
	return themeFg(activeCLITheme.muted, value)
}

func footerInfo(value string) string {
	return themeFg(activeCLITheme.info, value)
}

func footerSecondary(value string) string {
	return themeFg(activeCLITheme.secondary, value)
}

func footerMetric(label, value string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	return footerLabel(label) + " " + value
}

// renderTurnReceipt attaches the completed turn's token and cost breakdown to
// the assistant response. Unlike the persistent footer, this is historical
// message metadata: it stays in transcript scrollback and deliberately uses a
// quieter palette than runtime/session state.
func renderTurnReceipt(u *provider.Usage, p *provider.Pricing, d *event.CacheDiagnostics) string {
	if u == nil || u.TotalTokens == 0 {
		return ""
	}

	groups := []string{shortTokens(u.TotalTokens) + " tok"}
	if u.PromptTokens > 0 {
		cached := u.CacheHitTokens
		fresh := u.CacheMissTokens
		if fresh == 0 {
			fresh = max(u.PromptTokens-cached, 0)
		}
		groups = append(groups,
			"in "+shortTokens(u.PromptTokens),
			"cached "+shortTokens(cached),
			"new "+shortTokens(fresh),
		)
	}
	groups = append(groups, "out "+shortTokens(u.CompletionTokens))
	if u.ReasoningTokens > 0 {
		groups = append(groups, "reasoning "+shortTokens(u.ReasoningTokens))
	}
	if p != nil {
		groups = append(groups, fmt.Sprintf("%s%.4f", p.Symbol(), p.Cost(u)))
	}

	separator := footerHint(" · ")
	styled := make([]string, 0, len(groups))
	for _, group := range groups {
		styled = append(styled, footerValue(group))
	}
	receipt := statusFooterIndent + footerLabel(i18n.M.ChatTurnReceiptLabel) + "  " + strings.Join(styled, separator)
	if d != nil && d.PrefixChanged {
		reasons := strings.Join(d.PrefixChangeReasons, "+")
		if reasons == "" {
			reasons = "unknown"
		}
		receipt += separator + themeFg(activeCLITheme.warn, "cache prefix changed: "+reasons)
	}
	return receipt
}

// primaryStatusLine renders the interaction half of the first footer row. The
// model/profile group is laid out separately so it can stay right-anchored on
// wide terminals and move as one unit on narrow terminals.
func (m chatTUI) primaryStatusLine(modeTag string, shellMode, cancelRequested bool) string {
	status := statusFooterIndent + modeTag
	switch {
	case m.rewind != nil:
		status += " · ⟲ rewind"
	case m.mcpImport != nil:
		status += " · MCP import"
	case m.resumePick != nil:
		status += " · " + i18n.M.StatusResumePicker
	case m.quickPick != nil:
		status += " · " + m.quickPick.title
	case m.mcp != nil:
		status += " · MCP"
	case m.skillPick != nil:
		status += " · " + i18n.M.SkillPickerStatusLabel
	case m.chooser != nil:
		status += " · " + i18n.M.ChatStatusQuestion
	case m.pendingApproval != nil && m.pendingApproval.Tool == planApprovalTool:
		status += " · " + i18n.M.ChatStatusPlanApproval
	case m.pendingApproval != nil:
		status += " · " + i18n.M.ChatStatusToolApproval
	case m.clipboardImagePending:
		status += " · " + yellow(i18n.M.ClipboardImagePastingHint)
	case m.copyNoticeText != "":
		status += " · " + green(m.copyNoticeText)
	case cancelRequested:
		status += " · " + i18n.M.CtrlCQuitHint
	case shellMode:
		status += " · " + i18n.M.ShellModeHint
	case m.ctrl != nil && m.ctrl.AutoApproveTools():
	default:
	}
	if mt := m.mouseTag(); mt != "" {
		status += " · " + mt
	}
	return status
}

// statusCompactRight builds the compact right-hand side of the single status
// row: model · effort · work · ↑in ↓out · cache · context · jobs.
func (m chatTUI) statusCompactRight(maxWidth int) string {
	if m.statuslineCmd != "" && m.statuslineOut != "" {
		full := ansi.Strip(m.statuslineOut)
		if visibleWidth(full) <= maxWidth {
			return footerValue(m.statuslineOut)
		}
		return footerHint(compactMiddle(ansi.Strip(full), maxWidth))
	}
	sep := " · "
	var parts []string

	if model := strings.TrimSpace(m.label); model != "" {
		parts = append(parts, footerInfo(model))
	}
	if effort := m.effortLevel; effort != "" {
		if effort != "auto" {
			parts = append(parts, themeStyle(activeCLITheme.info).Bold(true).Render(effort))
		} else {
			parts = append(parts, footerValue(effort))
		}
	}
	if m.runtimeProfile != "" {
		parts = append(parts, footerSecondary(runtimeProfileDisplay(m.runtimeProfile)))
	}
	if m.ctrl != nil {
		prompt, completion := 0, 0
		if u := m.ctrl.LastUsage(); u != nil {
			prompt, completion = u.PromptTokens, u.CompletionTokens
		}
		parts = append(parts, footerValue(fmt.Sprintf("↑%s ↓%s", shortTokens(prompt), shortTokens(completion))))

		body := "0%"
		rate := 0.0
		if b, r, ok := m.cacheStatus(); ok {
			body, rate = b, r
		}
		parts = append(parts, themeFg(cacheStatusColor(rate), body))

		used, window := m.ctrl.ContextSnapshot()
		pct := 0
		if used > 0 && window > 0 {
			pct = used * 100 / window
		}
		ctxColor := activeCLITheme.muted
		switch {
		case pct >= 85:
			ctxColor = activeCLITheme.danger
		case pct >= 60:
			ctxColor = activeCLITheme.warn
		}
		parts = append(parts, themeFg(ctxColor, fmt.Sprintf("%s (%d%%)", shortTokens(used), pct)))
		if jt := m.jobsTag(); jt != "" {
			parts = append(parts, footerInfo(ansi.Strip(jt)))
		}
	}

	if len(parts) == 0 {
		return ""
	}

	full := strings.Join(parts, sep)
	if visibleWidth(full) <= maxWidth {
		return full
	}

	if model := strings.TrimSpace(m.label); model != "" {
		budget := maxWidth
		for _, p := range parts[1:] {
			budget -= visibleWidth(p) + len(sep)
		}
		if budget >= 4 {
			parts[0] = footerInfo(compactMiddle(model, budget))
		}
	}
	full = strings.Join(parts, sep)
	if visibleWidth(full) <= maxWidth {
		return full
	}
	return footerHint(compactMiddle(ansi.Strip(full), maxWidth))
}

func cacheStatusColor(rate float64) cliColor {
	switch {
	case rate >= 80:
		return activeCLITheme.success
	case rate >= 50:
		return activeCLITheme.info
	default:
		return activeCLITheme.warn
	}
}

func renderContextStatusGroups(used, window int, ratio float64) []string {
	if used == 0 || window == 0 {
		return nil
	}
	pct := used * 100 / window
	ctxValue := fmt.Sprintf("%s (%d%%)", shortTokens(used), pct)

	if ratio <= 0 || ratio >= 1 {
		ctxValue = fmt.Sprintf("%s / %s (%d%%)", shortTokens(used), shortTokens(window), pct)
		color := activeCLITheme.muted
		switch {
		case pct >= 85:
			color = activeCLITheme.danger
		case pct >= 60:
			color = activeCLITheme.warn
		}
		return []string{footerMetric(i18n.M.ChatStatusContextLabel, themeFg(color, ctxValue))}
	}

	threshold := int(ratio * 100)
	left := max(threshold-pct, 0)
	ctxColor := activeCLITheme.muted
	compactColor := activeCLITheme.muted
	switch {
	case pct >= threshold:
		// Preserve two levels of urgency from the selected design: context is a
		// warning, while the exhausted compaction headroom is the actual danger.
		ctxColor = activeCLITheme.warn
		compactColor = activeCLITheme.danger
	case left <= 10:
		ctxColor = activeCLITheme.warn
		compactColor = activeCLITheme.warn
	}
	return []string{
		footerMetric(i18n.M.ChatStatusContextLabel, themeFg(ctxColor, ctxValue)),
		footerMetric(i18n.M.ChatStatusCompactLabel, themeFg(compactColor, fmt.Sprintf("%d%%", left))),
	}
}

// statusTelemetryGroups returns independently placeable session metrics. Git is
// intentionally excluded because it owns the flexible identity slot; keeping
// metrics separate lets narrow layouts wrap only between semantic groups.
func (m chatTUI) statusTelemetryGroups() []string {
	if m.statuslineCmd != "" && m.statuslineOut != "" {
		return []string{m.statuslineOut}
	}
	var data []string
	if m.ctrl != nil {
		if body, rate, ok := m.cacheStatus(); ok {
			data = append(data, footerMetric(i18n.M.ChatStatusCacheLabel, themeFg(cacheStatusColor(rate), body)))
		}
		used, window := m.ctrl.ContextSnapshot()
		data = append(data, renderContextStatusGroups(used, window, m.ctrl.CompactRatio())...)
		if jt := m.jobsTag(); jt != "" {
			data = append(data, footerMetric(i18n.M.ChatStatusJobsLabel, footerInfo(ansi.Strip(jt))))
		}
	}
	return data
}

// renderStatusBlock owns the complete persistent footer layout. The optional
// data band is separated from interaction state when Git or telemetry exists;
// narrow screens add deliberate left-aligned rows only between semantic groups.
func (m chatTUI) renderStatusBlock(primary string, width int) string {
	if width <= 0 {
		width = 1
	}
	right := m.statusCompactRight(max(width-visibleWidth(statusFooterIndent), 1))
	return layoutStatusSides(primary, right, width)
}

// hideStatusHintWhenKeyNamesCannotFit keeps the readable Shift+Tab/Ctrl+Y
// spelling on normal terminals without hard-wrapping a single shortcut on an
// extremely narrow terminal. In that case the idle state remains visible and
// the optional shortcut help yields space to the composer.
func hideStatusHintWhenKeyNamesCannotFit(primary string, width int) string {
	hint := i18n.M.ChatStatusCycleHintCompact
	for _, group := range strings.Split(hint, " · ") {
		if visibleWidth(statusFooterIndent+group) > width {
			return strings.Replace(primary, " · "+footerHint(hint), "", 1)
		}
	}
	return primary
}

func statusFooterDivider(width int) string {
	width = max(width, 1)
	if width <= visibleWidth(statusFooterIndent) {
		return themeFg(activeCLITheme.border, strings.Repeat("─", width))
	}
	ruleWidth := width - visibleWidth(statusFooterIndent)
	return statusFooterIndent + themeFg(activeCLITheme.border, strings.Repeat("─", ruleWidth))
}

func layoutStatusSides(left, right string, width int) string {
	switch {
	case right == "":
		return wrapStatusGroups(left, width)
	case left == "":
		return rightAlignStatusGroup(right, width)
	}
	leftWidth := visibleWidth(left)
	rightWidth := visibleWidth(right)
	if leftWidth+statusFooterGroupGap+rightWidth <= width {
		return left + strings.Repeat(" ", width-leftWidth-rightWidth) + right
	}
	// Once the two semantic halves no longer fit, switch layout deliberately:
	// interaction groups wrap only at their separators, while model/work owns a
	// new left-aligned row. This avoids the floating right-side orphan seen when
	// a terminal crosses the medium-width breakpoint.
	return wrapStatusGroups(left, width) + "\n" + statusFooterIndent + right
}

func wrapStatusGroups(line string, width int) string {
	if width <= 0 || line == "" || visibleWidth(line) <= width {
		return line
	}
	groups := strings.Split(line, " · ")
	if len(groups) < 2 {
		return wrapStatusLine(line, width)
	}

	var rows []string
	current := groups[0]
	for _, group := range groups[1:] {
		candidate := current + " · " + group
		if visibleWidth(candidate) <= width {
			current = candidate
			continue
		}
		rows = append(rows, wrapStatusLine(current, width))
		current = statusFooterIndent + group
	}
	rows = append(rows, wrapStatusLine(current, width))
	return strings.Join(rows, "\n")
}

func rightAlignStatusGroup(group string, width int) string {
	if group == "" {
		return ""
	}
	if visibleWidth(group) <= width {
		return strings.Repeat(" ", width-visibleWidth(group)) + group
	}
	return wrapStatusLine(group, width)
}

func (m chatTUI) layoutGitTelemetry(width int) string {
	telemetryGroups := m.statusTelemetryGroups()
	telemetry := strings.Join(telemetryGroups, "  ")
	hasGit := strings.TrimSpace(m.gitStatus.Repo) != "" && strings.TrimSpace(m.gitStatus.Branch) != ""
	if !hasGit {
		// Without a Git identity there is no left-hand peer to balance. Keep the
		// telemetry anchored to the normal footer indent instead of leaving a
		// repo-sized visual hole across most of a wide terminal.
		return packStatusGroups(telemetryGroups, width)
	}

	fullGitBudget := max(width-visibleWidth(statusFooterIndent), 1)
	git := m.gitStatus.RenderWithin(fullGitBudget, activeCLITheme.warn)
	gitLine := statusFooterIndent + git
	if telemetry == "" {
		return gitLine
	}

	telemetryWidth := visibleWidth(telemetry)
	if visibleWidth(gitLine)+statusFooterGroupGap+telemetryWidth <= width {
		return gitLine + strings.Repeat(" ", width-visibleWidth(gitLine)-telemetryWidth) + telemetry
	}

	// Under width pressure Git gets its own full row instead of being shortened
	// merely to keep telemetry beside it. Telemetry then packs left-to-right by
	// semantic group, so no right-aligned fragment floats on a continuation row.
	return gitLine + "\n" + packStatusGroups(telemetryGroups, width)
}

func packStatusGroups(groups []string, width int) string {
	width = max(width, 1)
	if len(groups) == 0 {
		return ""
	}
	indent := statusFooterIndent
	if width <= visibleWidth(indent) {
		indent = ""
	}

	var rows []string
	current := indent
	for _, group := range groups {
		if strings.TrimSpace(ansi.Strip(group)) == "" {
			continue
		}
		candidate := current + group
		if strings.TrimSpace(ansi.Strip(current)) != "" {
			candidate = current + "  " + group
		}
		if visibleWidth(candidate) <= width {
			current = candidate
			continue
		}
		if strings.TrimSpace(ansi.Strip(current)) != "" {
			rows = append(rows, current)
		}
		current = indent + group
		if visibleWidth(current) > width {
			rows = append(rows, wrapStatusLine(current, width))
			current = indent
		}
	}
	if strings.TrimSpace(ansi.Strip(current)) != "" {
		rows = append(rows, current)
	}
	return strings.Join(rows, "\n")
}
