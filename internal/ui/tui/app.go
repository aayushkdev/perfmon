package tui

import (
	"context"
	"fmt"
	"sort"
	"strings"
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
	activeMiddle string // "cores" or "procs"
	procSort     string // "mem", "cpu", "pid", "name"
	procSortAsc  bool   // sort ascending when true, descending when false
}

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
			// toggle between cores and processes
			if a.activeMiddle == "cores" {
				a.switchMiddle("procs")
			} else {
				a.switchMiddle("cores")
			}
			return nil
		case 'c', 'C':
			// cycle process sort order (mem -> cpu -> pid -> name)
			a.cycleProcSort()
			return nil
		case 's', 'S':
			// toggle ascending/descending for the current sort column
			a.toggleProcSortDir()
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
	a.status = textPanel("", false)

	a.coreTable = tablePanel("Cores")
	a.processTable = tablePanel("Processes")
	// processTable placeholder will be populated when rendering
	a.middlePages = tview.NewPages()
	a.middlePages.AddPage("cores", a.coreTable, true, true)
	a.middlePages.AddPage("procs", a.processTable, true, false)
	a.activeMiddle = "cores"
	a.root = tview.NewGrid().
		SetRows(3, 0, 9, 2).
		SetColumns(32, 0, 32).
		SetBorders(false).
		AddItem(a.header, 0, 0, 1, 3, 0, 0, false).
		AddItem(a.cpuPanel, 1, 0, 1, 1, 0, 0, false).
		AddItem(a.middlePages, 1, 1, 1, 1, 0, 0, true).
		AddItem(a.memoryPanel, 1, 2, 1, 1, 0, 0, false).
		AddItem(a.gpuPanel, 2, 0, 1, 1, 0, 0, false).
		AddItem(a.thermalPanel, 2, 1, 1, 1, 0, 0, false).
		AddItem(a.batteryPanel, 2, 2, 1, 1, 0, 0, false).
		AddItem(a.status, 3, 0, 1, 3, 0, 0, false)
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
			a.status.SetText(fmt.Sprintf("[red]collector error:[white] %v", err))
			return
		}
		a.render(snap)
	})
}

