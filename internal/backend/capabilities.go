package backend

import (
	"github.com/aayushkdev/perfmon/internal/backend/gpu"
	"github.com/aayushkdev/perfmon/internal/model"
	"os"
	"path/filepath"
)

func (c *Collector) capabilities() []model.Capability {
	return []model.Capability{
		capability("CPU governor", "standard cpufreq governor switching", "policy", countExistingPaths(
			filepath.Join(c.sys, "devices/system/cpu/cpu*/cpufreq/scaling_governor"),
			filepath.Join(c.sys, "devices/system/cpu/cpufreq/policy*/scaling_governor"),
		), filepath.Join(c.sys, "devices/system/cpu/cpu0/cpufreq/scaling_governor")),
		capability("CPU EPP", "energy performance preference", "policy", countExistingPaths(
			filepath.Join(c.sys, "devices/system/cpu/cpu*/cpufreq/energy_performance_preference"),
			filepath.Join(c.sys, "devices/system/cpu/cpufreq/policy*/energy_performance_preference"),
		), filepath.Join(c.sys, "devices/system/cpu/cpu0/cpufreq/energy_performance_preference")),
		capability("CPU core online", "per-core online/offline control", "core", countExistingPaths(
			filepath.Join(c.sys, "devices/system/cpu/cpu*/online"),
		), filepath.Join(c.sys, "devices/system/cpu/cpu1/online")),
		capability("CPU turbo", "boost/turbo switch exposed by driver", "system", 1, firstExistingPath(
			filepath.Join(c.sys, "devices/system/cpu/intel_pstate/no_turbo"),
			filepath.Join(c.sys, "devices/system/cpu/amd_pstate/no_turbo"),
			filepath.Join(c.sys, "devices/system/cpu/cpufreq/boost"),
		)),
		capability("Power profile", "ACPI platform profile selection", "system", len(c.readPowerProfileChoices()), filepath.Join(c.sys, "firmware/acpi/platform_profile")),
		capability("Thermal sensors", "hwmon and thermal zone telemetry", "sensor", countExistingPaths(
			filepath.Join(c.sys, "class/hwmon/hwmon*/temp*_input"),
			filepath.Join(c.sys, "class/thermal/thermal_zone*/temp"),
		), filepath.Join(c.sys, "class/hwmon")),
		capability("Package power", "RAPL powercap telemetry", "package", countExistingPaths(
			filepath.Join(c.sys, "class/powercap/intel-rapl:*"),
		), filepath.Join(c.sys, "class/powercap")),
		capability("Battery", "battery and AC power-supply telemetry", "supply", countExistingPaths(
			filepath.Join(c.sys, "class/power_supply/*"),
		), filepath.Join(c.sys, "class/power_supply")),
		capability("GPU DRM", "kernel DRM device discovery", "device", len(gpu.Read(c.sys)), filepath.Join(c.sys, "class/drm")),
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
