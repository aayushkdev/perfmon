package intel

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/aayushkdev/perfmon/internal/backend/cpu"
	"github.com/aayushkdev/perfmon/internal/model"
)

type Classifier struct {
	performance map[int]bool
	efficiency  map[int]bool
}

func New(sys string) Classifier {
	return Classifier{
		performance: readCPUList(filepath.Join(sys, "devices/cpu_core/cpus")),
		efficiency:  readCPUList(filepath.Join(sys, "devices/cpu_atom/cpus")),
	}
}

func (Classifier) Name() string {
	return "intel"
}

func (c Classifier) Classify(core model.CPUCore, info cpu.Info) model.CoreType {
	if !strings.Contains(strings.ToLower(info.Vendor), "intel") {
		return model.CoreUnknown
	}
	if c.performance[core.ID] {
		return model.CorePerformance
	}
	if c.efficiency[core.ID] {
		return model.CoreEfficiency
	}
	switch classifyTopologyType(core.TopologyType) {
	case model.CorePerformance, model.CoreEfficiency:
		return classifyTopologyType(core.TopologyType)
	}
	return model.CoreUnknown
}

func readCPUList(path string) map[int]bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	return parseCPUList(strings.TrimSpace(string(data)))
}

func parseCPUList(value string) map[int]bool {
	cpus := map[int]bool{}
	for _, part := range strings.Split(value, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if start, end, ok := strings.Cut(part, "-"); ok {
			addRange(cpus, start, end)
			continue
		}
		id, err := strconv.Atoi(part)
		if err == nil {
			cpus[id] = true
		}
	}
	return cpus
}

func addRange(cpus map[int]bool, startRaw, endRaw string) {
	start, startErr := strconv.Atoi(strings.TrimSpace(startRaw))
	end, endErr := strconv.Atoi(strings.TrimSpace(endRaw))
	if startErr != nil || endErr != nil || end < start {
		return
	}
	for id := start; id <= end; id++ {
		cpus[id] = true
	}
}

func classifyTopologyType(raw string) model.CoreType {
	switch {
	case strings.Contains(raw, "performance"), raw == "core", strings.Contains(raw, "big"):
		return model.CorePerformance
	case strings.Contains(raw, "efficiency"), strings.Contains(raw, "atom"), strings.Contains(raw, "little"):
		return model.CoreEfficiency
	default:
		return model.CoreUnknown
	}
}
