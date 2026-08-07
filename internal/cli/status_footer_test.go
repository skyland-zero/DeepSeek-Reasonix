package cli

import (
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"

	"github.com/charmbracelet/x/ansi"

	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/i18n"
	"reasonix/internal/provider"
)

func TestTurnReceiptKeepsCompletePerTurnBreakdown(t *testing.T) {
	defer restoreThemeForTest(activeColorProfile, activeCLITheme)
	defer i18n.DetectLanguage("en")
	activeColorProfile = colorprofile.NoTTY
	configureCLITheme("dark")
	i18n.DetectLanguage("zh")

	u := &provider.Usage{
		PromptTokens:     13_625,
		CompletionTokens: 392,
		TotalTokens:      14_017,
		CacheHitTokens:   13_184,
		CacheMissTokens:  441,
		ReasoningTokens:  24,
	}
	p := &provider.Pricing{CacheHit: .1, Input: 1, Output: 2}
	got := renderTurnReceipt(u, p, nil)
	for _, want := range []string{
		"本轮", "14.0K tok", "in 13.6K", "cached 13.2K", "new 441",
		"out 392", "reasoning 24", "¥0.0025",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("turn receipt %q missing %q", got, want)
		}
	}
	if strings.Contains(got, "\033[") {
		t.Fatalf("NO_COLOR turn receipt contains escapes: %q", got)
	}
}

func TestTurnReceiptFallsBackToDerivedFreshTokensAndWrapsCleanly(t *testing.T) {
	defer restoreThemeForTest(activeColorProfile, activeCLITheme)
	defer i18n.DetectLanguage("en")
	activeColorProfile = colorprofile.ANSI256
	configureCLITheme("dark")
	i18n.DetectLanguage("en")

	got := renderTurnReceipt(&provider.Usage{
		PromptTokens: 1_200, CompletionTokens: 80, TotalTokens: 1_280, CacheHitTokens: 900,
	}, nil, &event.CacheDiagnostics{PrefixChanged: true, PrefixChangeReasons: []string{"tools"}})
	plain := ansi.Strip(got)
	for _, want := range []string{"TURN", "cached 900", "new 300", "cache prefix changed: tools"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("turn receipt %q missing %q", plain, want)
		}
	}
	for i, line := range strings.Split(wrapTranscript(got, 32), "\n") {
		if width := visibleWidth(line); width > 32 {
			t.Fatalf("wrapped turn receipt row %d width = %d, want <= 32: %q", i, width, line)
		}
	}
}

func TestTurnReceiptIgnoresEmptyUsage(t *testing.T) {
	if got := renderTurnReceipt(nil, nil, nil); got != "" {
		t.Fatalf("nil usage receipt = %q, want empty", got)
	}
	if got := renderTurnReceipt(&provider.Usage{}, nil, nil); got != "" {
		t.Fatalf("empty usage receipt = %q, want empty", got)
	}
}

func TestTurnReceiptBandUsesSingleQuietBoundary(t *testing.T) {
	defer restoreThemeForTest(activeColorProfile, activeCLITheme)
	activeColorProfile = colorprofile.NoTTY
	configureCLITheme("dark")

	band := renderTurnReceiptBand("  TURN  14.0K tok · in 13.6K", 48)
	if len(strings.Split(band, "\n")) != 1 {
		t.Fatalf("turn receipt band should be one line, got %d:\n%s", len(strings.Split(band, "\n")), band)
	}
	if !strings.Contains(band, "TURN  14.0K tok") {
		t.Fatalf("receipt body missing from band:\n%s", band)
	}
	plain := ansi.Strip(band)
	if !strings.HasPrefix(plain, "  ──────") {
		t.Fatalf("band should start with indent + 6 dashes: %q", plain)
	}
}

