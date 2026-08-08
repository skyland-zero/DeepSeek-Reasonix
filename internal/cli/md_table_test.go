package cli

import (
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// railColumns returns the terminal display columns of every vertical rail in
// a rendered table line: content rails (│) plus border corners and junctions
// (┌┬┐├┼┤└┴┘), so border and content lines compare on equal footing. Column
// widths are display columns, so CJK cells push rails correctly.
func railColumns(line string) []int {
	plain := ansi.Strip(line)
	var cols []int
	col := 0
	for _, r := range plain {
		switch r {
		case '│', '┌', '┬', '┐', '├', '┼', '┤', '└', '┴', '┘':
			cols = append(cols, col)
		}
		col += visibleWidth(string(r))
	}
	return cols
}

// assertTableGrid checks the box-drawing contract every renderer output line
// must honour: no line may exceed the renderer width (or the terminal wraps
// the trailing rail onto its own line) and every rail must land on the same
// columns, so border and content rows line up. Tables whose natural widths
// fit within the space are allowed to render narrower than the width.
func assertTableGrid(t *testing.T, out string, width int) {
	t.Helper()
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) == 0 {
		t.Fatal("render produced no lines")
	}
	ref := railColumns(lines[0])
	for i, line := range lines {
		if vw := visibleWidth(line); vw > width {
			t.Errorf("line %d overflows renderer width %d: %d wide\n%q", i, width, vw, line)
		}
		if !slices.Equal(railColumns(line), ref) {
			t.Errorf("line %d rails misaligned with line 0\nline 0: %q\nline %d: %q",
				i, lines[0], i, line)
		}
	}
}

// assertTableFillsWidth asserts the table consumes the full renderer width —
// valid when at least one cell is wide enough that water-fill must distribute
// all available space (the regression case for the rounding overshoot).
func assertTableFillsWidth(t *testing.T, out string, width int) {
	t.Helper()
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) == 0 {
		t.Fatal("render produced no lines")
	}
	for i, line := range lines {
		if vw := visibleWidth(line); vw != width {
			t.Errorf("line %d width = %d, want exactly %d\n%q", i, vw, width, line)
		}
	}
}

// userReviewTable is the CJK-heavy, long-cell report table from the field
// report that exposed the water-fill overflow: its natural widths are far
// beyond a fair share, so rounding could hand out one column more than
// available and push every line a column past the renderer width.
const userReviewTable = `| # | 问题 | 证据 | 建议 |
|---|---|---|---|
| 11 | csproj HintPath 指向不存在的包，编译隐患 | Student.Web/4-Student.Web.csproj:53-57（AspNet.ScriptManager.jQuery.1.8.2/UI.Combined.1.8.24）、:288-301（Senparc.CO2NET.Cache.Redis.5.2.0.1、RedLock.4.1.0.1、Senparc.Weixin.Cache.Redis.2.20.8） | 逐一核对 packages/ 实际版本修复；跨项目 bin 引用改为 ProjectReference 或 NuGet |
| 12 | 构建脚本重复 4 份 | 4 个 bat 各含同一段 VS18 路径探测（build.bat:16-24 等）；2 个 publish ps1 仅差 2 个变量 | 合并为参数化脚本（build.bat -p student/teacher 风格） |
| 13 | 残留文件与副本 | 4 个 .vspscc（VSS 绑定）；Student.Web/Models/StudntBYSModle.cs（拼写错误、Models 目录仅此 1 文件）；学生端 HomeBF - 复制.html、SystemError - 副本.html 等；Grids/↔GridsStudents/、Venues/↔VenuesNew/、Holidays/↔HolidayStudens/、Achievements/↔AchievementsBF/ 内部重复目录 | 确认无引用后删除；重复目录合并保留最新版 |
| 14 | 前端外部依赖残留 | Student.StudentWeb/Student/News/OpenPDF.html:228（jsdelivr vue@2.7.16）、Student.Web/res/zsyzj/css/ydui.css:1952（非 HTTPS iconfont） | 本地化（项目已有自托管资源惯例） |
`

// TestRenderTableGridFitsWidth is the regression test for the water-fill
// overshoot: column-width rounding must never hand out more space than the
// renderer width allows, or the trailing rail wraps onto its own line.
func TestRenderTableGridFitsWidth(t *testing.T) {
	for _, w := range []int{60, 80, 100, 120, 140, 160, 180, 187, 200, 240} {
		t.Run("width", func(t *testing.T) {
			out := newMarkdownRenderer(w).Render(userReviewTable)
			assertTableGrid(t, out, w)
			// Content-forced case: the wide 证据 cell must consume every
			// column of the available width — never more, never less.
			assertTableFillsWidth(t, out, w)
		})
	}
}

// TestRenderTableGridStreaming checks the streaming path (bottom border
// omitted) keeps the same fit-and-alignment contract.
func TestRenderTableGridStreaming(t *testing.T) {
	for _, w := range []int{80, 120, 187} {
		r := newMarkdownRenderer(w)
		r.streaming = true
		assertTableGrid(t, r.Render(userReviewTable), w)
	}
}

// tableBlock extracts the box-drawing table region (first ┌ border to last └
// border) so a streamed render can be compared against the committed render.
func tableBlock(out string) string {
	lines := strings.Split(out, "\n")
	start, end := -1, -1
	for i, ln := range lines {
		if strings.Contains(ln, "┌") && start == -1 {
			start = i
		}
		if strings.Contains(ln, "└") {
			end = i
		}
	}
	if start == -1 || end == -1 {
		return ""
	}
	return strings.Join(lines[start:end+1], "\n")
}

