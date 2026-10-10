package tui

import (
	"fmt"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"os"
	"strconv"
	"strings"
	"syscall"
)

type signalInfo struct {
	num  int
	name string
}

var linuxSignals = []signalInfo{
	{1, "SIGHUP"}, {2, "SIGINT"}, {3, "SIGQUIT"}, {4, "SIGILL"},
	{5, "SIGTRAP"}, {6, "SIGABRT"}, {7, "SIGBUS"}, {8, "SIGFPE"},
	{9, "SIGKILL"}, {10, "SIGUSR1"}, {11, "SIGSEGV"}, {12, "SIGUSR2"},
	{13, "SIGPIPE"}, {14, "SIGALRM"}, {15, "SIGTERM"}, {16, "SIGSTKFLT"},
	{17, "SIGCHLD"}, {18, "SIGCONT"}, {19, "SIGSTOP"}, {20, "SIGTSTP"},
	{21, "SIGTTIN"}, {22, "SIGTTOU"}, {23, "SIGURG"}, {24, "SIGXCPU"},
	{25, "SIGXFSZ"}, {26, "SIGVTALRM"}, {27, "SIGPROF"}, {28, "SIGWINCH"},
	{29, "SIGIO"}, {30, "SIGPWR"}, {31, "SIGSYS"},
}

const signalColumns = 4

func signalRows() int {
	return (len(linuxSignals) + signalColumns - 1) / signalColumns
}

func (a *App) openSignalDialog() {
	if a.activeMiddle != "procs" || a.signalOpen {
		return
	}
	row, _ := a.processTable.GetSelection()
	if row <= 0 || row-1 >= len(a.procList) {
		a.flashFooter("[yellow]Select a process row first.[-]")
		return
	}
	p := a.procList[row-1]
	if p.PID <= 1 || p.PID == os.Getpid() {
		a.flashFooter(fmt.Sprintf("[yellow]Refusing to signal protected PID %d.[-]", p.PID))
		return
	}

	table := buildSignalTable()

	input := tview.NewInputField().
		SetLabel(fmt.Sprintf(" PID %d  %s  signal # ", p.PID, truncateProcessText(fallback(p.Name, "-"), 20))).
		SetFieldWidth(5).
		SetAcceptanceFunc(tview.InputFieldInteger)
	input.SetBackgroundColor(palette.panel)
	input.SetFieldBackgroundColor(palette.panel)
	input.SetLabelColor(palette.accent)
	input.SetPlaceholder("e.g. 15")

	send := func(text string) {
		text = strings.TrimSpace(text)
		if text == "" {
			if info, ok := signalAt(table.GetSelection()); ok {
				text = strconv.Itoa(info.num)
			}
		}
		if text == "" {
			a.flashFooter("[yellow]Enter a signal number, e.g. 15 for SIGTERM.[-]")
			return
		}
		num, err := strconv.Atoi(text)
		if err != nil || num < 1 || num > len(linuxSignals) {
			a.flashFooter(fmt.Sprintf("[yellow]Invalid signal number %q.[-]", text))
			return
		}
		a.closeSignalDialog()
		a.deliverSignal(p, syscall.Signal(num))
	}

	input.SetChangedFunc(func(text string) { highlightSignal(table, text) })
	input.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if info, ok := moveSignalSelection(table, event.Key()); ok {
			setInputSignal(input, info.num)
			return nil
		}
		return event
	})
	input.SetDoneFunc(func(key tcell.Key) {
		switch key {
		case tcell.KeyEnter:
			send(input.GetText())
		case tcell.KeyEscape:
			a.closeSignalDialog()
		case tcell.KeyTab, tcell.KeyBacktab:
			a.app.SetFocus(table)
		}
	})

	table.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyEscape:
			a.closeSignalDialog()
			return nil
		case tcell.KeyTab, tcell.KeyBacktab:
			a.app.SetFocus(input)
			return nil
		case tcell.KeyEnter:
			if info, ok := signalAt(table.GetSelection()); ok {
				a.closeSignalDialog()
				a.deliverSignal(p, syscall.Signal(info.num))
			}
			return nil
		case tcell.KeyUp, tcell.KeyDown, tcell.KeyLeft, tcell.KeyRight:
			if info, ok := moveSignalSelection(table, event.Key()); ok {
				setInputSignal(input, info.num)
			}
			return nil
		}
		return event
	})

	dialog := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(input, 1, 0, true).
		AddItem(table, 0, 1, false)
	dialog.SetBorder(true)
	dialog.SetBorderColor(palette.border)
	dialog.SetTitle(" Send signal ").SetTitleColor(palette.accent)
	dialog.SetBackgroundColor(palette.panel)

	width := signalColumns*16 + 6
	height := signalRows() + 1 + 2
	a.signalOpen = true
	a.signalDialog = dialog
	a.pages.AddPage("signal", centeredPrimitive(dialog, width, height), true, true)
	a.app.SetFocus(input)
}