func TestTurnReceiptAdaptsContrastAcrossThemes(t *testing.T) {
	defer restoreThemeForTest(activeColorProfile, activeCLITheme)
	defer i18n.DetectLanguage("en")
	activeColorProfile = colorprofile.ANSI256
	i18n.DetectLanguage("en")

	for _, tt := range []struct {
		mode, labelSGR, valueSGR string
	}{
		{mode: "dark", labelSGR: "\033[38;5;248m", valueSGR: "\033[38;5;251m"},
		{mode: "light", labelSGR: "\033[38;5;241m", valueSGR: "\033[38;5;239m"},
	} {
		t.Run(tt.mode, func(t *testing.T) {
			configureCLITheme(tt.mode)
			receipt := renderTurnReceipt(&provider.Usage{
				PromptTokens: 900, CompletionTokens: 100, TotalTokens: 1_000,
			}, nil, nil)
			band := renderTurnReceiptBand(receipt, 80)
			for _, want := range []string{tt.labelSGR + "─", tt.labelSGR + "TURN", tt.valueSGR + "1.0K tok"} {
				if !strings.Contains(band, want) {
					t.Fatalf("%s receipt %q missing semantic style %q", tt.mode, band, want)
				}
			}
			if strings.Count(ansi.Strip(band), "\n") != 0 {
				t.Fatalf("%s receipt should be one line: %q", tt.mode, ansi.Strip(band))
			}
		})
	}
}

func TestStatusFooterSemanticPaletteAcrossThemes(t *testing.T) {
	t.Setenv("SKYCODE_THEME", "")
	t.Setenv("SKYCODE_THEME_STYLE", "")
	defer restoreThemeForTest(activeColorProfile, activeCLITheme)
	activeColorProfile = colorprofile.ANSI256

	for _, tt := range []struct {
		mode, labelSGR, valueSGR, infoSGR, secondarySGR string
	}{
		{mode: "dark", labelSGR: "\033[38;5;248m", valueSGR: "\033[38;5;251m", infoSGR: "\033[38;5;80m", secondarySGR: "\033[38;5;141m"},
		{mode: "light", labelSGR: "\033[38;5;241m", valueSGR: "\033[38;5;239m", infoSGR: "\033[38;5;25m", secondarySGR: "\033[38;5;104m"},
	} {
		t.Run(tt.mode, func(t *testing.T) {
			configureCLITheme(tt.mode)
			m := newTestChatTUI()
			m.label = "deepseek-v4-flash"
			m.effortLevel = "auto"
			m.runtimeProfile = "full"
			got := m.statusCompactRight(80)
			for _, want := range []string{
				tt.infoSGR + "deepseek-v4-flash",
				tt.secondarySGR + "balanced",
			} {
				if !strings.Contains(got, want) {
					t.Fatalf("compact right group %q missing semantic style %q", got, want)
				}
			}
			primary := m.primaryStatusLine(" Auto ", false, false)
			if primary != ansi.Strip(primary) {
				t.Fatalf("%s primary status line should be plain text: %q", tt.mode, primary)
			}
		})
	}
}

func TestStatusFooterThemesKeepIdenticalGeometry(t *testing.T) {
	defer restoreThemeForTest(activeColorProfile, activeCLITheme)

	m := newTestChatTUI()
	m.ctrl = control.New(control.Options{})
	m.label = "deepseek-v4-flash"
	m.effortLevel = "max"
	m.runtimeProfile = "full"
	m.balance = "¥12.34"
	m.gitStatus = gitStatus{Repo: "DeepSeek-Reasonix", Branch: "feature/theme-footer", Added: 3}

	render := func(mode string, profile colorprofile.Profile) string {
		activeColorProfile = profile
		configureCLITheme(mode)
		primary := m.primaryStatusLine(" Auto ", false, false)
		return ansi.Strip(m.renderStatusBlock(primary, 132))
	}
	dark := render("dark", colorprofile.ANSI256)
	light := render("light", colorprofile.ANSI256)
	plain := render("dark", colorprofile.NoTTY)
	if dark != light || dark != plain {
		t.Fatalf("theme modes changed footer geometry:\ndark:\n%s\nlight:\n%s\nplain:\n%s", dark, light, plain)
	}
}

