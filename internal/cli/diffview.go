// Renders a unified diff as line-numbered, syntax-highlighted rows with a
// colored +/- sign column. Rows carry no background bar: the block reads as
// quiet indented code under the tool's card.
package cli

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/alecthomas/chroma/v2/formatters"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/charmbracelet/x/ansi"

	"reasonix/internal/event"
	"reasonix/internal/i18n"
)

const tabWidth = 4

const (
	// diffFoldLimit is the max lines to show in a diff when folding is enabled
	// (/diff-fold toggle). 0 means show all lines.
	diffFoldLimit = 40
)

var (
	diffChromaFmt = formatters.Get("terminal256")
	hunkRE        = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@`)
)

// diffStat renders a change's "+A -B" tally, green/red, omitting a zero side.
func diffStat(d event.FileDiff) string {
	parts := make([]string, 0, 2)
	if d.Added > 0 {
		parts = append(parts, green("+"+strconv.Itoa(d.Added)))
	}
	if d.Removed > 0 {
		parts = append(parts, red("-"+strconv.Itoa(d.Removed)))
	}
	return strings.Join(parts, " ")
}

func diffPath(args string) string {
	var p struct {
		Path string `json:"path"`
	}
	_ = json.Unmarshal([]byte(args), &p)
	return p.Path
}

// diffBlock renders a writer call as a header line ("← Update path  +A -B")
// plus the highlighted, folded diff body. Returns nil when there's no textual
// diff. The header uses the same opencode-style line as a tool card so the
// edit preview keeps the card's icon and verb.
func diffBlock(name, args string, d event.FileDiff, width, maxLines int) []string {
	if d.Diff == "" {
		return nil
	}
	path := diffPath(args)
	header := toolCardLine(name, args, "", false, width)
	if stat := diffStat(d); stat != "" {
		header += "  " + stat
	}
	return append([]string{header}, diffBody(d, path, width, maxLines)...)
}

// diffBody renders the hunks with a line-number gutter, dropping the file and
// "@@" headers (a dim "⋮" marks each hunk jump) and folding past maxLines to a
// "+N more" footer. path selects the syntax lexer.
func diffBody(d event.FileDiff, path string, width, maxLines int) []string {
	if d.Diff == "" {
		return nil
	}
	src := strings.Split(strings.TrimRight(d.Diff, "\n"), "\n")
	// Drop the "--- a/… / +++ b/…" header pair positionally — matching the prefix
	// on every line would eat real content (a deleted SQL "-- x" renders "--- x",
	// an added "++ y" renders "+++ y").
	if len(src) >= 2 && strings.HasPrefix(src[0], "--- ") && strings.HasPrefix(src[1], "+++ ") {
		src = src[2:]
	}
	gw := gutterWidth(src)

	var rows []string
	oldNo, newNo, hunks := 0, 0, 0
	for _, ln := range src {
		if ln == "" {
			continue
		}
		switch ln[0] {
		case '@':
			if m := hunkRE.FindStringSubmatch(ln); m != nil {
				oldNo, newNo = atoi(m[1]), atoi(m[3])
			}
			if hunks > 0 {
				rows = append(rows, "  "+dim("⋮"))
			}
			hunks++
		case '+':
			rows = append(rows, diffRow('+', ln[1:], path, width, newNo, gw))
			newNo++
		case '-':
			rows = append(rows, diffRow('-', ln[1:], path, width, oldNo, gw))
			oldNo++
		case '\\':
			rows = append(rows, "  "+dim(clampPlain(ln, width-2)))
		default:
			code := ln
			if ln[0] == ' ' {
				code = ln[1:]
			}
			rows = append(rows, diffRow(' ', code, path, width, newNo, gw))
			oldNo++
			newNo++
		}
	}

	if maxLines > 0 && len(rows) > maxLines {
		folded := len(rows) - (maxLines - 1)
		rows = rows[:maxLines-1]
		rows = append(rows, "  "+dim(fmt.Sprintf(i18n.M.DiffFoldedFmt, folded)))
	}
	return rows
}

// diffRow draws one diff line: a dim right-aligned line-number gutter, then a
// colored "+"/"-" sign (or a blank sign column for context) and the
// syntax-highlighted code. Rows carry no background bar — the sign column
// alone carries the add/remove meaning, so the block reads as quiet indented
// code under the tool card. Fixed columns are "  " + gutter + " s " (gw+5),
// leaving codeW = width-gw-5 so the longest row lands exactly at width.
func diffRow(sign byte, code, path string, width, lineNo, gw int) string {
	gutter := dim(lpad(strconv.Itoa(lineNo), gw))
	codeW := width - 2 - gw - 3 // "  " prefix + gutter + " s " sign column
	if codeW < 1 {
		codeW = 1
	}
	code = clampPlain(code, codeW)
	if !colorOn() {
		if sign == ' ' {
			return "  " + gutter + "   " + code
		}
		return "  " + gutter + " " + string(sign) + " " + code
	}
	if sign == ' ' {
		return "  " + gutter + "   " + highlightCode(path, code)
	}
	fg := activeCLITheme.success
	if sign == '-' {
		fg = activeCLITheme.err
	}
	return "  " + gutter + " " + themeFg(fg, string(sign)) + " " + highlightCode(path, code)
}

func gutterWidth(lines []string) int {
	max := 0
	for _, ln := range lines {
		m := hunkRE.FindStringSubmatch(ln)
		if m == nil {
			continue
		}
		for _, p := range [][2]int{{1, 2}, {3, 4}} {
			end := atoi(m[p[0]])
			if m[p[1]] != "" {
				end += atoi(m[p[1]])
			} else {
				end++
			}
			if end > max {
				max = end
			}
		}
	}
	if w := len(strconv.Itoa(max)); w > 2 {
		return w
	}
	return 2
}

func lpad(s string, w int) string {
	if len(s) >= w {
		return s
	}
	return strings.Repeat(" ", w-len(s)) + s
}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

func clampPlain(s string, w int) string {
	if w < 1 {
		w = 1
	}
	return ansi.Truncate(expandTabs(s), w, "")
}

// expandTabs replaces tabs with spaces to the next tabWidth stop. A literal tab
// has zero StringWidth but the terminal advances it to a tab stop, so leaving
// tabs in a clamped row overflows the measured width — expand them so the
// clamped output matches what's drawn.
func expandTabs(s string) string {
	if !strings.ContainsRune(s, '\t') {
		return s
	}
	var b strings.Builder
	col := 0
	for _, r := range s {
		if r == '\t' {
			n := tabWidth - col%tabWidth
			for i := 0; i < n; i++ {
				b.WriteByte(' ')
			}
			col += n
			continue
		}
		b.WriteRune(r)
		col++
	}
	return b.String()
}

func highlightCode(path, code string) string {
	if code == "" {
		return code
	}
	lexer := lexers.Match(path)
	if lexer == nil {
		lexer = lexers.Fallback
	}
	it, err := lexer.Tokenise(nil, code)
	if err != nil {
		return code
	}
	var b strings.Builder
	styleName := "github-dark"
	if activeCLITheme.name == "light" {
		styleName = "github"
	}
	style := styles.Get(styleName)
	if diffChromaFmt.Format(&b, style, it) != nil {
		return code
	}
	return strings.TrimRight(b.String(), "\n")
}

// highlightCodeByLang highlights using a language alias, for use in markdown blocks.
func highlightCodeByLang(lang, code string) string {
	if code == "" {
		return code
	}
	lexer := lexers.Get(lang)
	if lexer == nil {
		lexer = lexers.Fallback
	}
	it, err := lexer.Tokenise(nil, code)
	if err != nil {
		return code
	}
	var b strings.Builder
	styleName := "github-dark"
	if activeCLITheme.name == "light" {
		styleName = "github"
	}
	style := styles.Get(styleName)
	if diffChromaFmt.Format(&b, style, it) != nil {
		return code
	}
	return strings.TrimRight(b.String(), "\n")
}
