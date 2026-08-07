package cli

import (
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
)

// userPaste is the multi-line pasted content from the field report: mixed
// indent, short lines, and one line long enough to overflow a 120-column
// bubble.
const userPaste = `类别    短文本    长文本
问候    你好    这是一个非常典型的长文本示例,它的内容被刻意拉得很长,目的是用来测试表格在渲染长段落文字时的换行表现,以及阅读者对于冗长叙述的耐心程度。
描述
  下雨了    窗外的雨从午后一直下到深夜,雨点敲打着屋檐和树叶,发出细密而连绵的声响,空气里弥漫着潮湿的泥土气息,整个世界仿佛都被这场雨温柔地包裹了起来。
指令    请稍候
  请您暂时在当前位置停留片刻,不要随意走动,也不要关闭任何正在运行的窗口或程序,因为系统正在进行必要的维护和更新操作,整个过程预计需要几分钟时间。
引用    学无止境
  学习是一条永无止境的道路,它既没有明确的起点,也没有最终的终点,每一个阶段的结束都意味着另一个阶段的开始,而真正的智慧,往往就隐藏在这条漫长道路的每一寸风景之中。
示例    测试
  本单元格内容包含逗号,句号。以及各种标点符号——破折号、分号、冒号,甚至还有括号(圆括号)和[方括号],用来验证它们在 Markdown表格单元格中是否能够被正确、完整地解析和展示。`

// TestWrapFixedContent pins the fixed-container contract: fitting lines pass
// through byte-for-byte (leading whitespace, blank lines, ANSI), while an
// overlong line wraps with its leading whitespace carried onto every
// continuation line — the margin never collapses at a break.
func TestWrapFixedContent(t *testing.T) {
	fit := "  a\nplain\n\n\x1b[2m  styled\x1b[0m"
	if got := wrapFixedContent(fit, 80); got != fit {
		t.Errorf("fitting lines altered:\n got  %q\n want %q", got, fit)
	}

	over := "  " + strings.Repeat("中", 60) // 122 visible columns
	out := wrapFixedContent(over, 30)
	for i, l := range strings.Split(out, "\n") {
		if !strings.HasPrefix(l, "  ") {
			t.Errorf("line %d lost its leading indent: %q", i, l)
		}
		if visibleWidth(l) > 30 {
			t.Errorf("line %d exceeds width 30: %q", i, l)
		}
	}
	if strings.ReplaceAll(out, "\n  ", "") != over {
		t.Errorf("content lost or duplicated:\n got  %q\n want %q", out, over)
	}

	if got := wrapFixedContent("\t中", 80); !strings.HasPrefix(got, "    ") {
		t.Errorf("tab not expanded to a tab stop: %q", got)
	}
	if got := wrapFixedContent("a\nb", 0); got != "a\nb" {
		t.Errorf("width <= 0 must be a no-op: %q", got)
	}
}

// TestRenderUserBubbleFixedMargin is the regression test for the pasted
// multi-line bubble: every line keeps the fixed "› "/"  " margin plus the
// content's own leading whitespace, overlong lines wrap inside the container
// with the indent on each continuation, and no line exceeds the bubble width.
func TestRenderUserBubbleFixedMargin(t *testing.T) {
	defer func(prev colorprofile.Profile) { activeColorProfile = prev }(activeColorProfile)
	activeColorProfile = colorprofile.ANSI256

	const width = 120
	out := renderUserBubble(userPaste, width, false)
	plainLines := strings.Split(ansi.Strip(out), "\n")
	if len(plainLines) == 0 {
		t.Fatal("bubble rendered empty")
	}

	// Fixed left margin on every line; nothing wider than the bubble.
	for i, l := range plainLines {
		if visibleWidth(l) > width {
			t.Errorf("line %d exceeds bubble width %d: %d wide", i, width, visibleWidth(l))
		}
		if i == 0 {
			if !strings.HasPrefix(l, "› ") {
				t.Errorf("first line lost the bubble prefix: %q", l)
			}
		} else if !strings.HasPrefix(l, "  ") {
			t.Errorf("line %d lost the fixed continuation margin: %q", i, l)
		}
	}

	// An indented short line keeps its own leading whitespace inside the
	// bubble (2 margin + 2 content spaces).
	found := false
	for _, l := range plainLines {
		if strings.Contains(l, "下雨了") {
			found = true
			if !strings.HasPrefix(l, "    下雨了") {
				t.Errorf("indented content lost its leading margin: %q", l)
			}
		}
	}
	if !found {
		t.Fatal("indented content line missing")
	}

	// Every continuation of the overlong 示例 line carries the content indent.
	first := -1
	for i, l := range plainLines {
		if strings.Contains(l, "本单元格内容") {
			first = i
			break
		}
	}
	if first < 0 {
		t.Fatal("overlong line missing")
	}
	if first == len(plainLines)-1 {
		t.Fatal("overlong line did not wrap")
	}
	for i, l := range plainLines[first+1:] {
		if !strings.HasPrefix(l, "    ") {
			t.Errorf("overlong line continuation %d lost the content indent: %q", i, l)
		}
	}

	// Content preservation: strip the fixed margin from every line and
	// concatenate. Wordwrap consumes whitespace runs at break points (a
	// long CJK run that can't fit after the previous cell drops the
	// separator spaces), so compare with all spaces removed — every non-
	// space character must survive, in order.
	var joined strings.Builder
	for i, l := range plainLines {
		body := l
		if i == 0 {
			body = strings.TrimPrefix(body, "› ")
		} else {
			body = strings.TrimPrefix(body, "  ")
		}
		joined.WriteString(strings.TrimRight(body, " "))
	}
	flat := func(s string) string { return strings.ReplaceAll(s, " ", "") }
	if got, want := flat(joined.String()), flat(strings.ReplaceAll(userPaste, "\n", "")); got != want {
		t.Errorf("content mismatch:\n got  %q\n want %q", got, want)
	}
}

