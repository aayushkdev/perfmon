package power

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/aayushkdev/perfmon/internal/model"
)

type Sample struct {
	Energy uint64
	At     time.Time
}

func Read(sys string, previous map[string]Sample) []model.PowerDomain {
	domains := make([]model.PowerDomain, 0, 8)
	matches, _ := filepath.Glob(filepath.Join(sys, "class/powercap/intel-rapl:*"))
	for _, base := range matches {
		energy := readMicroEnergy(filepath.Join(base, "energy_uj"))
		if energy == nil {
			continue
		}
		name := readString(filepath.Join(base, "name"))
		if name == "" {
			name = filepath.Base(base)
		}
		domains = append(domains, model.PowerDomain{
			Name:      name,
			Source:    "rapl",
			PowerW:    raplPower(base, *energy, previous),
			EnergyJ:   ujToJ(energy),
			LimitW:    readMicroPower(filepath.Join(base, "constraint_0_power_limit_uw")),
			MaxW:      readMicroPower(filepath.Join(base, "constraint_0_max_power_uw")),
			CriticalW: readMicroPower(filepath.Join(base, "constraint_0_crit_power_uw")),
		})
	}
	return domains
}

func raplPower(base string, energy uint64, previous map[string]Sample) *float64 {
	key := filepath.Base(base)
	now := time.Now()
	prev, ok := previous[key]
	previous[key] = Sample{Energy: energy, At: now}
	if !ok || prev.At.IsZero() || !now.After(prev.At) {
		return nil
	}
	delta := energy
	if energy >= prev.Energy {
		delta = energy - prev.Energy
	}
	seconds := now.Sub(prev.At).Seconds()
	if seconds <= 0 {
		return nil
	}
	watts := float64(delta) / 1e6 / seconds
	return &watts
}

func readMicroEnergy(path string) *uint64 {
	raw := readString(path)
	if raw == "" {
		return nil
	}
	value, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return nil
	}
	return &value
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

func ujToJ(value *uint64) *float64 {
	if value == nil {
		return nil
	}
	joules := float64(*value) / 1e6
	return &joules
}

func readString(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}
