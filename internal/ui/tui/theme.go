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
	bg:        tcell.ColorDefault,
	panel:     tcell.ColorDefault,
	border:    tcell.ColorBlue,
	selection: tcell.ColorBlue,
	text:      tcell.ColorDefault,
	muted:     tcell.ColorSilver,
	accent:    tcell.ColorAqua,
	good:      tcell.ColorGreen,
	warn:      tcell.ColorYellow,
	bad:       tcell.ColorRed,
}
