package amd

import (
	"strings"

	"github.com/aayushkdev/perfmon/internal/backend/cpu"
	"github.com/aayushkdev/perfmon/internal/model"
)

type Classifier struct{}

func New() Classifier {
	return Classifier{}
}

func (Classifier) Name() string {
	return "amd"
}

func (Classifier) Classify(core model.CPUCore, _ cpu.Info) model.CoreType {
	switch classifyTopologyType(core.TopologyType) {
	case model.CorePerformance, model.CoreEfficiency:
		return classifyTopologyType(core.TopologyType)
	}
	return model.CoreUnknown
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

func Supports(info cpu.Info) bool {
	vendor := strings.ToLower(info.Vendor)
	return strings.Contains(vendor, "amd") || strings.Contains(vendor, "authenticamd")
}
