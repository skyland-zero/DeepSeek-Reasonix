package cli

import (
	"strings"

	"reasonix/internal/branding"
	"reasonix/internal/i18n"
)

const (
	// bannerCardMinWidth is the narrowest transcript width that still shows
	// the codex-style session header card; below it the banner falls back to
	// the one-line form.
	bannerCardMinWidth = 44
	// bannerCardMaxInner caps the card's inner width like codex's
	// SESSION_HEADER_MAX_INNER_WIDTH, so very wide terminals keep a readable
	// header instead of a full-width box.
	bannerCardMaxInner = 56
)

// cliBuildVersion carries the -ldflags injected version string for the
// banner title. It is a package-level var (not a parameter) so the many
// newChatTUI test call sites stay unchanged.
var cliBuildVersion = "dev"

// bannerCard renders the dim-bordered session header card (title, subtitle,
// model, workspace) clipped to width. Content lines keep their own colours
// and truncate to the inner width so the card never overflows.
func bannerCard(subtitle, model, workspace, version string, width int) string {
	inner := bannerCardMaxInner
	// total card width = inner + 2 (border columns); content area = inner - 2
	// (one padding space each side), so every line is inner+2 wide like the
	// borders and stays below the terminal width instead of hugging it.
	if w := width - 4; w < inner {
		inner = max(4, w)
	}

	var lines []string
	if inner >= treeIslandMinInner {
		art := floatingTreeIslandArt()
		pad := max(0, (inner-2-treeIslandArtWidth)/2)
		for _, artLine := range strings.Split(art, "\n") {
			lines = append(lines, strings.Repeat(" ", pad)+artLine)
		}
	}
	title := dim(">_ ") + bold("skycode") + dim(" (v"+version+")")
	lines = append(lines, title)
	if subtitle != "" {
		lines = append(lines, subtitle)
	}
	if model != "" {
		lines = append(lines, dim("model")+"  "+truncateMiddle(model, inner-9))
	}
	if workspace != "" {
		lines = append(lines, dim("dir")+"  "+truncateMiddle(workspace, inner-8))
	}

	var b strings.Builder
	bar := strings.Repeat("─", inner)
	b.WriteString(dim("╭" + bar + "╮"))
	b.WriteByte('\n')
	for _, line := range lines {
		content := compactEnd(line, inner-2)
		b.WriteString(dim("│"))
		b.WriteByte(' ')
		b.WriteString(content)
		b.WriteString(strings.Repeat(" ", inner-2-visibleWidth(content)))
		b.WriteByte(' ')
		b.WriteString(dim("│"))
		b.WriteByte('\n')
	}
	b.WriteString(dim("╰" + bar + "╯"))
	return b.String()
}

// truncateMiddle keeps the head and tail of a long value with a middle
// ellipsis (codex-style path shortening). Returns s unchanged when it fits.
func truncateMiddle(s string, maxWidth int) string {
	if maxWidth <= 0 || visibleWidth(s) <= maxWidth {
		return s
	}
	if maxWidth <= 3 {
		return compactEnd(s, maxWidth)
	}
	keep := maxWidth - 1
	leftWidth := keep / 2
	rightWidth := keep - leftWidth
	left := takeLeftWidth(s, leftWidth)
	right := takeRightWidth(s, rightWidth)
	return left + "…" + right
}

// renderTUIBanner is the title card + tip + optional missing-key warning
// printed once at the top of the session. Wide terminals get the codex-style
// dim-bordered session header card; narrow ones keep the compact one-line form.
func (m *chatTUI) renderTUIBanner(label, missing string, width int) string {
	var b strings.Builder
	if width >= bannerCardMinWidth {
		workspace := ""
		if m.ctrl != nil {
			workspace = m.ctrl.WorkspaceRoot()
		}
		b.WriteString(bannerCard(i18n.M.Subtitle, label, workspace, cliBuildVersion, width))
		b.WriteByte('\n')
	} else {
		b.WriteString(accent("◆") + " " + bold(branding.BinaryName) + "  " + dim("· "+label) + "\n")
	}
	b.WriteString(dim("  "+i18n.M.ChatTip) + "\n")
	if missing != "" {
		b.WriteString(wrapForViewport("  ! "+missing, width, activeCLITheme.warn) + "\n")
	}
	return b.String()
}
