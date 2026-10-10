package backend

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func (c *Collector) readAvailableGovernors() []string {
	return dedupeSortedFieldsFromGlobs(
		filepath.Join(c.sys, "devices/system/cpu/cpu*/cpufreq/scaling_available_governors"),
		filepath.Join(c.sys, "devices/system/cpu/cpufreq/policy*/scaling_available_governors"),
	)
}

func (c *Collector) readEPPChoices() []string {
	return orderEPPChoices(dedupeSortedFieldsFromGlobs(
		filepath.Join(c.sys, "devices/system/cpu/cpu*/cpufreq/energy_performance_available_preferences"),
		filepath.Join(c.sys, "devices/system/cpu/cpufreq/policy*/energy_performance_available_preferences"),
	))
}

func orderEPPChoices(choices []string) []string {
	preferred := []string{"performance", "balance_performance", "balance_power", "power", "default"}
	available := make(map[string]bool, len(choices))
	for _, choice := range choices {
		available[choice] = true
	}
	ordered := make([]string, 0, len(choices))
	for _, choice := range preferred {
		if available[choice] {
			ordered = append(ordered, choice)
			delete(available, choice)
		}
	}
	remaining := make([]string, 0, len(available))
	for choice := range available {
		remaining = append(remaining, choice)
	}
	sort.Strings(remaining)
	return append(ordered, remaining...)
}

func (c *Collector) readPowerProfile() string {
	path := filepath.Join(c.sys, "firmware/acpi/platform_profile")
	return normalizePowerProfile(readString(path))
}

func (c *Collector) readCPUDriver() string {
	for _, path := range []string{
		filepath.Join(c.sys, "devices/system/cpu/intel_pstate/status"),
		filepath.Join(c.sys, "devices/system/cpu/amd_pstate/status"),
	} {
		if readString(path) != "" {
			switch {
			case strings.Contains(path, "intel_pstate"):
				return "intel_pstate"
			case strings.Contains(path, "amd_pstate"):
				return "amd_pstate"
			}
		}
	}
	for _, path := range []string{
		filepath.Join(c.sys, "devices/system/cpu/cpu0/cpufreq/scaling_driver"),
		filepath.Join(c.sys, "devices/system/cpu/cpufreq/policy0/scaling_driver"),
	} {
		if raw := readString(path); raw != "" {
			return raw
		}
	}
	if _, err := os.Stat(filepath.Join(c.sys, "devices/system/cpu/cpufreq")); err == nil {
		return "cpufreq"
	}
	return ""
}

func (c *Collector) readPowerProfileChoices() []string {
	path := filepath.Join(c.sys, "firmware/acpi/platform_profile_choices")
	return strings.Fields(readString(path))
}

func (c *Collector) readTurboEnabled() *bool {
	candidates := []string{
		filepath.Join(c.sys, "devices/system/cpu/intel_pstate/no_turbo"),
		filepath.Join(c.sys, "devices/system/cpu/amd_pstate/no_turbo"),
		filepath.Join(c.sys, "devices/system/cpu/cpufreq/boost"),
	}
	for _, path := range candidates {
		raw := readString(path)
		if raw == "" {
			continue
		}
		enabled := raw != "1"
		if strings.HasSuffix(path, "boost") {
			enabled = raw == "1"
		}
		return &enabled
	}
	return nil
}