func (a *App) initialRender(ctx context.Context) {
	snap, err := a.collector.Snapshot(ctx)
	if err != nil {
		a.status.SetText(fmt.Sprintf("[red]collector error:[white] %v", err))
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
		"[#7dd3fc::b]perfmon[-:-:-]  [#94a3b8]kernel %s  arch %s  cpu %s  mem %s  updated %s[-]",
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
	a.renderFooter(s, "[#64748b]Select a core row, then use the key actions in the footer. Writes go through /sys and may require privileges.[-]")
}

// renderProcesses populates the processTable. Right now it's a stub that
// shows a placeholder unless the collector implements a ProcessLister.
func (a *App) renderProcesses(s model.Snapshot) {
	a.processTable.Clear()
	headers := []string{"PID", "Name", "CPU%", "MEM"}
	sortKeys := []string{"pid", "name", "cpu", "mem"}
	for col, h := range headers {
		active := sortKeys[col] == a.procSort
		if active {
			arrow := "▼"
			if a.procSortAsc {
				arrow = "▲"
			}
			h += " " + arrow
		}
		a.processTable.SetCell(0, col, headerCell(h, active))
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

	for i := range procs {
		p := procs[i]
		r := i + 1
		a.processTable.SetCell(r, 0, cell(fmt.Sprintf("%d", p.PID), palette.text, false))
		a.processTable.SetCell(r, 1, cell(fallback(p.Name, "-"), palette.muted, false))
		a.processTable.SetCell(r, 2, cell(fmt.Sprintf("%.1f", p.CPUPercent), palette.text, false))
		a.processTable.SetCell(r, 3, cell(bytes(p.RSSBytes), palette.text, false))
	}
}

func (a *App) cycleProcSort() {
	switch a.procSort {
	case "mem":
		a.procSort = "cpu"
	case "cpu":
		a.procSort = "pid"
	case "pid":
		a.procSort = "name"
	default:
		a.procSort = "mem"
	}
	// Every column defaults to descending; press 's' to flip.
	a.procSortAsc = false
}

func (a *App) toggleProcSortDir() {
	a.procSortAsc = !a.procSortAsc
}

func (a *App) toggleSelectedCore(ctx context.Context) {
	if a.controls == nil {
		a.status.SetText("[#f59e0b]Core online control is not supported by this collector.[-]")
		return
	}
	row, _ := a.coreTable.GetSelection()
	if row <= 0 || row-1 >= len(a.last.CPU.Cores) {
		a.status.SetText("[#f59e0b]Select a core row first.[-]")
		return
	}
	core := a.last.CPU.Cores[row-1]
	targetOnline := !core.Online
	action := "online"
	if !targetOnline {
		action = "offline"
	}
	if !a.controls.CanSetCoreOnline(core.ID) {
		a.status.SetText(fmt.Sprintf("[#f59e0b]Core %d cannot be toggled on this system.[-]", core.ID))
		return
	}
	go a.applyControl(ctx, fmt.Sprintf("core %d %s", core.ID, action), func(runCtx context.Context) error {
		return a.controls.SetCoreOnline(runCtx, core.ID, targetOnline)
	})
}

func (a *App) toggleTurbo(ctx context.Context) {
	if a.controls == nil {
		a.status.SetText("[#f59e0b]Turbo control is not supported by this collector.[-]")
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
		a.status.SetText("[#f59e0b]Governor control is not supported by this collector.[-]")
		return
	}
	if len(a.last.CPU.Governors) == 0 {
		a.status.SetText("[#f59e0b]No governors detected on this system.[-]")
		return
	}
	choice := nextValue(a.last.CPU.ActiveGov, a.last.CPU.Governors)
	go a.applyControl(ctx, fmt.Sprintf("governor %s", choice), func(runCtx context.Context) error {
		return a.controls.SetCPUGovernor(runCtx, choice)
	})
}

func (a *App) chooseEPP(ctx context.Context) {
	if a.controls == nil {
		a.status.SetText("[#f59e0b]EPP control is not supported by this collector.[-]")
		return
	}
	options := a.last.CPU.EPPChoices
	if len(options) == 0 {
		options = []string{"performance", "balance_performance", "balance_power", "power"}
	}
	choice := nextValue(a.last.CPU.EPP, options)
	go a.applyControl(ctx, fmt.Sprintf("EPP %s", choice), func(runCtx context.Context) error {
		return a.controls.SetEPP(runCtx, choice)
	})
}

func (a *App) choosePowerProfile(ctx context.Context) {
	if a.controls == nil {
		a.status.SetText("[#f59e0b]Power profile control is not supported by this collector.[-]")
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
			a.renderFooter(a.last, fmt.Sprintf("[#ef4444]Failed to set %s: %v[-]", label, err))
		})
		return
	}
	a.app.QueueUpdateDraw(func() {
		a.renderFooter(a.last, fmt.Sprintf("[#22c55e]Set %s.[-]", label))
	})
	a.initialRender(ctx)
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
	legend := a.controlLegend(s)
	if message == "" {
		a.status.SetText(legend)
		return
	}
	a.status.SetText(message + "\n" + legend)
}

func (a *App) controlLegend(s model.Snapshot) string {
	parts := make([]string, 0, 6)
	if a.controls != nil {
		if capabilityEnabled(s, "CPU governor") {
			parts = append(parts, "[#7dd3fc]g[-] governor")
		}
		if capabilityEnabled(s, "CPU EPP") {
			parts = append(parts, "[#7dd3fc]e[-] epp")
		}
		if capabilityEnabled(s, "CPU turbo") {
			parts = append(parts, "[#7dd3fc]t[-] turbo")
		}
		parts = append(parts, "[#7dd3fc]m[-] mode")
		parts = append(parts, "[#7dd3fc]o[-] core")
		// UI-side controls
		parts = append(parts, "[#7dd3fc]p[-] toggle cores/procs")
		parts = append(parts, "[#7dd3fc]c[-] cycle proc sort")
		parts = append(parts, "[#7dd3fc]s[-] sort dir")
	}
	parts = append(parts, "[#7dd3fc]r[-] refresh", "[#7dd3fc]q[-] quit")
	return "[#94a3b8]" + strings.Join(parts, "  ") + "[-]"
}