func (a *App) closeSignalDialog() {
	if !a.signalOpen {
		return
	}
	a.signalOpen = false
	a.signalDialog = nil
	a.pages.RemovePage("signal")
	a.app.SetFocus(a.processTable)
}

func buildSignalTable() *tview.Table {
	table := tview.NewTable().SetSelectable(true, true)
	table.SetBackgroundColor(palette.panel)
	table.SetSelectedStyle(tcell.StyleDefault.Background(palette.selection).Foreground(palette.selectionText))

	rows := signalRows()
	for i, sig := range linuxSignals {
		group := i / rows
		row := i % rows
		label := fmt.Sprintf("[aqua]%2d[-]  [silver](%s)[-]", sig.num, sig.name)
		c := cell(label, palette.text, false)
		c.SetMaxWidth(16)
		table.SetCell(row, group, c)
	}
	table.Select(0, 0)
	return table
}

func signalAt(row, column int) (signalInfo, bool) {
	rows := signalRows()
	if row < 0 || row >= rows || column < 0 || column >= signalColumns {
		return signalInfo{}, false
	}
	index := column*rows + row
	if index >= len(linuxSignals) {
		return signalInfo{}, false
	}
	return linuxSignals[index], true
}

func highlightSignal(table *tview.Table, text string) {
	num, err := strconv.Atoi(strings.TrimSpace(text))
	if err != nil {
		return
	}
	rows := signalRows()
	for i, sig := range linuxSignals {
		if sig.num == num {
			table.Select(i%rows, i/rows)
			return
		}
	}
}

func moveSignalSelection(table *tview.Table, key tcell.Key) (signalInfo, bool) {
	switch key {
	case tcell.KeyUp, tcell.KeyDown, tcell.KeyLeft, tcell.KeyRight:
	default:
		return signalInfo{}, false
	}
	rows := signalRows()
	row, col := table.GetSelection()
	group := col
	index := group*rows + row
	if row < 0 || row >= rows || group < 0 || group >= signalColumns || index >= len(linuxSignals) {
		index, row, group = 0, 0, 0
	}
	switch key {
	case tcell.KeyUp:
		if row > 0 {
			index--
		}
	case tcell.KeyDown:
		if row < rows-1 && index+1 < len(linuxSignals) {
			index++
		}
	case tcell.KeyLeft:
		if group > 0 {
			index -= rows
		}
	case tcell.KeyRight:
		if group < signalColumns-1 && index+rows < len(linuxSignals) {
			index += rows
		}
	}
	if index < 0 {
		index = 0
	}
	if index >= len(linuxSignals) {
		index = len(linuxSignals) - 1
	}
	table.Select(index%rows, index/rows)
	return linuxSignals[index], true
}

func setInputSignal(input *tview.InputField, num int) {
	input.SetText(strconv.Itoa(num))
}

func centeredPrimitive(p tview.Primitive, width, height int) tview.Primitive {
	return tview.NewFlex().
		AddItem(nil, 0, 1, false).
		AddItem(tview.NewFlex().SetDirection(tview.FlexRow).
			AddItem(nil, 0, 1, false).
			AddItem(p, height, 1, true).
			AddItem(nil, 0, 1, false), width, 1, true).
		AddItem(nil, 0, 1, false)
}

func dimOutside(screen tcell.Screen, x, y, w, h int) {
	width, height := screen.Size()
	for row := 0; row < height; row++ {
		for col := 0; col < width; col++ {
			if col >= x && col < x+w && row >= y && row < y+h {
				continue
			}
			mainc, combc, style, _ := screen.GetContent(col, row)
			screen.SetContent(col, row, mainc, combc, style.Dim(true))
		}
	}
}

func signalName(sig syscall.Signal) string {
	for _, s := range linuxSignals {
		if s.num == int(sig) {
			return s.name
		}
	}
	return sig.String()
}
