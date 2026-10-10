package tui

import (
	"context"
	"fmt"
	"github.com/aayushkdev/perfmon/internal/backend"
	"github.com/aayushkdev/perfmon/internal/model"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"syscall"
	"time"
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
	table.SetSelectedStyle(tcell.StyleDefault.Background(palette.selection).Foreground(palette.selectionText))
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
