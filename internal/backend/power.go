package backend

import (
	"github.com/aayushkdev/perfmon/internal/model"
	"path/filepath"
	"strconv"
	"time"
)

func (c *Collector) readPowerDomains() []model.PowerDomain {
	domains := make([]model.PowerDomain, 0, 8)
	matches, _ := filepath.Glob(filepath.Join(c.sys, "class/powercap/intel-rapl:*"))
	for _, base := range matches {
		energy := readMicroEnergy(filepath.Join(base, "energy_uj"))
		if energy == nil {
			continue
		}
		name := readString(filepath.Join(base, "name"))
		if name == "" {
			name = filepath.Base(base)
		}
		domain := model.PowerDomain{
			Name:      name,
			Source:    "rapl",
			PowerW:    c.readRAPLPower(base, energy),
			EnergyJ:   ujToJ(energy),
			LimitW:    readMicroPower(filepath.Join(base, "constraint_0_power_limit_uw")),
			MaxW:      readMicroPower(filepath.Join(base, "constraint_0_max_power_uw")),
			CriticalW: readMicroPower(filepath.Join(base, "constraint_0_crit_power_uw")),
		}
		domains = append(domains, domain)
	}
	return domains
}

// Note: hwmon-based CPU power reading was removed in favor of RAPL-only aggregation.

func (c *Collector) readRAPLPower(base string, energy *uint64) *float64 {
	key := filepath.Base(base)
	now := time.Now()
	prev, ok := c.raplPrev[key]
	c.raplPrev[key] = raplSample{energy: *energy, at: now}
	if !ok || prev.at.IsZero() || !now.After(prev.at) {
		return nil
	}
	delta := uint64Delta(*energy, prev.energy)
	seconds := now.Sub(prev.at).Seconds()
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
	j := float64(*value) / 1e6
	return &j
}

func uint64Delta(cur, prev uint64) uint64 {
	if cur >= prev {
		return cur - prev
	}
	return cur
}
