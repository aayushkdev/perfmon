package tui

import (
	"fmt"
	"github.com/aayushkdev/perfmon/internal/model"
	"strings"
)

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