// TestRenderTableStreamingClosesEarly proves a table followed by another
// block is finalised while still streaming — water-fill widths and the bottom
// border appear the moment the model moves on — and renders byte-identical to
// the committed render so the streamed view never jumps at commitPending.
func TestRenderTableStreamingClosesEarly(t *testing.T) {
	md := userReviewTable + "\n\nAnd a follow-up paragraph after the table."
	for _, w := range []int{60, 80, 120} {
		t.Run("width", func(t *testing.T) {
			r := newMarkdownRenderer(w)
			r.streaming = true
			got := r.Render(md)

			block := tableBlock(got)
			if block == "" {
				t.Fatalf("no closed table block at width %d:\n%s", w, got)
			}
			want := tableBlock(newMarkdownRenderer(w).Render(md))
			if block != want {
				t.Errorf("streamed closed table != committed table at width %d\n--- streamed ---\n%s\n--- committed ---\n%s",
					w, block, want)
			}
			assertTableGrid(t, block, w)
		})
	}
}

// TestRenderTableStreamingAdaptiveColumns proves the streaming renderer sizes
// columns from the rows seen so far: the trailing half-written row (no
// newline yet) renders but never widens its column, and once the last row
// completes the streamed layout matches the committed one — so commitPending
// cannot reflow the table at the end of the answer.
func TestRenderTableStreamingAdaptiveColumns(t *testing.T) {
	const md = "| col a | col b |\n" +
		"|---|---|\n" +
		"| aa | bb |\n" +
		"| aaa | bbbbb |\n" +
		"| aaaaa | b |\n"

	committedCols := railColumns(strings.Split(newMarkdownRenderer(80).Render(md), "\n")[0])

	lines := strings.Split(md, "\n")
	for i := 3; i <= len(lines); i++ {
		prefix := strings.Join(lines[:i], "\n")
		r := newMarkdownRenderer(80)
		r.streaming = true
		out := r.Render(prefix)
		assertTableGrid(t, out, 80)
		if i == len(lines) {
			// All rows complete: the streamed layout must already match the
			// committed one, so the final redraw never reshuffles columns.
			if got := railColumns(strings.Split(out, "\n")[0]); !slices.Equal(got, committedCols) {
				t.Errorf("full streamed columns %v != committed %v\n%s", got, committedCols, out)
			}
		}
	}

	// A half-written trailing row (no newline) must not change the columns:
	// its width is excluded until the model completes the cell.
	streamed := func(md string) []int {
		r := newMarkdownRenderer(80)
		r.streaming = true
		return railColumns(strings.Split(r.Render(md), "\n")[0])
	}
	base := "| col a | col b |\n|---|---|\n| aa | bb |\n"
	half := base + "| w | zzzzzzzzzz"
	if got, want := streamed(half), streamed(base); !slices.Equal(got, want) {
		t.Errorf("half-written row changed columns: half %v, base %v", got, want)
	}
	done := streamed(half + " |\n")
	if slices.Equal(done, streamed(half)) {
		t.Errorf("completed row should widen its column: half %v, done %v", streamed(half), done)
	}
}

// TestRenderTableEmbeddedCellBreaks covers cells where the model pre-wrapped
// its own content with hard line breaks (surfaced by goldmark as soft breaks).
// The renderer must re-flow the cell at the column width — not honour the
// author's arbitrary break points — and lose no content.
func TestRenderTableEmbeddedCellBreaks(t *testing.T) {
	md := "| # | 证据 | 建议 |\n" +
		"|---|---|---|\n" +
		"| 11 | Student.Web/4-Student.Web.csproj:53-\n" +
		"57（AspNet.ScriptManager.jQuery.1.8.2/UI.Combined.1.8.24）、:288-\n" +
		"301（Senparc.CO2NET.Cache.Redis.5.2.0.1、RedLock.4.1.0.1、Senparc.Weixin.Cac\n" +
		"he.Redis.2.20.8） | 逐一核对 packages/ 实际版本修复；跨项目 bin\n" +
		"引用改为 ProjectReference 或 NuGet |\n"
	for _, w := range []int{60, 80, 120} {
		t.Run("width", func(t *testing.T) {
			out := newMarkdownRenderer(w).Render(md)
			assertTableGrid(t, out, w)
			// Content is preserved across wrap lines; the model's own break
			// splits "Cache" mid-word (Cac/he.Redis), so that fragment is
			// checked as the renderer faithfully emits it.
			compact := strings.ReplaceAll(ansi.Strip(out), "\n", "")
			for _, want := range []string{
				"2.20.8）", "Weixin.Cac", "或 NuGet", "ProjectReference",
			} {
				if !strings.Contains(compact, want) {
					t.Errorf("content lost %q at width %d:\n%s", want, w, out)
				}
			}
		})
	}
}

// TestRenderTablePureCJK narrow case: every column barely wide enough for two
// CJK characters; distribution must still fit exactly.
func TestRenderTablePureCJK(t *testing.T) {
	md := "| 姓名 | 部门 | 状态 |\n|---|---|---|\n| 张三 | 研发部 | 正常 |\n| 李四 | 测试组 | 请假 |\n"
	for _, w := range []int{36, 40, 44, 60} {
		t.Run("width", func(t *testing.T) {
			assertTableGrid(t, newMarkdownRenderer(w).Render(md), w)
		})
	}
}
