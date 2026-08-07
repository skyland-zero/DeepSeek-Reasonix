package cli

import (
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"

	"reasonix/internal/event"
)

func TestDiffBodyDropsHeadersKeepsLineNumbers(t *testing.T) {
	d := event.FileDiff{Diff: "--- a/x.go\n+++ b/x.go\n@@ -7 +7 @@\n-old\n+new\n", Added: 1, Removed: 1}
	joined := strings.Join(diffBody(d, "x.go", 80, 40), "\n")
	if strings.Contains(joined, "--- a/") || strings.Contains(joined, "+++ b/") || strings.Contains(joined, "@@") {
		t.Fatalf("file/hunk headers should be dropped, got:\n%s", joined)
	}
	for _, want := range []string{"old", "new", "7"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q (code or line number) in:\n%s", want, joined)
		}
	}
}

func TestDiffBodyFolds(t *testing.T) {
	var b strings.Builder
	b.WriteString("--- a/x\n+++ b/x\n@@ -1,8 +1,8 @@\n")
	for i := 0; i < 8; i++ {
		b.WriteString("+line\n")
	}
	body := diffBody(event.FileDiff{Diff: b.String()}, "x", 80, 5)
	if len(body) != 5 {
		t.Fatalf("want 5 rows (4 content + footer), got %d:\n%s", len(body), strings.Join(body, "\n"))
	}
	// 8 rendered add rows minus the 4 kept = 4 folded.
	if !strings.Contains(body[len(body)-1], "4") {
		t.Fatalf("footer should report 4 folded lines, got %q", body[len(body)-1])
	}
}

func TestDiffBodyNoFoldWhenShort(t *testing.T) {
	d := event.FileDiff{Diff: "@@ -1 +1 @@\n+a\n"}
	if got := len(diffBody(d, "x", 80, 40)); got != 1 {
		t.Fatalf("want 1 unfolded row, got %d", got)
	}
}

func TestDiffBlockHeader(t *testing.T) {
	d := event.FileDiff{Diff: "@@ -1 +1 @@\n-a\n+b\n", Added: 1, Removed: 1}
	block := diffBlock("edit_file", `{"path":"pkg/x.go"}`, d, 80, 40)
	if len(block) == 0 || !strings.Contains(block[0], "Update") || !strings.Contains(block[0], "pkg/x.go") {
		t.Fatalf("header should name verb + path, got %q", block[0])
	}
}

func TestDiffBlockNilWithoutDiff(t *testing.T) {
	if diffBlock("write_file", `{"path":"x"}`, event.FileDiff{}, 80, 40) != nil {
		t.Fatal("no diff should yield no block")
	}
}

func TestDiffPath(t *testing.T) {
	if got := diffPath(`{"path":"a/b.go","old_string":"x"}`); got != "a/b.go" {
		t.Fatalf("got %q", got)
	}
	if got := diffPath(`not json`); got != "" {
		t.Fatalf("malformed args should yield empty path, got %q", got)
	}
}

func TestDiffRowKeepsLineNumberNoBackground(t *testing.T) {
	defer func(prev colorprofile.Profile) { activeColorProfile = prev }(activeColorProfile)
	activeColorProfile = colorprofile.ANSI256

	line := diffRow('+', "a + b", "x.go", 40, 12, 3)
	// The row must keep the line-number gutter and the colored sign, but carry
	// no background bar: the sign column alone conveys add/remove.
	if strings.Contains(line, "\033[48") {
		t.Fatalf("diff row must not carry a background bar: %q", line)
	}
	plain := ansi.Strip(line)
	if !strings.Contains(plain, "12") || !strings.Contains(plain, "+ a + b") {
		t.Fatalf("row should keep line number and sign, got %q", line)
	}

	// The longest sign row must land exactly at width — no background bar to
	// absorb an overflow, so an off-by-one would wrap and misalign the block.
	long := diffRow('+', strings.Repeat("x", 60), "x.go", 40, 12, 3)
	if ansi.StringWidth(long) > 40 {
		t.Fatalf("sign row overflows width: %d > 40: %q", ansi.StringWidth(long), long)
	}
}
