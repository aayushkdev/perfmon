package tui

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// selectionColor is the fixed background used for selected rows and cells. It is
// an explicit RGB color so its brightness is known and the text color can be
// chosen for contrast, unlike an ANSI color whose shade depends on the terminal.
var selectionColor = tcell.NewRGBColor(0x2b, 0x4c, 0x7e)

var palette = struct {
	bg            tcell.Color
	panel         tcell.Color
	border        tcell.Color
	selection     tcell.Color
	selectionText tcell.Color
	text          tcell.Color
	muted         tcell.Color
	accent        tcell.Color
	good          tcell.Color
	warn          tcell.Color
	bad           tcell.Color
}{
	bg:            tcell.ColorDefault,
	panel:         tcell.ColorDefault,
	border:        tcell.ColorBlue,
	selection:     selectionColor,
	selectionText: contrastText(selectionColor),
	text:          tcell.ColorDefault,
	muted:         tcell.ColorSilver,
	accent:        tcell.ColorAqua,
	good:          tcell.ColorGreen,
	warn:          tcell.ColorYellow,
	bad:           tcell.ColorRed,
}

// contrastText returns black or white, whichever reads better on bg.
func contrastText(bg tcell.Color) tcell.Color {
	r, g, b := bg.RGB()
	if 0.299*float64(r)+0.587*float64(g)+0.114*float64(b) > 128 {
		return tcell.ColorBlack
	}
	return tcell.ColorWhite
}

func headerCell(text string, active bool) *tview.TableCell {
	color := palette.accent
	style := tcell.StyleDefault.Foreground(color).Background(palette.panel).Bold(true)
	if active {
		style = style.Foreground(palette.accent).Underline(true)
	}
	return tview.NewTableCell(text).SetStyle(style).SetExpansion(1).SetSelectable(false)
}

func cell(text string, color tcell.Color, bold bool) *tview.TableCell {
	style := tcell.StyleDefault.Foreground(color).Background(palette.panel)
	if bold {
		style = style.Bold(true)
	}
	return tview.NewTableCell(text).SetStyle(style).SetExpansion(1)
}
