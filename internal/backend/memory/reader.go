package memory

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/aayushkdev/perfmon/internal/model"
)

func Read(proc, sys string) model.Memory {
	values := readMeminfo(filepath.Join(proc, "meminfo"))
	total := values["MemTotal"] * 1024
	available := values["MemAvailable"] * 1024
	free := values["MemFree"] * 1024
	if available == 0 {
		available = free
	}
	swapTotal := values["SwapTotal"] * 1024
	swapFree := values["SwapFree"] * 1024
	mem := model.Memory{
		TotalBytes:     total,
		AvailableBytes: available,
		SwapTotalBytes: swapTotal,
	}
	if total > available {
		mem.UsedBytes = total - available
	}
	if swapTotal > swapFree {
		mem.SwapUsedBytes = swapTotal - swapFree
	}
	if modules, speed := readMemoryModules(sys); modules > 0 || speed > 0 {
		if modules > 0 {
			mem.ModuleCount = &modules
		}
		if speed > 0 {
			mem.SpeedMHz = &speed
		}
	} else {
		// If sysfs didn't yield results, try dmidecode as a root-only fallback.
		if mod, spd := dmidecodeFallback(); mod > 0 || spd > 0 {
			if mod > 0 {
				mem.ModuleCount = &mod
			}
			if spd > 0 {
				mem.SpeedMHz = &spd
			}
		}
	}
	return mem
}

// updateProcesses scans /proc and computes simple CPU% and memory metrics.
// This is intentionally conservative: it reads minimal per-PID data and
// tolerates permission errors. It keeps previous jiffies to compute deltas.

func readMemoryModules(sys string) (int, int) {
	// Look for dimm entries under EDAC: devices/system/edac/mc*/csrow*/dimm*
	pattern := filepath.Join(sys, "devices", "system", "edac", "mc*", "csrow*", "dimm*")
	matches, _ := filepath.Glob(pattern)
	if len(matches) == 0 {
		return 0, 0
	}
	count := 0
	var speedFound int
	for _, dimm := range matches {
		// verify it's a directory
		if fi, err := os.Stat(dimm); err != nil || !fi.IsDir() {
			continue
		}
		count++
		// try common file names for speed
		for _, name := range []string{"speed", "dimm_speed", "max_speed", "bus_speed"} {
			path := filepath.Join(dimm, name)
			if raw := readString(path); raw != "" {
				// try parse MHz or kHz values
				if v, err := strconv.Atoi(strings.Fields(raw)[0]); err == nil && v > 0 {
					// if value looks like kHz (very large) convert
					if v > 10000 {
						v = v / 1000
					}
					speedFound = v
					break
				}
			}
		}
	}
	return count, speedFound
}

// dmidecodeFallback attempts to run dmidecode to extract memory device
// information. It requires root privileges. Returns (moduleCount, speedMHz)
// or (0,0) on failure.

func dmidecodeFallback() (int, int) {
	out, err := exec.Command("dmidecode", "-t", "17").Output()
	if err != nil {
		return 0, 0
	}
	data := string(out)
	// parse blocks separated by blank lines; look for "Speed:" and count
	lines := strings.Split(data, "\n")
	moduleCount := 0
	speedMHz := 0
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "Speed:") {
			moduleCount++
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				// parts like: Speed: 2400 MHz or Speed: Unknown
				if v, err := strconv.Atoi(parts[1]); err == nil && v > 0 {
					speedMHz = v
				}
			}
		}
	}
	return moduleCount, speedMHz
}

func readMeminfo(path string) map[string]uint64 {
	data, err := os.ReadFile(path)
	if err != nil {
		return map[string]uint64{}
	}
	values := map[string]uint64{}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		key := strings.TrimSuffix(fields[0], ":")
		value, err := strconv.ParseUint(fields[1], 10, 64)
		if err == nil {
			values[key] = value
		}
	}
	return values
}

func readString(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}
