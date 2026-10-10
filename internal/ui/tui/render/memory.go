package render

import (
	"fmt"
	"github.com/aayushkdev/perfmon/internal/model"
	"strings"
)

func Memory(mem model.Memory) string {
	usedPct := percent(mem.UsedBytes, mem.TotalBytes)
	swapPct := percent(mem.SwapUsedBytes, mem.SwapTotalBytes)
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
	return strings.Join(lines, "\n")
}
