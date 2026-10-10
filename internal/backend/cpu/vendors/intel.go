package vendors

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/aayushkdev/perfmon/internal/backend/cpu/types"
	"github.com/aayushkdev/perfmon/internal/model"
)

type IntelClassifier struct {
	performance map[int]bool
	efficiency  map[int]bool
}

func NewIntel(sys string) IntelClassifier {
	return IntelClassifier{
		performance: readIntelCPUList(filepath.Join(sys, "devices/cpu_core/cpus")),
		efficiency:  readIntelCPUList(filepath.Join(sys, "devices/cpu_atom/cpus")),
	}
}

func (IntelClassifier) Name() string {
	return "intel"
}

func (c IntelClassifier) Classify(core model.CPUCore, info types.Info) model.CoreType {
	if !strings.Contains(strings.ToLower(info.Vendor), "intel") {
		return model.CoreUnknown
	}
	if c.performance[core.ID] {
		return model.CorePerformance
	}
	if c.efficiency[core.ID] {
		return model.CoreEfficiency
	}
	switch classifyIntelTopologyType(core.TopologyType) {
	case model.CorePerformance, model.CoreEfficiency:
		return classifyIntelTopologyType(core.TopologyType)
	}
	return model.CoreUnknown
}

func readIntelCPUList(path string) map[int]bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	return parseIntelCPUList(strings.TrimSpace(string(data)))
}

func parseIntelCPUList(value string) map[int]bool {
	cpus := map[int]bool{}
	for _, part := range strings.Split(value, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if start, end, ok := strings.Cut(part, "-"); ok {
			addIntelRange(cpus, start, end)
			continue
		}
		id, err := strconv.Atoi(part)
		if err == nil {
			cpus[id] = true
		}
	}
	return cpus
}

func addIntelRange(cpus map[int]bool, startRaw, endRaw string) {
	start, startErr := strconv.Atoi(strings.TrimSpace(startRaw))
	end, endErr := strconv.Atoi(strings.TrimSpace(endRaw))
	if startErr != nil || endErr != nil || end < start {
		return
	}
	for id := start; id <= end; id++ {
		cpus[id] = true
	}
}

func classifyIntelTopologyType(raw string) model.CoreType {
	switch {
	case strings.Contains(raw, "performance"), raw == "core", strings.Contains(raw, "big"):
		return model.CorePerformance
	case strings.Contains(raw, "efficiency"), strings.Contains(raw, "atom"), strings.Contains(raw, "little"):
		return model.CoreEfficiency
	default:
		return model.CoreUnknown
	}
}
