package tui

import (
	"fmt"
	"github.com/aayushkdev/perfmon/internal/model"
	"os"
	"sort"
	"strings"
	"syscall"
)

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
