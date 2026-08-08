package cli

import (
	"strings"
	"testing"

	"reasonix/internal/i18n"
)

func testBannerTUI() chatTUI {
	return chatTUI{label: "deepseek-v4-flash"}
}

func TestBannerCardShape(t *testing.T) {
	// width 44 -> inner 40 < treeIslandMinInner: no art, pure info card
	out := bannerCard(i18n.M.Subtitle, "deepseek-v4-flash", "", "3c3ef171", 44)
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	// top border, title, subtitle, model, bottom border
	if len(lines) != 5 {
		t.Fatalf("card shape wrong (want 5 lines, got %d):\n%q", len(lines), out)
	}
	if !strings.HasPrefix(ansiStrip(lines[0]), "╭") || !strings.HasSuffix(ansiStrip(lines[0]), "╮") {
		t.Fatalf("top border missing:\n%q", lines[0])
	}
	if !strings.HasPrefix(ansiStrip(lines[4]), "╰") || !strings.HasSuffix(ansiStrip(lines[4]), "╯") {
		t.Fatalf("bottom border missing:\n%q", lines[4])
	}
	title := ansiStrip(lines[1])
	if !strings.Contains(title, "skycode") || !strings.Contains(title, "v3c3ef171") {
		t.Fatalf("title line missing brand or version:\n%q", title)
	}
	if !strings.Contains(ansiStrip(lines[2]), i18n.M.Subtitle) {
		t.Fatalf("subtitle line missing subtitle:\n%q", lines[2])
	}
	model := ansiStrip(lines[3])
	if !strings.Contains(model, "model") || !strings.Contains(model, "deepseek-v4-flash") {
		t.Fatalf("model line missing label or value:\n%q", lines[3])
	}
	for i, line := range lines {
		if w := visibleWidth(ansiStrip(line)); w > 44 {
			t.Fatalf("card line %d exceeds width 44 (%d):\n%q", i, w, line)
		}
	}
}

func TestBannerCardWorkspaceLine(t *testing.T) {
	out := bannerCard("subtitle", "model-x", "C:/path/to/workspace", "dev", 60)
	if !strings.Contains(ansiStrip(out), "workspace") || !strings.Contains(ansiStrip(out), "dir") {
		t.Fatalf("workspace line missing:\n%q", ansiStrip(out))
	}
}

func TestBannerCardInnerWidthClamp(t *testing.T) {
	out := bannerCard("subtitle", "model-x", "", "dev", 400)
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		w := visibleWidth(ansiStrip(line))
		if w > bannerCardMaxInner+2 {
			t.Fatalf("card should clamp to inner width %d, got %d:\n%q", bannerCardMaxInner, w, line)
		}
	}
}

// TestBannerCardUniformWidth guards the border geometry: content lines must
// match the border width exactly and stay below the terminal width, else the
// right border lands on the last column and autowrap eats it.
func TestBannerCardUniformWidth(t *testing.T) {
	for _, width := range []int{44, 60, 80, 400} {
		out := bannerCard(i18n.M.Subtitle, "deepseek-v4-flash", `d:\workspace`, "dev", width)
		lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
		want := visibleWidth(ansiStrip(lines[0]))
		for i, line := range lines {
			w := visibleWidth(ansiStrip(line))
			if w != want {
				t.Fatalf("width %d: line %d (%d) differs from border width %d:\n%q", width, i, w, want, line)
			}
			if w >= width {
				t.Fatalf("width %d: line %d fills the terminal (%d):\n%q", width, i, w, line)
			}
		}
	}
}

func TestBannerCardLongValueTruncated(t *testing.T) {
	long := strings.Repeat("x", 200)
	out := bannerCard("", long, "", "dev", 44)
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if w := visibleWidth(ansiStrip(line)); w > 44 {
			t.Fatalf("truncated card line exceeds width 44 (%d):\n%q", w, line)
		}
	}
	if !strings.Contains(ansiStrip(out), "…") {
		t.Fatalf("long value should be middle-truncated:\n%q", ansiStrip(out))
	}
}

