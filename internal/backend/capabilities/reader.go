package capabilities

import (
	"github.com/aayushkdev/perfmon/internal/backend/gpu"
	"github.com/aayushkdev/perfmon/internal/model"
	"os"
	"path/filepath"
	"strings"
)

func Read(sys string) []model.Capability {
	return []model.Capability{
		capability("CPU governor", "standard cpufreq governor switching", "policy", countExistingPaths(
			filepath.Join(sys, "devices/system/cpu/cpu*/cpufreq/scaling_governor"),
			filepath.Join(sys, "devices/system/cpu/cpufreq/policy*/scaling_governor"),
		), filepath.Join(sys, "devices/system/cpu/cpu0/cpufreq/scaling_governor")),
		capability("CPU EPP", "energy performance preference", "policy", countExistingPaths(
			filepath.Join(sys, "devices/system/cpu/cpu*/cpufreq/energy_performance_preference"),
			filepath.Join(sys, "devices/system/cpu/cpufreq/policy*/energy_performance_preference"),
		), filepath.Join(sys, "devices/system/cpu/cpu0/cpufreq/energy_performance_preference")),
		capability("CPU core online", "per-core online/offline control", "core", countExistingPaths(
			filepath.Join(sys, "devices/system/cpu/cpu*/online"),
		), filepath.Join(sys, "devices/system/cpu/cpu1/online")),
		capability("CPU turbo", "boost/turbo switch exposed by driver", "system", 1, firstExistingPath(
			filepath.Join(sys, "devices/system/cpu/intel_pstate/no_turbo"),
			filepath.Join(sys, "devices/system/cpu/amd_pstate/no_turbo"),
			filepath.Join(sys, "devices/system/cpu/cpufreq/boost"),
		)),
		capability("Power profile", "ACPI platform profile selection", "system", len(readPowerProfileChoices(sys)), filepath.Join(sys, "firmware/acpi/platform_profile")),
		capability("Thermal sensors", "hwmon and thermal zone telemetry", "sensor", countExistingPaths(
			filepath.Join(sys, "class/hwmon/hwmon*/temp*_input"),
			filepath.Join(sys, "class/thermal/thermal_zone*/temp"),
		), filepath.Join(sys, "class/hwmon")),
		capability("Package power", "RAPL powercap telemetry", "package", countExistingPaths(
			filepath.Join(sys, "class/powercap/intel-rapl:*"),
		), filepath.Join(sys, "class/powercap")),
		capability("Battery", "battery and AC power-supply telemetry", "supply", countExistingPaths(
			filepath.Join(sys, "class/power_supply/*"),
		), filepath.Join(sys, "class/power_supply")),
		capability("GPU DRM", "kernel DRM device discovery", "device", len(gpu.Read(sys)), filepath.Join(sys, "class/drm")),
	}
}

func capability(name, description, scope string, targets int, path string, extraPaths ...string) model.Capability {
	status := model.CapabilityUnavailable
	for _, candidate := range append([]string{path}, extraPaths...) {
		if candidate == "" {
			continue
		}
		if _, err := os.Stat(candidate); err == nil {
			status = model.CapabilityAvailable
			path = candidate
			break
		}
	}
	paths := append([]string{path}, extraPaths...)
	return model.Capability{Name: name, Status: status, Description: description, Path: path, Scope: scope, Targets: targets, Paths: paths}
}

func countExistingPaths(patterns ...string) int {
	total := 0
	for _, pattern := range patterns {
		matches, _ := filepath.Glob(pattern)
		total += len(matches)
	}
	return total
}

func readPowerProfileChoices(sys string) []string {
	raw, err := os.ReadFile(filepath.Join(sys, "firmware/acpi/platform_profile_choices"))
	if err != nil {
		return nil
	}
	return strings.Fields(string(raw))
}

func firstExistingPath(paths ...string) string {
	for _, path := range paths {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ""
}