func renderControlsBox(s model.Snapshot, supported bool) string {
	governorCap := capabilityByName(s, "CPU governor")
	eppCap := capabilityByName(s, "CPU EPP")
	turboCap := capabilityByName(s, "CPU turbo")
	coreCap := capabilityByName(s, "CPU core online")
	modeCap := powerModeCapability(s)
	if !supported {
		lines := []string{
			"[#e2e8f0::b]Controls[-:-:-]",
			"",
			"[#94a3b8]mode   [-] " + fallback(s.CPU.PowerProfile, "unavailable"),
			"[#94a3b8]governor[-] " + controlValue(fallback(s.CPU.ActiveGov, "unavailable"), governorCap),
			"[#94a3b8]EPP    [-] " + controlValue(fallback(s.CPU.EPP, "unavailable"), eppCap),
			"[#94a3b8]turbo  [-] " + controlValue(boolPtr(s.CPU.TurboEnabled), turboCap),
			"[#94a3b8]core   [-] " + controlValue(coreControlSummary(s), coreCap),
			"",
			"[#64748b]No writable control backend is available.[-]",
		}
		return strings.Join(lines, "\n")
	}
	lines := []string{
		"[#e2e8f0::b]Controls[-:-:-]",
		"",
		"[#94a3b8]mode   [-] " + fallback(s.CPU.PowerProfile, "unavailable"),
		"[#94a3b8]governor[-] " + controlValue(fallback(s.CPU.ActiveGov, "unavailable"), governorCap),
		"[#94a3b8]EPP    [-] " + controlValue(fallback(s.CPU.EPP, "unavailable"), eppCap),
		"[#94a3b8]turbo  [-] " + controlValue(boolPtr(s.CPU.TurboEnabled), turboCap),
		"[#94a3b8]core   [-] " + controlValue(coreControlSummary(s), coreCap),
		"",
		"[#94a3b8]g[-] cycle governor" + capabilityState(governorCap),
		"[#94a3b8]e[-] cycle EPP" + capabilityState(eppCap),
		"[#94a3b8]t[-] toggle turbo" + capabilityState(turboCap),
		"[#94a3b8]m[-] cycle power mode" + capabilityState(modeCap),
		"[#94a3b8]o[-] toggle selected core" + capabilityState(coreCap),
		"",
		"[#64748b]Topology and capability scope come from kernel topology and sysfs target discovery.[-]",
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
		return "  [#f59e0b]unavailable[-]"
	}
	label := statusLabel(cap.Status)
	if cap.Targets > 0 {
		return fmt.Sprintf("  %s [#64748b](%d target%s)[-]", label, cap.Targets, plural(cap.Targets))
	}
	return "  " + label
}