func TestRenderTUIBannerWide(t *testing.T) {
	m := testBannerTUI()
	out := m.renderTUIBanner("deepseek-v4-flash", "", 80)
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	// 12 art rows + title + subtitle + model + 2 borders + tip
	if len(lines) != 18 {
		t.Fatalf("banner shape wrong (want 18 lines, got %d):\n%q", len(lines), out)
	}
	if !strings.HasPrefix(ansiStrip(lines[0]), "╭") {
		t.Fatalf("wide banner should start with the card:\n%q", lines[0])
	}
	if !strings.Contains(ansiStrip(lines[1]), "▀") {
		t.Fatalf("card should start with the tree-island art:\n%q", lines[1])
	}
	if !strings.Contains(ansiStrip(out), "█") {
		t.Fatalf("card art should contain island blocks:\n%q", ansiStrip(out))
	}
	found := false
	for _, line := range lines {
		if strings.Contains(ansiStrip(line), "deepseek-v4-flash") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("model line missing in banner:\n%q", out)
	}
	if !strings.Contains(lines[len(lines)-1], i18n.M.ChatTip) {
		t.Fatalf("tip line missing ChatTip:\n%q", lines[len(lines)-1])
	}
	for i, line := range lines {
		if w := visibleWidth(line); w > 80 {
			t.Fatalf("banner line %d exceeds width 80 (%d):\n%q", i, w, line)
		}
	}
}

func TestFloatingTreeIslandArt(t *testing.T) {
	art := floatingTreeIslandArt()
	lines := strings.Split(art, "\n")
	if len(lines) != len(treeIslandRows) {
		t.Fatalf("art should have %d rows, got %d", len(treeIslandRows), len(lines))
	}
	for i, line := range lines {
		if w := visibleWidth(ansiStrip(line)); w > treeIslandArtWidth {
			t.Fatalf("art row %d exceeds width %d (%d):\n%q", i, treeIslandArtWidth, w, line)
		}
	}
	if !strings.Contains(art, "~") || !strings.Contains(art, "▀") || !strings.Contains(art, "█") {
		t.Fatalf("art should contain clouds and island glyphs:\n%q", art)
	}
}

func TestBannerCardSkipsArtWhenNarrow(t *testing.T) {
	out := bannerCard("subtitle", "model-x", "", "dev", 44)
	if strings.Contains(ansiStrip(out), "▀▀▀") || strings.Contains(ansiStrip(out), "~ ~") {
		t.Fatalf("narrow card should skip the tree-island art:\n%q", ansiStrip(out))
	}
}

func TestRenderTUIBannerNarrow(t *testing.T) {
	m := testBannerTUI()
	out := m.renderTUIBanner("", "", 40)
	if !strings.Contains(out, "◆") || !strings.Contains(out, "skycode") {
		t.Fatalf("narrow banner should fall back to the one-line form:\n%q", out)
	}
	if !strings.Contains(out, i18n.M.ChatTip) {
		t.Fatalf("narrow banner should keep the tip:\n%q", out)
	}
	if strings.Contains(out, "╭") {
		t.Fatalf("narrow banner must not render the card:\n%q", out)
	}
}

func TestRenderTUIBannerMissingKeyWide(t *testing.T) {
	m := testBannerTUI()
	out := m.renderTUIBanner("", "set SKYCODE_API_KEY", 80)
	if !strings.Contains(out, "set SKYCODE_API_KEY") {
		t.Fatalf("wide banner should surface the missing-key warning:\n%q", out)
	}
	if !strings.Contains(out, "╭") {
		t.Fatalf("wide banner should keep the card with a warning present:\n%q", out)
	}
}

func ansiStrip(s string) string {
	var b strings.Builder
	esc := false
	for _, r := range s {
		if r == '\x1b' {
			esc = true
			continue
		}
		if esc {
			if r >= 'A' && r <= 'Z' {
				esc = false
			}
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
