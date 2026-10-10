package tui

import (
	"fmt"
	"github.com/aayushkdev/perfmon/internal/model"
	"github.com/gdamore/tcell/v2"
	"strings"
)

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