func TestStatusFooterGitAndDividerAdaptToTheme(t *testing.T) {
	defer restoreThemeForTest(activeColorProfile, activeCLITheme)
	activeColorProfile = colorprofile.ANSI256

	for _, tt := range []struct {
		mode, gitSGR, borderSGR string
	}{
		{mode: "dark", gitSGR: "\033[38;5;179m", borderSGR: "\033[38;5;237m"},
		{mode: "light", gitSGR: "\033[38;5;136m", borderSGR: "\033[38;5;254m"},
	} {
		t.Run(tt.mode, func(t *testing.T) {
			configureCLITheme(tt.mode)
			m := newTestChatTUI()
			m.gitStatus = gitStatus{Repo: "DeepSeek-Reasonix", Branch: "db4be5e6", Detached: true}
			git := m.layoutGitTelemetry(80)
			if !strings.Contains(git, tt.gitSGR+"DeepSeek-Reasonix") {
				t.Fatalf("%s Git identity should use warm semantic colour: %q", tt.mode, git)
			}
			divider := statusFooterDivider(40)
			if !strings.Contains(divider, tt.borderSGR) || visibleWidth(divider) != 40 {
				t.Fatalf("%s divider should use border token at full width: %q", tt.mode, divider)
			}
		})
	}
}

func TestContextFooterColorsOnlyValuesByUrgency(t *testing.T) {
	defer restoreThemeForTest(activeColorProfile, activeCLITheme)
	activeColorProfile = colorprofile.ANSI256
	configureCLITheme("dark")

	normal := strings.Join(renderContextStatusGroups(10, 100, .8), " ")
	if !strings.Contains(normal, "\033[38;5;248mCTX") || !strings.Contains(normal, "\033[38;5;251m10 (10%)") {
		t.Fatalf("normal context should use subtle label and neutral value: %q", normal)
	}

	warning := strings.Join(renderContextStatusGroups(75, 100, .8), " ")
	if !strings.Contains(warning, "\033[38;5;248mCOMPACT") || !strings.Contains(warning, "\033[38;5;179m5%") {
		t.Fatalf("near-threshold context should warn only on values: %q", warning)
	}

	critical := strings.Join(renderContextStatusGroups(80, 100, .8), " ")
	if !strings.Contains(critical, "\033[38;5;179m80 (80%)") || !strings.Contains(critical, "\033[38;5;167m0%") {
		t.Fatalf("critical context should keep warning/danger hierarchy: %q", critical)
	}
}

func TestStatusFooterNoColorKeepsSemanticLabels(t *testing.T) {
	defer restoreThemeForTest(activeColorProfile, activeCLITheme)
	activeColorProfile = colorprofile.NoTTY
	configureCLITheme("dark")

	m := newTestChatTUI()
	m.label = "deepseek-v4-flash"
	m.effortLevel = "auto"
	m.runtimeProfile = "full"
	m.balance = "¥12.34"
	block := m.renderStatusBlock(m.primaryStatusLine(" Auto ", false, false), 120)
	if strings.Contains(block, "\033[") {
		t.Fatalf("NO_COLOR footer contains escapes: %q", block)
	}
	for _, want := range []string{"deepseek-v4-flash", "auto", "balanced"} {
		if !strings.Contains(block, want) {
			t.Fatalf("NO_COLOR footer missing %q:\n%s", want, block)
		}
	}
}

func TestStatusFooterUsesReadableLocalizedHintAndWrapsCleanly(t *testing.T) {
	defer i18n.DetectLanguage("en")
	for _, tt := range []struct {
		lang, session string
	}{
		{lang: "en", session: "deepseek-v4-flash · auto · balanced"},
		{lang: "zh", session: "deepseek-v4-flash · auto · 均衡"},
		{lang: "zh-TW", session: "deepseek-v4-flash · auto · 均衡"},
	} {
		t.Run(tt.lang, func(t *testing.T) {
			i18n.DetectLanguage(tt.lang)
			m := newTestChatTUI()
			m.ctrl = control.New(control.Options{})
			m.label = "deepseek-v4-flash"
			m.runtimeProfile = "full"
			m.effortLevel = "auto"

			primary := m.primaryStatusLine(" Auto ", false, false)
			block := ansi.Strip(m.renderStatusBlock(primary, 80))
			lines := strings.Split(block, "\n")
			if len(lines) != 1 {
				t.Fatalf("localized footer rows = %d, want single line:\n%s", len(lines), block)
			}
			if !strings.Contains(lines[0], tt.session) {
				t.Fatalf("localized footer missing session groups:\n%s", block)
			}
		})
	}
}

