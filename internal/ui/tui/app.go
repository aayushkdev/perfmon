package tui

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/aayushkdev/perfmon/internal/backend"
	"github.com/aayushkdev/perfmon/internal/model"
)

type App struct {
	collector backend.SnapshotCollector
	controls  backend.CoreController
	interval  time.Duration

	app          *tview.Application
	pages        *tview.Pages
	root         *tview.Grid
	header       *tview.TextView
	cpuPanel     *tview.TextView
	coreTable    *tview.Table
	processTable *tview.Table
	middlePages  *tview.Pages
	memoryPanel  *tview.TextView
	thermalPanel *tview.TextView
	batteryPanel *tview.TextView
	gpuPanel     *tview.TextView
	status       *tview.TextView
	last         model.Snapshot
	activeMiddle string          // "cores" or "procs"
	procSort     string          // "mem", "cpu", "pid", "name"
	procSortAsc  bool            // sort ascending when true, descending when false
	procList     []model.Process // processes in the order currently displayed
	procQuery    string          // case-insensitive process filter
	procSearch   bool            // process search input is active
	flashMsg     string          // transient footer message (e.g. signal result)
	flashUntil   time.Time       // when flashMsg should yield to the default hint
	signalOpen   bool            // signal picker dialog is open
	signalDialog tview.Primitive // the open signal picker, kept bright while the rest dims
}

const footerHint = "[silver]Select a core row, then use the key actions in the footer. Writes go through /sys and may require privileges.[-]"

func NewApp(collector backend.SnapshotCollector, interval time.Duration) *App {
	a := &App{
		collector: collector,
		interval:  interval,
		app:       tview.NewApplication(),
		pages:     tview.NewPages(),
		procSort:  "mem",
	}
	if controls, ok := collector.(backend.CoreController); ok {
		a.controls = controls
	}
	a.build()
	return a
}

func (a *App) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	a.app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		// Always prioritize explicit Ctrl+C (or Ctrl+c) to exit the program.
		if (event.Modifiers() & tcell.ModCtrl) != 0 {
			// If Ctrl is held and the rune is 'c' or 'C', treat as quit.
			if event.Rune() == 'c' || event.Rune() == 'C' || event.Key() == tcell.KeyCtrlC {
				cancel()
				a.app.Stop()
				return nil
			}
			if a.procSearch && (event.Rune() == 'u' || event.Rune() == 'U') {
				a.procQuery = ""
				go a.refresh(ctx)
				return nil
			}
		}
		if a.procSearch {
			switch event.Key() {
			case tcell.KeyEsc:
				a.procQuery = ""
				a.procSearch = false
				go a.refresh(ctx)
				return nil
			case tcell.KeyEnter:
				a.selectNextProcessMatch()
				return nil
			case tcell.KeyBackspace, tcell.KeyBackspace2:
				query := []rune(a.procQuery)
				if len(query) > 0 {
					a.procQuery = string(query[:len(query)-1])
					go a.refresh(ctx)
				}
				return nil
			}
			if event.Rune() >= ' ' && event.Rune() != '/' {
				a.procQuery += string(event.Rune())
				go a.refresh(ctx)
			}
			return nil
		}
		if a.signalOpen {
			return event
		}
		if event.Key() == tcell.KeyEsc && a.procQuery != "" {
			a.procQuery = ""
			go a.refresh(ctx)
			return nil
		}
		if event.Key() == tcell.KeyEnter && a.activeMiddle == "procs" {
			a.openSignalDialog()
			return nil
		}
		switch event.Rune() {
		case 'q', 'Q':
			cancel()
			a.app.Stop()
			return nil
		case 'r', 'R':
			go a.refresh(ctx)
			return nil
		case 'g', 'G':
			a.chooseGovernor(ctx)
			return nil
		case 'p', 'P':
			// toggle between cores and processes, then refresh immediately
			if a.activeMiddle == "cores" {
				a.switchMiddle("procs")
			} else {
				a.switchMiddle("cores")
			}
			go a.refresh(ctx)
			return nil
		case '/':
			if a.activeMiddle == "procs" {
				a.procSearch = true
				a.renderFooter(a.last, "")
			}
			return nil
		case 'c', 'C':
			// cycle process sort order (pid -> name -> cpu -> mem)
			if a.activeMiddle == "procs" {
				a.cycleProcSort()
				go a.refresh(ctx)
			}
			return nil
		case 's', 'S':
			// toggle ascending/descending for the current sort column
			if a.activeMiddle == "procs" {
				a.toggleProcSortDir()
				go a.refresh(ctx)
			}
			return nil
		case 'k':
			// terminate the selected process (SIGTERM)
			a.signalSelectedProcess(syscall.SIGTERM)
			return nil
		case 'K':
			// force-kill the selected process (SIGKILL)
			a.signalSelectedProcess(syscall.SIGKILL)
			return nil
		case 'e', 'E':
			a.chooseEPP(ctx)
			return nil
		case 'm', 'M':
			a.choosePowerProfile(ctx)
			return nil
		case 't', 'T':
			// 't' toggles turbo (accept uppercase too)
			a.toggleTurbo(ctx)
			return nil
		case 'o', 'O':
			a.toggleSelectedCore(ctx)
			return nil
		}
		if event.Key() == tcell.KeyEsc {
			cancel()
			a.app.Stop()
			return nil
		}
		return event
	})

	a.app.SetAfterDrawFunc(func(screen tcell.Screen) {
		if !a.signalOpen || a.signalDialog == nil {
			return
		}
		x, y, w, h := a.signalDialog.GetRect()
		dimOutside(screen, x, y, w, h)
	})

	a.initialRender(ctx)
	go a.poll(ctx)
	return a.app.SetRoot(a.pages, true).EnableMouse(true).Run()
}

func (a *App) switchMiddle(name string) {
	if name == a.activeMiddle {
		return
	}
	switch name {
	case "cores":
		a.middlePages.ShowPage("cores")
		a.middlePages.HidePage("procs")
		a.activeMiddle = "cores"
	case "procs":
		a.middlePages.ShowPage("procs")
		a.middlePages.HidePage("cores")
		a.activeMiddle = "procs"
	}
}

