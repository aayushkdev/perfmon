package cpu

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/aayushkdev/perfmon/internal/backend/fs"
)

type MetadataReader struct{ Sys string }

func (r MetadataReader) AvailableGovernors() []string { return r.fields("scaling_available_governors") }
func (r MetadataReader) EPPChoices() []string {
	return r.fields("energy_performance_available_preferences")
}
func (r MetadataReader) fields(name string) []string {
	seen := map[string]struct{}{}
	for _, pattern := range []string{filepath.Join(r.Sys, "devices/system/cpu/cpu*/cpufreq", name), filepath.Join(r.Sys, "devices/system/cpu/cpufreq/policy*", name)} {
		matches, _ := filepath.Glob(pattern)
		for _, path := range matches {
			for _, field := range strings.Fields(fs.ReadString(path)) {
				seen[field] = struct{}{}
			}
		}
	}
	out := make([]string, 0, len(seen))
	for field := range seen {
		out = append(out, field)
	}
	sort.Strings(out)
	if name == "energy_performance_available_preferences" {
		return orderEPPChoices(out)
	}
	return out
}
func (r MetadataReader) PowerProfile() string {
	return normalize(fs.ReadString(filepath.Join(r.Sys, "firmware/acpi/platform_profile")))
}
func (r MetadataReader) Driver() string {
	for _, path := range []string{filepath.Join(r.Sys, "devices/system/cpu/intel_pstate/status"), filepath.Join(r.Sys, "devices/system/cpu/amd_pstate/status")} {
		if fs.ReadString(path) != "" {
			if strings.Contains(path, "intel_pstate") {
				return "intel_pstate"
			}
			return "amd_pstate"
		}
	}
	for _, path := range []string{filepath.Join(r.Sys, "devices/system/cpu/cpu0/cpufreq/scaling_driver"), filepath.Join(r.Sys, "devices/system/cpu/cpufreq/policy0/scaling_driver")} {
		if raw := fs.ReadString(path); raw != "" {
			return raw
		}
	}
	if _, err := os.Stat(filepath.Join(r.Sys, "devices/system/cpu/cpufreq")); err == nil {
		return "cpufreq"
	}
	return ""
}
func (r MetadataReader) PowerProfileChoices() []string {
	return strings.Fields(fs.ReadString(filepath.Join(r.Sys, "firmware/acpi/platform_profile_choices")))
}
func (r MetadataReader) TurboEnabled() *bool {
	for _, path := range []string{filepath.Join(r.Sys, "devices/system/cpu/intel_pstate/no_turbo"), filepath.Join(r.Sys, "devices/system/cpu/amd_pstate/no_turbo"), filepath.Join(r.Sys, "devices/system/cpu/cpufreq/boost")} {
		raw := fs.ReadString(path)
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
func orderEPPChoices(choices []string) []string {
	preferred := []string{"performance", "balance_performance", "balance_power", "power", "default"}
	available := map[string]bool{}
	for _, choice := range choices {
		available[choice] = true
	}
	ordered := []string{}
	for _, choice := range preferred {
		if available[choice] {
			ordered = append(ordered, choice)
			delete(available, choice)
		}
	}
	remaining := []string{}
	for choice := range available {
		remaining = append(remaining, choice)
	}
	sort.Strings(remaining)
	return append(ordered, remaining...)
}
func normalize(profile string) string {
	switch strings.ToLower(strings.TrimSpace(profile)) {
	case "low-power", "powersave":
		return "powersave"
	case "balanced", "balance":
		return "balanced"
	case "performance", "perf":
		return "performance"
	default:
		return strings.ToLower(strings.TrimSpace(profile))
	}
}