func statusLabel(status model.CapabilityStatus) string {
	switch status {
	case model.CapabilityAvailable:
		return "[#22c55e]available[-]"
	case model.CapabilityConditional:
		return "[#f59e0b]conditional[-]"
	default:
		return "[#f59e0b]unavailable[-]"
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
		return "[#f59e0b]" + value + "[-]"
	default:
		return "[#64748b]" + value + "[-]"
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
		fmt.Sprintf("[#e2e8f0::b]%s[-:-:-]", short),
		fmt.Sprintf("[#94a3b8]cores [-] %d online / %d total", onlineCores(cpu.Cores), len(cpu.Cores)),
		fmt.Sprintf("[#94a3b8]freq [-] %s", freq(avgFreq)),
		fmt.Sprintf("[#94a3b8]temp [-] %s", floatPtr(cpu.TemperatureC, "C")),
	}
	if cpu.PowerW != nil {
		lines = append(lines, fmt.Sprintf("[#94a3b8]power[-] %s", floatPtr(cpu.PowerW, "W")))
	}
	lines = append(lines,
		fmt.Sprintf("[#94a3b8]usage [-] %s %5.1f%%", bar(cpu.UsagePercent, 14), cpu.UsagePercent),
		fmt.Sprintf("[#94a3b8]hybrid[-] %s", yesNo(cpu.HybridKnown && cpu.Hybrid)),
		fmt.Sprintf("[#94a3b8]mode[-] %s", fallback(cpu.PowerProfile, "unavailable")),
		fmt.Sprintf("[#94a3b8]governor[-] %s", fallback(cpu.ActiveGov, "unavailable")),
		fmt.Sprintf("[#94a3b8]epp   [-] %s", fallback(cpu.EPP, "unavailable")),
		fmt.Sprintf("[#94a3b8]turbo [-] %s", boolPtr(cpu.TurboEnabled)),
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
	a.coreTable.Clear()
	showType := a.last.CPU.HybridKnown
	headers := []string{"ID"}
	if showType {
		headers = append(headers, "Type")
	}
	headers = append(headers, "On", "Usage", "Freq", "Governor", "EPP")
	for col, h := range headers {
		a.coreTable.SetCell(0, col, cell(h, palette.accent, true))
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
}

func renderMemory(mem model.Memory) string {
	usedPct := percent(mem.UsedBytes, mem.TotalBytes)
	swapPct := percent(mem.SwapUsedBytes, mem.SwapTotalBytes)
	// Note: detailed DIMM/module metadata (count, speed, vendor) is not
	// available in the current Memory model. Show unavailable where missing
	// and prioritise a clear header + metadata block followed by usage.
	lines := []string{
		fmt.Sprintf("[#e2e8f0::b]%s[-:-:-]", "Memory"),
		fmt.Sprintf("[#94a3b8]total [-] %s", bytes(mem.TotalBytes)),
	}
	if mem.ModuleCount != nil {
		lines = append(lines, fmt.Sprintf("[#94a3b8]modules[-] %d", *mem.ModuleCount))
	}
	if mem.SpeedMHz != nil {
		lines = append(lines, fmt.Sprintf("[#94a3b8]speed [-] %d MHz", *mem.SpeedMHz))
	}
	lines = append(lines,
		"",
		fmt.Sprintf("[#94a3b8]usage [-] %s %5.1f%%", bar(usedPct, 14), usedPct),
		fmt.Sprintf("[#e2e8f0]%s[-] used of %s", bytes(mem.UsedBytes), bytes(mem.TotalBytes)),
		"",
		fmt.Sprintf("[#94a3b8]swap [-] %s %5.1f%%", bar(swapPct, 14), swapPct),
		fmt.Sprintf("[#e2e8f0]%s[-] used of %s", bytes(mem.SwapUsedBytes), bytes(mem.SwapTotalBytes)),
		"",
	)
	// PSI removed: no pressure information displayed.
	return strings.Join(lines, "\n")
}

func renderThermals(s model.Snapshot) string {
	if len(s.Thermals) == 0 {
		return "[#94a3b8]No thermal sensors detected.[-]\n\n[#64748b]Thermal zones (hwmon/thermal) are used when the kernel exposes them.[-]"
	}
	lines := make([]string, 0, len(s.Thermals)+1)
	lines = append(lines, "[#e2e8f0::b]Thermals[-:-:-]")
	for _, sensor := range s.Thermals {
		lines = append(lines, fmt.Sprintf("[#e2e8f0]%s[-] %s", sensor.Name, floatPtr(sensor.TemperatureC, "C")))
	}
	return strings.Join(lines, "\n")
}

func renderBattery(s model.Snapshot) string {
	if len(s.Batteries) == 0 && s.ACOnline == nil {
		return "[#94a3b8]No battery or AC telemetry detected.[-]\n\n[#64748b]power_supply is used when the kernel exposes it.[-]"
	}
	lines := make([]string, 0, 8)
	lines = append(lines, "[#e2e8f0::b]Battery[-:-:-]")
	if len(s.Batteries) == 0 {
		lines = append(lines, fmt.Sprintf("[#94a3b8]AC[-] %s", acState(s.ACOnline)))
	} else {
		for _, batt := range s.Batteries {
			lines = append(lines, fmt.Sprintf("[#94a3b8]AC[-] %s  [#94a3b8]flow[-] %s  [#94a3b8]rate[-] %s  [#94a3b8]v[-] %s",
				acState(s.ACOnline),
				batteryFlow(batt.Status, batt.PowerW),
				batteryRate(batt.PowerW, batt.Status, "W"),
				voltageValue(batt.VoltageV),
			))
			lines = append(lines, fmt.Sprintf("[#94a3b8]energy[-] %s/%s",
				floatPtr(batt.EnergyNowWh, "Wh"),
				floatPtr(batt.EnergyFullWh, "Wh"),
			))
		}
	}
	return strings.Join(lines, "\n")
}

func renderGPU(gpus []model.GPU) string {
	if len(gpus) == 0 {
		return "[#94a3b8]No DRM GPU devices detected.[-]\n\n[#64748b]AMD/Intel metrics can be added through DRM/hwmon backends; NVIDIA can use an optional nvidia-smi backend.[-]"
	}
	lines := make([]string, 0, len(gpus)*2)
	for _, gpu := range gpus {
		// First line: show the product name if available, otherwise the vendor.
		lines = append(lines,
			fmt.Sprintf("[#e2e8f0::b]%s[-:-:-]", fallback(gpu.Name, gpu.Vendor)),
			fmt.Sprintf("[#94a3b8]temp [-] %s   [#94a3b8]power[-] %s", floatPtr(gpu.TemperatureC, "C"), floatPtr(gpu.PowerW, "W")),
		)
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
	return tview.NewTableCell(text).SetStyle(style).SetExpansion(1)
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
	color := "#22c55e"
	if value >= 85 {
		color = "#ef4444"
	} else if value >= 65 {
		color = "#f59e0b"
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
		b.WriteString("[#334155]")
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
		return "[#22c55e]enabled[-]"
	}
	return "[#f59e0b]disabled[-]"
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