func (a *App) build() {
	tview.Styles.PrimitiveBackgroundColor = palette.bg
	tview.Styles.ContrastBackgroundColor = palette.panel
	tview.Styles.PrimaryTextColor = palette.text
	tview.Styles.SecondaryTextColor = palette.muted
	tview.Styles.BorderColor = palette.border
	tview.Styles.TitleColor = palette.accent

	a.header = textPanel("", false)
	a.cpuPanel = textPanel("CPU", true)
	a.memoryPanel = textPanel("Memory", true)
	a.thermalPanel = textPanel("Thermals", true)
	a.thermalPanel.SetWrap(true)
	a.batteryPanel = textPanel("Battery", true)
	a.batteryPanel.SetWrap(true)
	a.gpuPanel = textPanel("GPU", true)
	a.gpuPanel.SetWrap(true)
	a.status = textPanel("", false)

	a.coreTable = tablePanel("Cores")
	a.processTable = tablePanel("Processes")
	a.processTable.SetMouseCapture(func(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
		if action != tview.MouseScrollUp && action != tview.MouseScrollDown {
			return action, event
		}
		row, _ := a.processTable.GetSelection()
		if action == tview.MouseScrollUp {
			row--
		} else {
			row++
		}
		if row < 1 {
			row = 1
		}
		if len(a.procList) > 0 && row > len(a.procList) {
			row = len(a.procList)
		}
		a.processTable.Select(row, 0)
		return tview.MouseConsumed, nil
	})
	// processTable placeholder will be populated when rendering
	a.middlePages = tview.NewPages()
	a.middlePages.AddPage("cores", a.coreTable, true, true)
	a.middlePages.AddPage("procs", a.processTable, true, false)
	a.activeMiddle = "cores"
	a.root = tview.NewGrid().
		SetRows(3, 12, 0, 7, 2).
		SetColumns(-1, -1, -1).
		SetBorders(false).
		AddItem(a.header, 0, 0, 1, 3, 0, 0, false).
		AddItem(a.cpuPanel, 1, 0, 1, 1, 0, 0, false).
		AddItem(a.memoryPanel, 1, 1, 1, 1, 0, 0, false).
		AddItem(a.gpuPanel, 1, 2, 1, 1, 0, 0, false).
		AddItem(a.middlePages, 2, 0, 1, 3, 0, 0, true).
		AddItem(a.thermalPanel, 3, 0, 1, 2, 0, 0, false).
		AddItem(a.batteryPanel, 3, 2, 1, 1, 0, 0, false).
		AddItem(a.status, 4, 0, 1, 3, 0, 0, false)
	a.pages.AddPage("main", a.root, true, true)
}

func textPanel(title string, border bool) *tview.TextView {
	view := tview.NewTextView().
		SetDynamicColors(true).
		SetWrap(false)
	view.SetBackgroundColor(palette.panel)
	view.SetTextColor(palette.text)
	view.SetBorder(border)
	view.SetBorderColor(palette.border)
	view.SetTitleColor(palette.accent)
	if title != "" {
		view.SetTitle(" " + title + " ")
	}
	return view
}

func tablePanel(title string) *tview.Table {
	table := tview.NewTable().
		SetSelectable(true, false).
		SetFixed(1, 0)
	table.SetBackgroundColor(palette.panel)
	table.SetBorder(true)
	table.SetBorderColor(palette.border)
	table.SetTitle(" " + title + " ")
	table.SetTitleColor(palette.accent)
	table.SetSelectedStyle(tcell.StyleDefault.Background(palette.selection).Foreground(palette.text))
	return table
}

func (a *App) poll(ctx context.Context) {
	ticker := time.NewTicker(a.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.refresh(ctx)
		}
	}
}

func (a *App) refresh(ctx context.Context) {
	snap, err := a.collector.Snapshot(ctx)
	a.app.QueueUpdateDraw(func() {
		if err != nil {
			a.flashFooter(fmt.Sprintf("[red]collector error: %v[-]", err))
			return
		}
		a.render(snap)
	})
}

func (a *App) initialRender(ctx context.Context) {
	snap, err := a.collector.Snapshot(ctx)
	if err != nil {
		a.flashFooter(fmt.Sprintf("[red]collector error: %v[-]", err))
		return
	}
	a.render(snap)
}

func (a *App) render(s model.Snapshot) {
	a.last = s
	// Include overall CPU% and memory used (GiB) in the header for quick glance.
	cpuPct := fmt.Sprintf("%.1f%%", s.CPU.UsagePercent)
	memUsed := bytesGB(s.Memory.UsedBytes)
	a.header.SetText(fmt.Sprintf(
		"[aqua::b]perfmon[-:-:-]  [silver]kernel %s  arch %s  cpu %s  mem %s  updated %s[-]",
		fallback(s.Host.Kernel, "unknown"),
		fallback(s.Host.Architecture, "unknown"),
		cpuPct,
		memUsed,
		s.Timestamp.Format("15:04:05"),
	))
	a.cpuPanel.SetText(renderCPU(s.CPU))
	a.renderCores(s.CPU.Cores)
	a.renderProcesses(s)
	a.memoryPanel.SetText(renderMemory(s.Memory))
	a.thermalPanel.SetText(renderThermals(s))
	a.batteryPanel.SetText(renderBattery(s))
	a.gpuPanel.SetText(renderGPU(s.GPUs))
	msg := footerHint
	if !a.flashUntil.IsZero() && time.Now().Before(a.flashUntil) {
		msg = a.flashMsg
	}
	a.renderFooter(s, msg)
}

