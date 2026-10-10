package battery

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/aayushkdev/perfmon/internal/model"
)

func Read(sys string) ([]model.Battery, *bool) {
	matches, _ := filepath.Glob(filepath.Join(sys, "class/power_supply/*"))
	batteries := make([]model.Battery, 0, len(matches))
	var acOnline *bool
	for _, base := range matches {
		typ := strings.ToLower(readString(filepath.Join(base, "type")))
		name := filepath.Base(base)
		switch typ {
		case "battery":
			batt := model.Battery{
				Name:            name,
				Status:          readString(filepath.Join(base, "status")),
				CapacityPercent: readFloatField(filepath.Join(base, "capacity")),
				HealthPercent:   batteryHealthPercent(base),
				CycleCount:      readFloatField(filepath.Join(base, "cycle_count")),
				EnergyNowWh:     readEnergyWh(base, "energy_now", "charge_now"),
				EnergyFullWh:    readEnergyWh(base, "energy_full", "charge_full"),
				PowerW:          readPowerNowW(base),
				VoltageV:        readVoltageV(base),
				Present:         readBoolField(filepath.Join(base, "present")),
			}
			batteries = append(batteries, batt)
		case "mains", "usb", "ac":
			if online := readBoolField(filepath.Join(base, "online")); online != nil {
				acOnline = online
			}
		}
	}
	return batteries, acOnline
}

func batteryHealthPercent(base string) *float64 {
	full := readEnergyWh(base, "energy_full", "charge_full")
	design := readEnergyWh(base, "energy_full_design", "charge_full_design")
	if full == nil || design == nil || *design == 0 {
		return nil
	}
	value := (*full / *design) * 100
	return &value
}

func readEnergyWh(base string, names ...string) *float64 {
	for _, name := range names {
		path := filepath.Join(base, name)
		raw := readString(path)
		if raw == "" {
			continue
		}
		// parse as float to handle large values reliably
		v, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
		if err != nil {
			continue
		}
		// Common kernel semantics:
		// - energy_* fields are in micro-watt-hours (uWh)
		// - charge_* fields are in micro-ampere-hours (uAh)
		// If we find an energy field, convert uWh -> Wh by dividing by 1e6.
		// If we find a charge field, convert uAh -> Wh using the battery voltage
		// (voltage is provided in microvolts in sysfs so readVoltageV already
		// returns volts).
		lower := strings.ToLower(name)
		if strings.Contains(lower, "energy") {
			wh := v / 1e6
			return &wh
		}
		if strings.Contains(lower, "charge") {
			// need voltage to convert charge (uAh) to Wh: Wh = (uAh / 1e6) * V
			if volts := readVoltageV(base); volts != nil && *volts > 0 {
				wh := (v / 1e6) * (*volts)
				return &wh
			}
			// If voltage is unavailable, fall back to returning nil so callers
			// know the value is unreliable.
			return nil
		}
		// Unknown field name: treat as micro-watt-hours by default
		wh := v / 1e6
		return &wh
	}
	return nil
}

func readPowerNowW(base string) *float64 {
	if value := readMicroPower(filepath.Join(base, "power_now")); value != nil {
		return value
	}
	if value := readMicroPower(filepath.Join(base, "current_now")); value != nil {
		voltage := readVoltageV(base)
		if voltage != nil {
			watts := *value * *voltage
			return &watts
		}
		return value
	}
	return nil
}

func readMicroPower(path string) *float64 {
	raw := readString(path)
	if raw == "" {
		return nil
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil || value <= 0 {
		return nil
	}
	watts := value / 1e6
	return &watts
}

func readVoltageV(base string) *float64 {
	for _, name := range []string{"voltage_now", "voltage_min_design", "voltage_max_design"} {
		raw := readString(filepath.Join(base, name))
		if raw == "" {
			continue
		}
		value, err := strconv.ParseFloat(raw, 64)
		if err != nil || value <= 0 {
			continue
		}
		volts := value / 1e6
		return &volts
	}
	return nil
}

func readFloatField(path string) *float64 {
	raw := readString(path)
	if raw == "" {
		return nil
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return nil
	}
	return &value
}

func readBoolField(path string) *bool {
	raw := readString(path)
	if raw == "" {
		return nil
	}
	switch strings.ToLower(raw) {
	case "1", "y", "yes", "true", "on":
		v := true
		return &v
	case "0", "n", "no", "false", "off":
		v := false
		return &v
	default:
		return nil
	}
}

func readString(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}
