package render

import (
	"fmt"
	"github.com/aayushkdev/perfmon/internal/model"
	"strings"
)

func CPU(cpu model.CPU) string {
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