// renderProcesses populates the processTable. Right now it's a stub that
// shows a placeholder unless the collector implements a ProcessLister.
func (a *App) renderProcesses(s model.Snapshot) {
	selectedRow, _ := a.processTable.GetSelection()
	selectedPID := 0
	selectedStartTime := uint64(0)
	if selectedRow > 0 && selectedRow-1 < len(a.procList) {
		selectedPID = a.procList[selectedRow-1].PID
		selectedStartTime = a.procList[selectedRow-1].StartTime
	}
	a.processTable.Clear()
	a.procList = nil
	headers := []string{"PID", "Name", "Command", "CPU%", "MEM"}
	sortKeys := []string{"pid", "name", "", "cpu", "mem"}
	_, _, tableWidth, _ := a.processTable.GetInnerRect()
	showCommand := tableWidth == 0 || tableWidth >= 90
	commandWidth := 48
	if tableWidth > 0 {
		commandWidth = tableWidth - 68
		if commandWidth < 8 {
			commandWidth = 8
		}
		if commandWidth > 48 {
			commandWidth = 48
		}
	}
	if !showCommand {
		headers = []string{"PID", "Name", "CPU%", "MEM"}
		sortKeys = []string{"pid", "name", "cpu", "mem"}
	}
	for col, h := range headers {
		active := sortKeys[col] == a.procSort
		if active {
			arrow := "▼"
			if a.procSortAsc {
				arrow = "▲"
			}
			h += " " + arrow
		}
		header := headerCell(h, active)
		if col == 0 {
			header.SetMaxWidth(8).SetExpansion(1)
		} else if showCommand && col == 2 {
			header.SetMaxWidth(commandWidth).SetExpansion(1)
		} else if col == 3 || (!showCommand && col == 2) {
			header.SetMaxWidth(8).SetExpansion(1)
		} else if col == 4 || (!showCommand && col == 3) {
			header.SetMaxWidth(16).SetExpansion(1)
		}
		a.processTable.SetCell(0, col, header)
	}
	procs := s.Processes
	// If snapshot didn't include processes, try the collector directly as a
	// fallback (Collector provides Processes()). This helps if Snapshot
	// failed to populate processes for any reason.
	if len(procs) == 0 {
		if pl, ok := a.collector.(interface{ Processes() []model.Process }); ok {
			procs = pl.Processes()
		}
	}
	if len(procs) == 0 {
		a.processTable.SetCell(1, 0, cell("No process data available.", palette.muted, false))
		a.processTable.Select(0, 0)
		return
	}
	// Apply UI-side sorting based on a.procSort and a.procSortAsc. Copy first
	// so we never reorder the collector's shared slice.
	procs = append([]model.Process(nil), procs...)
	asc := a.procSortAsc
	switch a.procSort {
	case "cpu":
		sort.Slice(procs, func(i, j int) bool {
			if asc {
				return procs[i].CPUPercent < procs[j].CPUPercent
			}
			return procs[i].CPUPercent > procs[j].CPUPercent
		})
	case "pid":
		sort.Slice(procs, func(i, j int) bool {
			if asc {
				return procs[i].PID < procs[j].PID
			}
			return procs[i].PID > procs[j].PID
		})
	case "name":
		sort.Slice(procs, func(i, j int) bool {
			if asc {
				return strings.ToLower(procs[i].Name) < strings.ToLower(procs[j].Name)
			}
			return strings.ToLower(procs[i].Name) > strings.ToLower(procs[j].Name)
		})
	default: // mem
		sort.Slice(procs, func(i, j int) bool {
			if asc {
				return procs[i].RSSBytes < procs[j].RSSBytes
			}
			return procs[i].RSSBytes > procs[j].RSSBytes
		})
	}
	procs, treePrefixes := flattenProcessTree(procs)

	for i := range procs {
		p := procs[i]
		r := i + 1
		pidCell := cell(fmt.Sprintf("%d", p.PID), palette.text, false)
		pidCell.SetMaxWidth(8).SetExpansion(1)
		a.processTable.SetCell(r, 0, pidCell)
		nameCell := cell(treePrefixes[p.PID]+fallback(p.Name, "-"), palette.muted, false)
		nameCell.SetMaxWidth(28).SetExpansion(1)
		a.processTable.SetCell(r, 1, nameCell)
		column := 2
		if showCommand {
			commandCell := cell(truncateProcessText(p.Cmdline, commandWidth), palette.muted, false)
			commandCell.SetMaxWidth(commandWidth).SetExpansion(1)
			a.processTable.SetCell(r, column, commandCell)
			column++
		}
		cpuCell := cell(fmt.Sprintf("%.1f", p.CPUPercent), palette.text, false)
		cpuCell.SetMaxWidth(8).SetExpansion(1)
		a.processTable.SetCell(r, column, cpuCell)
		column++
		memCell := cell(bytes(p.RSSBytes), palette.text, false)
		memCell.SetMaxWidth(16).SetExpansion(1)
		a.processTable.SetCell(r, column, memCell)
	}
	a.procList = procs
	row := selectedProcessRow(procs, selectedPID, selectedStartTime)
	if a.procQuery != "" && !processMatches(procs, row-1, a.procQuery) {
		row = matchingProcessRow(procs, a.procQuery)
	}
	if row == 0 {
		row = validDataRow(selectedRow, len(procs))
	}
	a.processTable.Select(row, 0)
}

func processMatches(processes []model.Process, index int, query string) bool {
	if index < 0 || index >= len(processes) {
		return false
	}
	query = strings.ToLower(query)
	process := processes[index]
	return strings.Contains(strings.ToLower(fmt.Sprintf("%d", process.PID)), query) ||
		strings.Contains(strings.ToLower(process.Name), query) ||
		strings.Contains(strings.ToLower(process.Cmdline), query)
}

func matchingProcessRow(processes []model.Process, query string) int {
	for i := range processes {
		if processMatches(processes, i, query) {
			return i + 1
		}
	}
	return 0
}

func (a *App) selectNextProcessMatch() {
	if a.procQuery == "" || len(a.procList) == 0 {
		return
	}
	row, _ := a.processTable.GetSelection()
	start := row - 1
	for offset := 1; offset <= len(a.procList); offset++ {
		index := (start + offset) % len(a.procList)
		if processMatches(a.procList, index, a.procQuery) {
			a.processTable.Select(index+1, 0)
			return
		}
	}
}