// TestWrapTranscriptPassThrough guards the global re-wrap: fitting lines
// (tables, styled content, indented text) stay byte-identical and every line
// stays padded to the transcript width, while overlong lines keep their
// leading whitespace at each wrap.
func TestWrapTranscriptPassThrough(t *testing.T) {
	lines := []string{
		"  │ a    │ bb │",
		"\x1b[2m  indented dim\x1b[0m",
		"  下雨了    窗外的雨",
		"plain",
	}
	in := strings.Join(lines, "\n")
	got := wrapTranscript(in, 30)
	gotLines := strings.Split(got, "\n")
	if len(gotLines) != len(lines) {
		t.Fatalf("line count changed: %d -> %d", len(lines), len(gotLines))
	}
	for i, l := range gotLines {
		if ansi.Strip(strings.TrimRight(l, " ")) != ansi.Strip(lines[i]) {
			t.Errorf("fitting line %d altered: %q -> %q", i, lines[i], l)
		}
		if visibleWidth(l) != 30 {
			t.Errorf("line %d width = %d, want 30 (padded)", i, visibleWidth(l))
		}
	}

	// Overlong indented line: every wrapped piece keeps the indent.
	over := "  " + strings.Repeat("中", 40)
	wrapped := wrapTranscript(over, 24)
	for _, l := range strings.Split(wrapped, "\n") {
		if !strings.HasPrefix(ansi.Strip(l), "  ") {
			t.Errorf("wrapTranscript dropped leading indent: %q", l)
		}
		if visibleWidth(l) > 24 {
			t.Errorf("wrapTranscript line exceeds width: %q", l)
		}
	}
}

// TestReasoningBlockPreservesIndent: indented reasoning lines (common in
// streamed code snippets) keep their leading whitespace when they wrap.
func TestReasoningBlockPreservesIndent(t *testing.T) {
	raw := "  step one\n  " + strings.Repeat("中", 50)
	out := reasoningBlock(raw, 40, 0)
	if !strings.Contains(ansi.Strip(out), "step one") {
		t.Fatalf("reasoning content missing:\n%s", out)
	}
	content := strings.Split(ansi.Strip(out), "\n")
	if len(content) < 2 {
		t.Fatalf("overlong reasoning line did not wrap:\n%s", out)
	}
	for _, l := range content[1:] {
		// outputIndent (4 spaces) + the content's own indent
		if !strings.HasPrefix(l, "      ") {
			t.Errorf("reasoning continuation lost the indent: %q", l)
		}
	}
	// every non-space character survives, in order (wrap consumes spaces at
	// break points and re-adds the content indent per piece)
	flat := func(s string) string {
		return strings.ReplaceAll(strings.ReplaceAll(s, " ", ""), "\n", "")
	}
	if got, want := flat(ansi.Strip(out)), flat(strings.ReplaceAll(raw, "\n", "")); got != want {
		t.Errorf("reasoning content lost:\n got  %q\n want %q", got, want)
	}
}
