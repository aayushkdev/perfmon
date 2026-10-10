package tui

import (
	"fmt"

	"github.com/aayushkdev/perfmon/internal/model"
)

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
