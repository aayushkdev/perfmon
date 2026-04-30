package generic

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
	return "generic"
}

func (Classifier) Classify(core model.CPUCore, _ cpu.Info) model.CoreType {
	return classifyTopologyType(core.TopologyType)
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
