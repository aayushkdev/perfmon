package tui

import "github.com/gdamore/tcell/v2"

var palette = struct {
	bg        tcell.Color
	panel     tcell.Color
	border    tcell.Color
	selection tcell.Color
	text      tcell.Color
	muted     tcell.Color
	accent    tcell.Color
	good      tcell.Color
	warn      tcell.Color
	bad       tcell.Color
}{
	bg:        tcell.NewRGBColor(8, 13, 20),
	panel:     tcell.NewRGBColor(15, 23, 32),
	border:    tcell.NewRGBColor(51, 65, 85),
	selection: tcell.NewRGBColor(30, 64, 91),
	text:      tcell.NewRGBColor(226, 232, 240),
	muted:     tcell.NewRGBColor(148, 163, 184),
	accent:    tcell.NewRGBColor(125, 211, 252),
	good:      tcell.NewRGBColor(34, 197, 94),
	warn:      tcell.NewRGBColor(245, 158, 11),
	bad:       tcell.NewRGBColor(239, 68, 68),
}
