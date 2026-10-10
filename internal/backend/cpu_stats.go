package backend

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func (c *Collector) readCPUStats() (map[int]cpuStat, cpuStat, error) {
	data, err := os.ReadFile(filepath.Join(c.proc, "stat"))
	if err != nil {
		return nil, cpuStat{}, err
	}
	stats := make(map[int]cpuStat)
	var aggregate cpuStat

	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 8 || !strings.HasPrefix(fields[0], "cpu") {
			continue
		}
		stat, err := parseCPUStat(fields[1:])
		if err != nil {
			continue
		}
		if fields[0] == "cpu" {
			aggregate = stat
			continue
		}
		id, err := strconv.Atoi(strings.TrimPrefix(fields[0], "cpu"))
		if err == nil {
			stats[id] = stat
		}
	}
	if len(stats) == 0 {
		return nil, cpuStat{}, errors.New("no CPU stats found")
	}
	return stats, aggregate, nil
}

func parseCPUStat(fields []string) (cpuStat, error) {
	var values [10]uint64
	for i := range values {
		if i >= len(fields) {
			break
		}
		v, err := strconv.ParseUint(fields[i], 10, 64)
		if err != nil {
			return cpuStat{}, err
		}
		values[i] = v
	}
	idle := values[3] + values[4]
	total := uint64(0)
	for _, v := range values {
		total += v
	}
	return cpuStat{idle: idle, total: total}, nil
}

func usage(prev, cur cpuStat) float64 {
	if prev.total == 0 || cur.total <= prev.total {
		return 0
	}
	totalDelta := cur.total - prev.total
	idleDelta := cur.idle - prev.idle
	if totalDelta == 0 || idleDelta > totalDelta {
		return 0
	}
	return float64(totalDelta-idleDelta) * 100 / float64(totalDelta)
}
