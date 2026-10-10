package render

import (
	"fmt"
	"github.com/aayushkdev/perfmon/internal/model"
	"strings"
)

func Battery(s model.Snapshot) string {
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

func acState(value *bool) string {
	if value == nil {
		return "unavailable"
	}
	if *value {
		return "online"
	}
	return "offline"
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