func selectedProcessRow(processes []model.Process, pid int, startTime uint64) int {
	if pid <= 0 {
		return 0
	}
	for i, process := range processes {
		if process.PID == pid && (startTime == 0 || process.StartTime == startTime) {
			return i + 1
		}
	}
	return 0
}

func truncateProcessText(value string, max int) string {
	value = strings.TrimSpace(value)
	if len([]rune(value)) <= max {
		return value
	}
	runes := []rune(value)
	return string(runes[:max-1]) + "…"
}

func (a *App) cycleProcSort() {
	switch a.procSort {
	case "pid":
		a.procSort = "name"
	case "name":
		a.procSort = "cpu"
	case "cpu":
		a.procSort = "mem"
	default:
		a.procSort = "pid"
	}
	// Every column defaults to descending; press 's' to flip.
	a.procSortAsc = false
}

func (a *App) toggleProcSortDir() {
	a.procSortAsc = !a.procSortAsc
}

func (a *App) signalSelectedProcess(sig syscall.Signal) {
	if a.activeMiddle != "procs" {
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
	a.deliverSignal(p, sig)
}

func (a *App) deliverSignal(p model.Process, sig syscall.Signal) {
	name := fallback(p.Name, "-")
	if err := syscall.Kill(p.PID, sig); err != nil {
		a.flashFooter(fmt.Sprintf("[red]Failed to signal %d (%s): %v[-]", p.PID, name, err))
		return
	}
	a.flashFooter(fmt.Sprintf("[green]Sent %s to %d (%s).[-]", signalName(sig), p.PID, name))
}

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
	table.SetSelectedStyle(tcell.StyleDefault.Background(palette.selection).Foreground(palette.text))

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

func flattenProcessTree(processes []model.Process) ([]model.Process, map[int]string) {
	children := make(map[int][]model.Process)
	known := make(map[int]bool, len(processes))
	order := make(map[int]int, len(processes))
	for i, process := range processes {
		known[process.PID] = true
		order[process.PID] = i
		children[process.PPID] = append(children[process.PPID], process)
	}
	byOrder := func(items []model.Process) {
		sort.SliceStable(items, func(i, j int) bool { return order[items[i].PID] < order[items[j].PID] })
	}
	roots := make([]model.Process, 0)
	for _, process := range processes {
		if process.PPID <= 0 || !known[process.PPID] {
			roots = append(roots, process)
		}
	}
	byOrder(roots)
	for parent := range children {
		byOrder(children[parent])
	}
	flat := make([]model.Process, 0, len(processes))
	prefixes := make(map[int]string, len(processes))
	var visit func(model.Process, string, bool, bool)
	visit = func(process model.Process, prefix string, last, root bool) {
		branch := ""
		if !root {
			branch = "├─ "
			if last {
				branch = "└─ "
			}
		}
		prefixes[process.PID] = prefix + branch
		flat = append(flat, process)
		childPrefix := prefix
		if !root {
			if last {
				childPrefix += "   "
			} else {
				childPrefix += "│  "
			}
		}
		for i, child := range children[process.PID] {
			visit(child, childPrefix, i == len(children[process.PID])-1, false)
		}
	}
	for i, root := range roots {
		visit(root, "", i == len(roots)-1, true)
	}
	return flat, prefixes
}

func (a *App) toggleSelectedCore(ctx context.Context) {
	if a.controls == nil {
		a.flashFooter("[yellow]Core online control is not supported by this collector.[-]")
		return
	}
	row, _ := a.coreTable.GetSelection()
	if row <= 0 || row-1 >= len(a.last.CPU.Cores) {
		a.flashFooter("[yellow]Select a core row first.[-]")
		return
	}
	core := a.last.CPU.Cores[row-1]
	targetOnline := !core.Online
	action := "online"
	if !targetOnline {
		action = "offline"
	}
	if !a.controls.CanSetCoreOnline(core.ID) {
		a.flashFooter(fmt.Sprintf("[yellow]Core %d cannot be toggled on this system.[-]", core.ID))
		return
	}
	go a.applyControl(ctx, fmt.Sprintf("core %d %s", core.ID, action), func(runCtx context.Context) error {
		return a.controls.SetCoreOnline(runCtx, core.ID, targetOnline)
	})
}

func (a *App) toggleTurbo(ctx context.Context) {
	if a.controls == nil {
		a.flashFooter("[yellow]Turbo control is not supported by this collector.[-]")
		return
	}
	enabled := a.last.CPU.TurboEnabled != nil && *a.last.CPU.TurboEnabled
	target := !enabled
	go a.applyControl(ctx, fmt.Sprintf("turbo %s", onOff(target)), func(runCtx context.Context) error {
		return a.controls.SetTurboEnabled(runCtx, target)
	})
}

func (a *App) chooseGovernor(ctx context.Context) {
	if a.controls == nil {
		a.flashFooter("[yellow]Governor control is not supported by this collector.[-]")
		return
	}
	if len(a.last.CPU.Governors) == 0 {
		a.flashFooter("[yellow]No governors detected on this system.[-]")
		return
	}
	choice := nextValue(a.last.CPU.ActiveGov, a.last.CPU.Governors)
	go a.applyControl(ctx, fmt.Sprintf("governor %s", choice), func(runCtx context.Context) error {
		return a.controls.SetCPUGovernor(runCtx, choice)
	})
}

func (a *App) chooseEPP(ctx context.Context) {
	if a.controls == nil {
		a.flashFooter("[yellow]EPP control is not supported by this collector.[-]")
		return
	}
	options := a.last.CPU.EPPChoices
	if len(options) == 0 {
		a.flashFooter("[yellow]No EPP preferences are exposed by the CPU driver.[-]")
		return
	}
	choice := nextValue(a.last.CPU.EPP, options)
	go a.applyControl(ctx, fmt.Sprintf("EPP %s", choice), func(runCtx context.Context) error {
		return a.controls.SetEPP(runCtx, choice)
	})
}

func (a *App) choosePowerProfile(ctx context.Context) {
	if a.controls == nil {
		a.flashFooter("[yellow]Power profile control is not supported by this collector.[-]")
		return
	}
	choice := nextValue(a.last.CPU.PowerProfile, []string{"powersave", "balanced", "performance"})
	go a.applyControl(ctx, fmt.Sprintf("power mode %s", choice), func(runCtx context.Context) error {
		return a.controls.SetPowerProfile(runCtx, choice)
	})
}

func (a *App) applyControl(ctx context.Context, label string, fn func(context.Context) error) {
	err := fn(ctx)
	if err != nil {
		a.app.QueueUpdateDraw(func() {
			a.flashFooter(fmt.Sprintf("[red]Failed to set %s: %s[-]", label, compactControlError(err)))
		})
		return
	}
	a.app.QueueUpdateDraw(func() {
		a.flashFooter(fmt.Sprintf("[green]Set %s.[-]", label))
	})
	a.refresh(ctx)
}

func compactControlError(err error) string {
	message := strings.Join(strings.Fields(err.Error()), " ")
	const maxLength = 112
	if len(message) > maxLength {
		return message[:maxLength-3] + "..."
	}
	return message
}

func nextValue(current string, options []string) string {
	if len(options) == 0 {
		return current
	}
	for i, option := range options {
		if option == current {
			return options[(i+1)%len(options)]
		}
	}
	return options[0]
}

func onOff(enabled bool) string {
	if enabled {
		return "on"
	}
	return "off"
}

func coreTypeLabel(core model.CPUCore) string {
	switch core.Type {
	case model.CorePerformance:
		return "P-core"
	case model.CoreEfficiency:
		return "E-core"
	default:
		return ""
	}
}

func (a *App) renderFooter(s model.Snapshot, message string) {
	if message == "" {
		message = footerHint
	}
	if a.activeMiddle == "procs" && (a.procSearch || a.procQuery != "") {
		query := a.procQuery
		if query == "" {
			query = "_"
		}
		searchHint := fmt.Sprintf("[aqua]Search[-] %q", query)
		if a.procSearch {
			searchHint += "  [silver]Enter next  Esc clear[-]"
		} else {
			searchHint += "  [silver]/ edit  Esc clear[-]"
		}
		if message == footerHint {
			message = searchHint
		} else {
			message += "  " + searchHint
		}
	}
	a.status.SetText(message + "\n" + a.controlLegend(s))
}

// flashFooter shows a transient status message above the legend for two
// seconds before render reverts to the default hint.
func (a *App) flashFooter(message string) {
	a.flashMsg = message
	a.flashUntil = time.Now().Add(2 * time.Second)
	a.renderFooter(a.last, message)
}

func (a *App) controlLegend(s model.Snapshot) string {
	parts := make([]string, 0, 8)
	key := func(k, label string) string { return "[aqua]" + k + "[-] " + label }
	if a.controls != nil {
		if capabilityEnabled(s, "CPU governor") {
			parts = append(parts, key("g", "governor"))
		}
		if capabilityEnabled(s, "CPU EPP") {
			parts = append(parts, key("e", "epp"))
		}
		if capabilityEnabled(s, "CPU turbo") {
			parts = append(parts, key("t", "turbo"))
		}
		parts = append(parts, key("m", "mode"))
	}
	// Mode-specific controls: only advertise what the active view responds to.
	if a.activeMiddle == "procs" {
		parts = append(parts, key("/", "search"), key("c", "sort"), key("s", "dir"), key("⏎", "signal"), key("k", "term"), key("K", "kill"))
		parts = append(parts, key("p", "cores"))
	} else {
		if a.controls != nil {
			parts = append(parts, key("o", "core"))
		}
		parts = append(parts, key("p", "processes"))
	}
	parts = append(parts, key("r", "refresh"), key("q", "quit"))
	return "[silver]" + strings.Join(parts, "  ") + "[-]"
}

func renderControlsBox(s model.Snapshot, supported bool) string {
	governorCap := capabilityByName(s, "CPU governor")
	eppCap := capabilityByName(s, "CPU EPP")
	turboCap := capabilityByName(s, "CPU turbo")
	coreCap := capabilityByName(s, "CPU core online")
	modeCap := powerModeCapability(s)
	if !supported {
		lines := []string{
			"[white::b]Controls[-:-:-]",
			"",
			"[silver]mode   [-] " + fallback(s.CPU.PowerProfile, "unavailable"),
			"[silver]governor[-] " + controlValue(fallback(s.CPU.ActiveGov, "unavailable"), governorCap),
			"[silver]EPP    [-] " + controlValue(fallback(s.CPU.EPP, "unavailable"), eppCap),
			"[silver]turbo  [-] " + controlValue(boolPtr(s.CPU.TurboEnabled), turboCap),
			"[silver]core   [-] " + controlValue(coreControlSummary(s), coreCap),
			"",
			"[silver]No writable control backend is available.[-]",
		}
		return strings.Join(lines, "\n")
	}
	lines := []string{
		"[white::b]Controls[-:-:-]",
		"",
		"[silver]mode   [-] " + fallback(s.CPU.PowerProfile, "unavailable"),
		"[silver]governor[-] " + controlValue(fallback(s.CPU.ActiveGov, "unavailable"), governorCap),
		"[silver]EPP    [-] " + controlValue(fallback(s.CPU.EPP, "unavailable"), eppCap),
		"[silver]turbo  [-] " + controlValue(boolPtr(s.CPU.TurboEnabled), turboCap),
		"[silver]core   [-] " + controlValue(coreControlSummary(s), coreCap),
		"",
		"[silver]g[-] cycle governor" + capabilityState(governorCap),
		"[silver]e[-] cycle EPP" + capabilityState(eppCap),
		"[silver]t[-] toggle turbo" + capabilityState(turboCap),
		"[silver]m[-] cycle power mode" + capabilityState(modeCap),
		"[silver]o[-] toggle selected core" + capabilityState(coreCap),
		"",
		"[silver]Topology and capability scope come from kernel topology and sysfs target discovery.[-]",
	}
	return strings.Join(lines, "\n")
}

func capabilityByName(s model.Snapshot, name string) *model.Capability {
	for i := range s.Capabilities {
		if s.Capabilities[i].Name == name {
			return &s.Capabilities[i]
		}
	}
	return nil
}

func capabilityEnabled(s model.Snapshot, name string) bool {
	cap := capabilityByName(s, name)
	return cap != nil && cap.Status != model.CapabilityUnavailable
}

func powerModeCapability(s model.Snapshot) *model.Capability {
	if cap := capabilityByName(s, "Power profile"); cap != nil {
		return cap
	}
	if capabilityEnabled(s, "CPU governor") || capabilityEnabled(s, "CPU EPP") || capabilityEnabled(s, "CPU turbo") {
		return &model.Capability{
			Name:    "Power profile",
			Status:  model.CapabilityConditional,
			Scope:   "system",
			Targets: 1,
		}
	}
	return nil
}

func capabilityState(cap *model.Capability) string {
	if cap == nil {
		return "  [yellow]unavailable[-]"
	}
	label := statusLabel(cap.Status)
	if cap.Targets > 0 {
		return fmt.Sprintf("  %s [silver](%d target%s)[-]", label, cap.Targets, plural(cap.Targets))
	}
	return "  " + label
}

func statusLabel(status model.CapabilityStatus) string {
	switch status {
	case model.CapabilityAvailable:
		return "[green]available[-]"
	case model.CapabilityConditional:
		return "[yellow]conditional[-]"
	default:
		return "[yellow]unavailable[-]"
	}
}

func controlValue(value string, cap *model.Capability) string {
	if cap == nil {
		return value
	}
	switch cap.Status {
	case model.CapabilityAvailable:
		return value
	case model.CapabilityConditional:
		return "[yellow]" + value + "[-]"
	default:
		return "[silver]" + value + "[-]"
	}
}

func coreControlSummary(s model.Snapshot) string {
	cap := capabilityByName(s, "CPU core online")
	if cap == nil {
		return "unavailable"
	}
	return fmt.Sprintf("%s / %d online", statusLabel(cap.Status), onlineCores(s.CPU.Cores))
}

func topologySummary(top model.CPUTopology) string {
	if !top.Known {
		return "unavailable"
	}
	parts := make([]string, 0, 5)
	if top.Packages > 0 {
		parts = append(parts, fmt.Sprintf("%d pkg", top.Packages))
	}
	if top.NUMANodes > 0 {
		parts = append(parts, fmt.Sprintf("%d numa", top.NUMANodes))
	}
	if top.PhysicalCores > 0 {
		parts = append(parts, fmt.Sprintf("%d phys", top.PhysicalCores))
	}
	if top.LogicalCores > 0 {
		parts = append(parts, fmt.Sprintf("%d log", top.LogicalCores))
	}
	if top.ThreadsPerCore > 0 {
		parts = append(parts, fmt.Sprintf("%.1f tpc", top.ThreadsPerCore))
	}
	if len(parts) == 0 {
		return "known"
	}
	return strings.Join(parts, " / ")
}

func topologyLabel(prefix string, id int) string {
	if id < 0 {
		return "-"
	}
	return fmt.Sprintf("%s%d", prefix, id)
}

func plural(count int) string {
	if count == 1 {
		return ""
	}
	return "s"
}

func renderCPU(cpu model.CPU) string {
	// Shorten the model string for compact display
	short := shortCPUName(fallback(cpu.Model, "CPU"))
	// compute average frequency across cores with known frequency
	var sum int
	var count int
	for _, c := range cpu.Cores {
		if c.FrequencyMHz > 0 {
			sum += c.FrequencyMHz
			count++
		}
	}
	avgFreq := 0
	if count > 0 {
		avgFreq = sum / count
	}

	// Show summary top: cores, freq, temp, power (if present), then usage.
	lines := []string{
		fmt.Sprintf("[white::b]%s[-:-:-]", short),
		fmt.Sprintf("[silver]cores [-] %d online / %d total", onlineCores(cpu.Cores), len(cpu.Cores)),
		fmt.Sprintf("[silver]freq [-] %s", freq(avgFreq)),
		fmt.Sprintf("[silver]temp [-] %s", floatPtr(cpu.TemperatureC, "C")),
	}
	if cpu.PowerW != nil {
		lines = append(lines, fmt.Sprintf("[silver]power[-] %s", floatPtr(cpu.PowerW, "W")))
	}
	lines = append(lines,
		fmt.Sprintf("[silver]usage [-] %s %5.1f%%", bar(cpu.UsagePercent, 14), cpu.UsagePercent),
		fmt.Sprintf("[silver]hybrid[-] %s", yesNo(cpu.HybridKnown && cpu.Hybrid)),
		fmt.Sprintf("[silver]mode[-] %s", fallback(cpu.PowerProfile, "unavailable")),
		fmt.Sprintf("[silver]governor[-] %s", fallback(cpu.ActiveGov, "unavailable")),
		fmt.Sprintf("[silver]epp   [-] %s", fallback(cpu.EPP, "unavailable")),
		fmt.Sprintf("[silver]turbo [-] %s", boolPtr(cpu.TurboEnabled)),
	)
	return strings.Join(lines, "\n")
}

// shortCPUName returns a compact display name for a CPU model string by
// removing vendor marketing suffixes like "with ... Graphics" and trimming
// excessive whitespace.
func shortCPUName(in string) string {
	// remove common segments that refer to integrated graphics
	markers := []string{" with ", " with Integrated Graphics", " Graphics", " GPU"}
	out := in
	for _, m := range markers {
		if idx := strings.Index(strings.ToLower(out), strings.ToLower(m)); idx >= 0 {
			out = strings.TrimSpace(out[:idx])
			break
		}
	}
	// collapse multiple spaces
	out = strings.Join(strings.Fields(out), " ")
	return out
}

func (a *App) renderCores(cores []model.CPUCore) {
	selectedRow, _ := a.coreTable.GetSelection()
	a.coreTable.Clear()
	showType := a.last.CPU.HybridKnown
	headers := []string{"ID"}
	if showType {
		headers = append(headers, "Type")
	}
	headers = append(headers, "On", "Usage", "Freq", "Governor", "EPP")
	for col, h := range headers {
		a.coreTable.SetCell(0, col, cell(h, palette.accent, true).SetSelectable(false))
	}
	for row, core := range cores {
		r := row + 1
		a.coreTable.SetCell(r, 0, cell(fmt.Sprintf("%02d", core.ID), palette.text, false))
		col := 1
		if showType {
			a.coreTable.SetCell(r, col, cell(coreTypeLabel(core), coreColor(core.Type), false))
			col++
		}
		a.coreTable.SetCell(r, col, cell(online(core.Online), onlineColor(core.Online), false))
		col++
		a.coreTable.SetCell(r, col, cell(fmt.Sprintf("%s %4.1f%%", bar(core.UsagePercent, 10), core.UsagePercent), palette.text, false))
		col++
		a.coreTable.SetCell(r, col, cell(freq(core.FrequencyMHz), palette.text, false))
		col++
		a.coreTable.SetCell(r, col, cell(fallback(core.Governor, "-"), palette.muted, false))
		col++
		a.coreTable.SetCell(r, col, cell(fallback(core.EPP, "-"), palette.muted, false))
	}
	if len(cores) > 0 {
		a.coreTable.Select(validDataRow(selectedRow, len(cores)), 0)
	} else {
		a.coreTable.Select(0, 0)
	}
}

func validDataRow(row, dataRows int) int {
	if dataRows == 0 {
		return 0
	}
	if row < 1 || row > dataRows {
		return 1
	}
	return row
}

func renderMemory(mem model.Memory) string {
	usedPct := percent(mem.UsedBytes, mem.TotalBytes)
	swapPct := percent(mem.SwapUsedBytes, mem.SwapTotalBytes)
	// Note: detailed DIMM/module metadata (count, speed, vendor) is not
	// available in the current Memory model. Show unavailable where missing
	// and prioritise a clear header + metadata block followed by usage.
	lines := []string{
		fmt.Sprintf("[white::b]%s[-:-:-]", "Memory"),
		fmt.Sprintf("[silver]total [-] %s", bytes(mem.TotalBytes)),
	}
	if mem.ModuleCount != nil {
		lines = append(lines, fmt.Sprintf("[silver]modules[-] %d", *mem.ModuleCount))
	}
	if mem.SpeedMHz != nil {
		lines = append(lines, fmt.Sprintf("[silver]speed [-] %d MHz", *mem.SpeedMHz))
	}
	lines = append(lines,
		"",
		fmt.Sprintf("[silver]usage [-] %s %5.1f%%", bar(usedPct, 14), usedPct),
		fmt.Sprintf("[white]%s[-] used of %s", bytes(mem.UsedBytes), bytes(mem.TotalBytes)),
		"",
		fmt.Sprintf("[silver]swap [-] %s %5.1f%%", bar(swapPct, 14), swapPct),
		fmt.Sprintf("[white]%s[-] used of %s", bytes(mem.SwapUsedBytes), bytes(mem.SwapTotalBytes)),
		"",
	)
	// PSI removed: no pressure information displayed.
	return strings.Join(lines, "\n")
}

func renderThermals(s model.Snapshot) string {
	if len(s.Thermals) == 0 {
		return "[silver]No thermal sensors detected.[-]\n\n[silver]Thermal zones (hwmon/thermal) are used when the kernel exposes them.[-]"
	}
	lines := make([]string, 0, len(s.Thermals)+1)
	lines = append(lines, "[white::b]Thermals[-:-:-]")
	var coreMax *float64
	for _, sensor := range s.Thermals {
		lower := strings.ToLower(sensor.Name)
		if strings.HasPrefix(lower, "coretemp: core ") {
			if sensor.TemperatureC != nil && (coreMax == nil || *sensor.TemperatureC > *coreMax) {
				value := *sensor.TemperatureC
				coreMax = &value
			}
			continue
		}
		name := sensor.Name
		if strings.HasPrefix(lower, "coretemp: package") {
			name = "CPU package"
		} else if strings.HasPrefix(lower, "nvme: ") {
			name = "NVMe " + sensor.Name[len("nvme: "):]
		}
		lines = append(lines, fmt.Sprintf("[white]%s[-] %s", name, floatPtr(sensor.TemperatureC, "C")))
	}
	if coreMax != nil {
		lines = append(lines, fmt.Sprintf("[white]CPU cores max[-] %.1fC", *coreMax))
	}
	return strings.Join(lines, "\n")
}

func renderBattery(s model.Snapshot) string {
	if len(s.Batteries) == 0 && s.ACOnline == nil {
		return "[silver]No battery or AC telemetry detected.[-]\n\n[silver]power_supply is used when the kernel exposes it.[-]"
	}
	lines := make([]string, 0, 8)
	lines = append(lines, "[white::b]Battery[-:-:-]")
	if len(s.Batteries) == 0 {
		lines = append(lines, fmt.Sprintf("[silver]AC[-] %s", acState(s.ACOnline)))
	} else {
		for _, batt := range s.Batteries {
			lines = append(lines, fmt.Sprintf("[silver]AC[-] %s  [silver]flow[-] %s  [silver]rate[-] %s  [silver]v[-] %s",
				acState(s.ACOnline),
				batteryFlow(batt.Status, batt.PowerW),
				batteryRate(batt.PowerW, batt.Status, "W"),
				voltageValue(batt.VoltageV),
			))
			lines = append(lines, fmt.Sprintf("[silver]energy[-] %s/%s",
				floatPtr(batt.EnergyNowWh, "Wh"),
				floatPtr(batt.EnergyFullWh, "Wh"),
			))
		}
	}
	return strings.Join(lines, "\n")
}

func renderGPU(gpus []model.GPU) string {
	if len(gpus) == 0 {
		return "[silver]No DRM GPU devices detected.[-]\n\n[silver]AMD/Intel metrics can be added through DRM/hwmon backends; NVIDIA can use an optional nvidia-smi backend.[-]"
	}
	lines := make([]string, 0, len(gpus)*2)
	for _, gpu := range gpus {
		// First line: show the product name if available, otherwise the vendor.
		lines = append(lines, fmt.Sprintf("[white::b]%s[-:-:-]", fallback(gpu.Name, gpu.Vendor)))
		if gpu.Driver != "" {
			lines = append(lines, fmt.Sprintf("[silver]driver[-] %s", gpu.Driver))
		}
		if gpu.PCIID != "" {
			lines = append(lines, fmt.Sprintf("[silver]PCI[-] %s", gpu.PCIID))
		}
		if len(gpu.Outputs) > 0 {
			lines = append(lines, fmt.Sprintf("[silver]outputs[-] %s", strings.Join(gpu.Outputs, ", ")))
		}
		metrics := make([]string, 0, 2)
		if gpu.TemperatureC != nil {
			metrics = append(metrics, fmt.Sprintf("[silver]temp[-] %.1fC", *gpu.TemperatureC))
		}
		if gpu.PowerW != nil {
			metrics = append(metrics, fmt.Sprintf("[silver]power[-] %.2fW", *gpu.PowerW))
		}
		if len(metrics) > 0 {
			lines = append(lines, strings.Join(metrics, "   "))
		}
	}
	return strings.Join(lines, "\n")
}

func acState(value *bool) string {
	if value == nil {
		return "unavailable"
	}
	if *value {
		return "online"
	}
	return "offline"
}

func powerValue(value *float64, suffix string) string {
	if value == nil {
		return "unavailable"
	}
	return fmt.Sprintf("%.2f%s", *value, suffix)
}

func batteryFlow(status string, power *float64) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "charging":
		return "charging"
	case "discharging":
		return "discharging"
	case "full", "not charging":
		return "idle"
	default:
		return fallback(status, "unknown")
	}
}

