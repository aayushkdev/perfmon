package process

import (
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"

	"github.com/aayushkdev/perfmon/internal/model"
)

func Scan(proc string, prevProcJiffies map[int]uint64, prevTotalJiffies uint64) ([]model.Process, map[int]uint64, uint64) {
	data, err := os.ReadFile(filepath.Join(proc, "stat"))
	if err != nil {
		return nil, prevProcJiffies, prevTotalJiffies
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
	memTotal := uint64(0)
	memData, _ := os.ReadFile(filepath.Join(proc, "meminfo"))
	for _, line := range strings.Split(string(memData), "\n") {
		if strings.HasPrefix(line, "MemTotal:") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				if v, err := strconv.ParseUint(parts[1], 10, 64); err == nil {
					memTotal = v * 1024
				}
			}
			break
		}
	}

	pids, _ := filepath.Glob(filepath.Join(proc, "[0-9]*"))
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
		pj := utime + stime
		procJiffies[pid] = pj
		var cpuPct float64
		if prevTotal := prevTotalJiffies; prevTotal > 0 && totalNow > prevTotal {
			deltaTotal := float64(totalNow - prevTotal)
			prevP := prevProcJiffies[pid]
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
	sort.Slice(procs, func(i, j int) bool { return procs[i].RSSBytes > procs[j].RSSBytes })
	return procs, procJiffies, totalNow
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