func TestStatusFooterLocalizesMetricLabelsAndKeepsNarrowRows(t *testing.T) {
	defer i18n.DetectLanguage("en")
	for _, tt := range []struct {
		lang      string
		session   string
		telemetry []string
	}{
		{
			lang:      "zh",
			session:   "deepseek-v4-flash · auto · 均衡",
			telemetry: []string{"缓存", "上下文", "压缩", "任务"},
		},
		{
			lang:      "zh-TW",
			session:   "deepseek-v4-flash · auto · 均衡",
			telemetry: []string{"快取", "上下文", "壓縮", "任務"},
		},
	} {
		t.Run(tt.lang, func(t *testing.T) {
			i18n.DetectLanguage(tt.lang)
			m := newTestChatTUI()
			m.label = "deepseek-v4-flash"
			m.effortLevel = "auto"
			m.runtimeProfile = "full"
			if got := ansi.Strip(m.statusCompactRight(80)); got != tt.session {
				t.Fatalf("localized session metrics = %q, want %q", got, tt.session)
			}

			groups := []string{
				footerMetric(i18n.M.ChatStatusCacheLabel, footerValue("90%")),
			}
			groups = append(groups, renderContextStatusGroups(75, 100, .8)...)
			groups = append(groups,
				footerMetric(i18n.M.ChatStatusJobsLabel, footerInfo("2")),
			)
			packed := ansi.Strip(packStatusGroups(groups, 22))
			for _, label := range tt.telemetry {
				if !strings.Contains(packed, label+" ") {
					t.Fatalf("localized telemetry missing %q:\n%s", label, packed)
				}
			}
			for row, line := range strings.Split(packed, "\n") {
				if width := visibleWidth(line); width > 22 {
					t.Fatalf("localized telemetry row %d width = %d, want <= 22: %q", row, width, line)
				}
			}
		})
	}
}

func TestStatusFooterSwapsModelAndGitGroups(t *testing.T) {
	i18n.DetectLanguage("en")

	m := newTestChatTUI()
	m.ctrl = control.New(control.Options{})
	m.label = "deepseek-v4-flash"
	m.runtimeProfile = "full"
	m.effortLevel = "auto"
	m.balance = "¥12.34"
	m.gitStatus = gitStatus{
		Repo:      "DeepSeek-Reasonix",
		Branch:    "feature/responsive-footer",
		Added:     1199,
		Removed:   244,
		Untracked: 3,
	}

	primary := m.primaryStatusLine(" Auto ", false, false)
	lines := strings.Split(ansi.Strip(m.renderStatusBlock(primary, 160)), "\n")
	if len(lines) != 1 {
		t.Fatalf("wide status block lines = %d, want single row:\n%s", len(lines), strings.Join(lines, "\n"))
	}
	if !strings.Contains(lines[0], "deepseek-v4-flash · auto · balanced") {
		t.Fatalf("compact right should contain session info:\n%s", strings.Join(lines, "\n"))
	}
	if got := visibleWidth(lines[0]); got > 160 {
		t.Fatalf("row width = %d, want <= 160: %q", got, lines[0])
	}
}

func TestStatusFooterWithoutGitLeftAlignsTelemetry(t *testing.T) {
	defer i18n.DetectLanguage("en")
	i18n.DetectLanguage("en")

	m := newTestChatTUI()
	m.ctrl = control.New(control.Options{})
	m.label = "deepseek-v4-flash"
	m.effortLevel = "max"

	right := ansi.Strip(m.statusCompactRight(120))
	if !strings.Contains(right, "deepseek-v4-flash") || !strings.Contains(right, "max") {
		t.Fatalf("compact right should contain model and effort, got %q", right)
	}
	if got := visibleWidth(right); got >= 120 {
		t.Fatalf("compact right width = %d, want < 120: %q", got, right)
	}
}

func TestStatusFooterOmitsEmptyDataBand(t *testing.T) {
	m := newTestChatTUI()
	primary := "  Auto "
	block := ansi.Strip(m.renderStatusBlock(primary, 120))
	if block != primary {
		t.Fatalf("empty Git/telemetry status block = %q, want only %q", block, primary)
	}
	if strings.Contains(block, "─") {
		t.Fatalf("empty Git/telemetry status block retained a divider: %q", block)
	}
}