func voltageValue(value *float64) string {
	if value == nil {
		return "unavailable"
	}
	return fmt.Sprintf("%.2fV", *value)
}

// batteryRate returns the battery power value with an explicit + for charging
// and - for discharging. If status is unknown or the value is nil it falls
// back to "unavailable" or a plain value.
func batteryRate(value *float64, status string, suffix string) string {
	if value == nil {
		return "unavailable"
	}
	s := strings.ToLower(strings.TrimSpace(status))
	sign := ""
	switch s {
	case "charging":
		sign = "+"
	case "discharging":
		sign = "-"
	}
	return fmt.Sprintf("%s%.2f%s", sign, *value, suffix)
}

func headerCell(text string, active bool) *tview.TableCell {
	color := palette.muted
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

func bar(value float64, width int) string {
	if value < 0 {
		value = 0
	}
	if value > 100 {
		value = 100
	}
	blocks := []rune{' ', '▏', '▎', '▍', '▌', '▋', '▊', '▉', '█'}
	totalUnits := value / 100 * float64(width*8)
	full := int(totalUnits) / 8
	remainder := int(totalUnits) % 8
	if full > width {
		full = width
		remainder = 0
	}
	color := "green"
	if value >= 85 {
		color = "red"
	} else if value >= 65 {
		color = "yellow"
	}
	var b strings.Builder
	b.Grow(width + 16)
	b.WriteString("[" + color + "]")
	for i := 0; i < full; i++ {
		b.WriteRune('█')
	}
	if remainder > 0 && full < width {
		b.WriteRune(blocks[remainder])
		full++
	}
	if full < width {
		b.WriteString("[gray]")
		b.WriteString(strings.Repeat("░", width-full))
	}
	b.WriteString("[-]")
	return b.String()
}

func percent(used, total uint64) float64 {
	if total == 0 {
		return 0
	}
	return float64(used) * 100 / float64(total)
}

func bytes(v uint64) string {
	const unit = 1024
	if v < unit {
		return fmt.Sprintf("%d B", v)
	}
	div, exp := uint64(unit), 0
	for n := v / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(v)/float64(div), "KMGTPE"[exp])
}

