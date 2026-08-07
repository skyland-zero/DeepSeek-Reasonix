package cli

import (
	"strings"
)

// treeIslandRows is the Laputa-style silhouette of the floating island with
// the giant tree (crown + trunk) above it and clouds below. Row colour roles:
// rows 0-2 crown (muted), row 3 trunk (accent), rows 4-9 island (muted),
// rows 10-11 clouds (faint).
var treeIslandRows = []string{
	"          ▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀",
	"       ▄▄▄▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▄▄▄",
	"      █▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀█",
	"            ████████████",
	"       ▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄",
	"      ██████████████████████████████",
	"      ▀████████████████████████████▀",
	"        ▀▀████████████████████▀▀",
	"           ▀▀████████████▀▀",
	"              ▀▀████▀▀",
	"   ~ ~ ~ ~ ~ ~ ~ ~ ~ ~ ~ ~ ~ ~ ~ ~ ~ ~ ~",
	"  ~ ~ ~ ~ ~ ~ ~ ~ ~ ~ ~ ~ ~ ~ ~ ~ ~ ~ ~ ~",
}

// treeIslandArtWidth is the rendered width of the art (its widest row after
// centering).
const treeIslandArtWidth = 41

// treeIslandMinInner is the minimum card inner width that still fits the art.
// The threshold sits deliberately high (only terminals of roughly 60 columns
// or more): a tall banner on a short window would overflow the transcript
// viewport and strand its scroll offset, which the scroll-pinning logic does
// not re-anchor. Narrower terminals get the compact info-only card.
const treeIslandMinInner = 56

// floatingTreeIslandArt renders the tree-and-island silhouette, rows
// horizontally centred and tinted by role. themeFg keeps the output plain on
// monochrome terminals.
func floatingTreeIslandArt() string {
	maxW := 0
	for _, row := range treeIslandRows {
		if w := visibleWidth(row); w > maxW {
			maxW = w
		}
	}
	out := make([]string, len(treeIslandRows))
	for i, row := range treeIslandRows {
		fg := activeCLITheme.muted
		switch {
		case i == 3:
			fg = activeCLITheme.accent // trunk
		case i >= 10:
			fg = activeCLITheme.faint // clouds
		}
		pad := (maxW - visibleWidth(row)) / 2
		out[i] = strings.Repeat(" ", pad) + themeFg(fg, row)
	}
	return strings.Join(out, "\n")
}