func TestStatusFooterMediumLayoutLeftAlignsModelWork(t *testing.T) {
	i18n.DetectLanguage("en")

	m := newTestChatTUI()
	m.ctrl = control.New(control.Options{})
	m.label = "deepseek-v4-flash"
	m.runtimeProfile = "full"
	m.effortLevel = "auto"

	primary := m.primaryStatusLine(" Auto ", false, false)
	lines := strings.Split(ansi.Strip(m.renderStatusBlock(primary, 45)), "\n")
	if len(lines) < 2 {
		t.Fatalf("medium footer rows = %d, want wrapping at narrow width:\n%s", len(lines), strings.Join(lines, "\n"))
	}
	if !strings.HasPrefix(lines[1], statusFooterIndent+"deepseek-v4-flash") {
		t.Fatalf("model/work row should wrap to second line, got %q:\n%s", lines[1], strings.Join(lines, "\n"))
	}
}

func TestStatusFooterStacksGitAndTelemetryWithoutFloatingContinuation(t *testing.T) {
	i18n.DetectLanguage("en")

	m := newTestChatTUI()
	m.ctrl = control.New(control.Options{})
	m.gitStatus = gitStatus{
		Repo: "DeepSeek-Reasonix", Branch: "feature/responsive-footer", Added: 20, Removed: 4,
	}

	lines := strings.Split(ansi.Strip(m.layoutGitTelemetry(56)), "\n")
	if len(lines) != 1 {
		t.Fatalf("Git-only rows = %d, want 1:\n%s", len(lines), strings.Join(lines, "\n"))
	}
	if !strings.HasPrefix(lines[0], statusFooterIndent+"DeepSeek-Reasonix@") || !strings.Contains(lines[0], "+20 -4") {
		t.Fatalf("Git should own the complete row:\n%s", strings.Join(lines, "\n"))
	}
}

func TestStatusFooterNarrowLayoutBreaksBetweenGroups(t *testing.T) {
	i18n.DetectLanguage("en")

	m := newTestChatTUI()
	m.ctrl = control.New(control.Options{})
	m.label = "provider/" + strings.Repeat("long-model-", 8)
	m.runtimeProfile = "delivery"

	primary := m.primaryStatusLine(" Auto ", false, false)
	block := ansi.Strip(m.renderStatusBlock(primary, 32))
	lines := strings.Split(block, "\n")
	if len(lines) <= 1 {
		t.Fatalf("narrow status block lines = %d, want semantic wrapping:\n%s", len(lines), block)
	}
	for i, line := range lines {
		if got := visibleWidth(line); got > 32 {
			t.Fatalf("row %d width = %d, want <= 32: %q", i, got, line)
		}
	}
	if !strings.Contains(block, "provider/") {
		t.Fatalf("narrow layout dropped required information:\n%s", block)
	}
}

func TestStatusFooterCustomLineStillReplacesBuiltInData(t *testing.T) {
	i18n.DetectLanguage("en")

	m := newTestChatTUI()
	m.ctrl = control.New(control.Options{})
	m.label = "deepseek-v4-flash"
	m.runtimeProfile = "delivery"
	m.statuslineCmd = "custom-status"
	m.statuslineOut = "custom telemetry"

	primary := m.primaryStatusLine(" Auto ", false, false)
	block := ansi.Strip(m.renderStatusBlock(primary, 120))
	if strings.Contains(block, "deepseek-v4-flash") || strings.Contains(block, "delivery") {
		t.Fatalf("custom statusline should replace built-in data fields:\n%s", block)
	}
}

func TestStatusFooterHeightCountUsesRenderedLayout(t *testing.T) {
	i18n.DetectLanguage("en")

	m := newTestChatTUI()
	m.ctrl = control.New(control.Options{})
	m.width = 34
	m.label = "provider/" + strings.Repeat("long-model-", 6)
	m.runtimeProfile = "delivery"
	m.gitStatus = gitStatus{Repo: "VeryLongWorkspaceName", Branch: strings.Repeat("branch/", 8)}
	m.balance = "¥12.34"

	modeTag := " " + m.modeTagText() + " "
	primary := m.primaryStatusLine(modeTag, false, false)
	want := strings.Count(m.renderStatusBlock(primary, m.width), "\n") + 1
	if got := m.computeStatusLineCount(m.width); got != want {
		t.Fatalf("computed status rows = %d, rendered rows = %d", got, want)
	}
}