// bytesGB formats bytes as GiB with one decimal place (e.g., "3.2 GiB").
func bytesGB(v uint64) string {
	gib := float64(v) / 1024.0 / 1024.0 / 1024.0
	return fmt.Sprintf("%.1f GiB", gib)
}

func onlineCores(cores []model.CPUCore) int {
	total := 0
	for _, core := range cores {
		if core.Online {
			total++
		}
	}
	return total
}

func fallback(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

func boolPtr(value *bool) string {
	if value == nil {
		return "unavailable"
	}
	if *value {
		return "[green]enabled[-]"
	}
	return "[yellow]disabled[-]"
}

func floatPtr(value *float64, suffix string) string {
	if value == nil {
		return "unavailable"
	}
	return fmt.Sprintf("%.1f%s", *value, suffix)
}

func freq(mhz int) string {
	if mhz == 0 {
		return "-"
	}
	return fmt.Sprintf("%d MHz", mhz)
}

func online(value bool) string {
	if value {
		return "online"
	}
	return "offline"
}

func onlineColor(value bool) tcell.Color {
	if value {
		return palette.good
	}
	return palette.warn
}

func coreType(value model.CoreType) string {
	switch value {
	case model.CorePerformance:
		return "P-core"
	case model.CoreEfficiency:
		return "E-core"
	default:
		return "-"
	}
}

func coreColor(value model.CoreType) tcell.Color {
	switch value {
	case model.CorePerformance:
		return palette.accent
	case model.CoreEfficiency:
		return palette.good
	default:
		return palette.muted
	}
}

func capabilityColor(status model.CapabilityStatus) tcell.Color {
	switch status {
	case model.CapabilityAvailable:
		return palette.good
	case model.CapabilityConditional:
		return palette.warn
	default:
		return palette.bad
	}
}
