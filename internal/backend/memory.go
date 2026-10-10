package backend

import (
	"bytes"
	"github.com/aayushkdev/perfmon/internal/model"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
)

func (c *Collector) readMemory() model.Memory {
	values := readMeminfo(filepath.Join(c.proc, "meminfo"))
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
	// PSI removed: do not populate pressure fields
	// Best-effort: try to detect DIMM/module count and speed from EDAC sysfs
	// entries (varies by kernel and platform). This is non-fatal — if nothing
	// is found the fields remain nil.
	if modules, speed := c.readMemoryModules(); modules > 0 || speed > 0 {
		if modules > 0 {
			mem.ModuleCount = &modules
		}
		if speed > 0 {
			mem.SpeedMHz = &speed
		}
	} else {
		// If sysfs didn't yield results, try dmidecode as a root-only fallback.
		if mod, spd := c.dmidecodeFallback(); mod > 0 || spd > 0 {
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
func (c *Collector) updateProcesses() {
	// read total jiffies from /proc/stat
	data, err := os.ReadFile(filepath.Join(c.proc, "stat"))
	if err != nil {
		return
	}
	var totalNow uint64
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "cpu ") {
			fields := strings.Fields(line)
			for _, f := range fields[1:] {
				if v, err := strconv.ParseUint(f, 10, 64); err == nil {
					totalNow += v
				}
			}
			break
		}
	}
	// read mem total
	memTotal := uint64(0)
	memData, _ := os.ReadFile(filepath.Join(c.proc, "meminfo"))
	for _, line := range strings.Split(string(memData), "\n") {
		if strings.HasPrefix(line, "MemTotal:") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				if v, err := strconv.ParseUint(parts[1], 10, 64); err == nil {
					// value is in kB
					memTotal = v * 1024
				}
			}
			break
		}
	}

	pids, _ := filepath.Glob(filepath.Join(c.proc, "[0-9]*"))
	procs := make([]model.Process, 0, len(pids))
	procJiffies := make(map[int]uint64)
	for _, p := range pids {
		pidStr := filepath.Base(p)
		pid, err := strconv.Atoi(pidStr)
		if err != nil {
			continue
		}
		statPath := filepath.Join(p, "stat")
		bs, err := os.ReadFile(statPath)
		if err != nil {
			continue
		}
		// parse /proc/<pid>/stat robustly: comm can contain spaces and is
		// enclosed in parentheses. We'll extract comm and then split the
		// remaining fields.
		ppid, startTime, utime, stime, rssPages, name, ok := parseProcStat(bs)
		if !ok {
			continue
		}
		// compute jiffies
		pj := utime + stime
		procJiffies[pid] = pj
		// compute deltas
		var cpuPct float64
		if prevTotal := c.prevTotalJiffies; prevTotal > 0 && totalNow > prevTotal {
			deltaTotal := float64(totalNow - prevTotal)
			prevP := c.prevProcJiffies[pid]
			deltaP := float64(0)
			if pj > prevP {
				deltaP = float64(pj - prevP)
			}
			if deltaTotal > 0 {
				cpuPct = 100.0 * deltaP / deltaTotal
			}
		}
		// rss: pages -> bytes. Use the system page size instead of assuming 4KiB.
		rssBytes := uint64(0)
		if rssPages > 0 {
			rssBytes = rssPages * uint64(syscall.Getpagesize())
		}
		memPct := 0.0
		if memTotal > 0 {
			memPct = float64(rssBytes) * 100.0 / float64(memTotal)
		}
		procs = append(procs, model.Process{
			PID:        pid,
			PPID:       ppid,
			StartTime:  startTime,
			Name:       name,
			Cmdline:    readProcCmdline(filepath.Join(p, "cmdline")),
			CPUPercent: cpuPct,
			MemPercent: memPct,
			RSSBytes:   rssBytes,
		})
	}
	// sort by memory (RSS) desc by default
	sort.Slice(procs, func(i, j int) bool { return procs[i].RSSBytes > procs[j].RSSBytes })
	c.processes = procs
	c.prevProcJiffies = procJiffies
	c.prevTotalJiffies = totalNow
}

func readProcCmdline(path string) string {
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) == 0 {
		return ""
	}
	return strings.TrimSpace(strings.ReplaceAll(string(raw), "\x00", " "))
}

// parseProcStat parses the raw contents of /proc/<pid>/stat. The comm field
// (process name) is enclosed in parentheses and may contain spaces, so we
// extract it carefully. Returns (ppid, startTime, utime, stime, rssPages, name, ok).
func parseProcStat(raw []byte) (int, uint64, uint64, uint64, uint64, string, bool) {
	// find the first '(' and last ')' which delimit comm
	l := bytes.IndexByte(raw, '(')
	r := bytes.LastIndexByte(raw, ')')
	if l < 0 || r < 0 || r <= l {
		return 0, 0, 0, 0, 0, "", false
	}
	name := string(raw[l+1 : r])
	// fields before '(' are pid, after ')' are the rest
	after := raw[r+1:]
	fields := strings.Fields(string(after))
	// according to procfs, utime is field 13, stime 14, rss is 24 relative to
	// the start of the whole line. After splitting like this, fields[11] is
	// utime (since fields starts at index 0 corresponding to field 3).
	// We need to ensure there are enough fields.
	if len(fields) < 22 { // need at least up to rss
		return 0, 0, 0, 0, 0, name, false
	}
	ppid, err0 := strconv.Atoi(fields[1])
	startTime, errStart := strconv.ParseUint(fields[19], 10, 64)
	// utime: fields[11], stime: fields[12], rss: fields[21]
	utime, err1 := strconv.ParseUint(fields[11], 10, 64)
	stime, err2 := strconv.ParseUint(fields[12], 10, 64)
	rssPages, err3 := strconv.ParseUint(fields[21], 10, 64)
	if err0 != nil || errStart != nil || err1 != nil || err2 != nil || err3 != nil {
		return 0, 0, 0, 0, 0, name, false
	}
	return ppid, startTime, utime, stime, rssPages, name, true
}

// readMemoryModules attempts to read memory module count and common speed from
// EDAC-related sysfs entries. It returns (moduleCount, speedMHz). Both values
// are best-effort and may be zero when unavailable.
func (c *Collector) readMemoryModules() (int, int) {
	// Look for dimm entries under EDAC: devices/system/edac/mc*/csrow*/dimm*
	pattern := filepath.Join(c.sys, "devices", "system", "edac", "mc*", "csrow*", "dimm*")
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
func (c *Collector) dmidecodeFallback() (int, int) {
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

// PSI support removed. The kernel pressure files are not parsed anymore.
