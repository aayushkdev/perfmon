package vendors

import (
	"strings"

	"github.com/aayushkdev/perfmon/internal/backend/cpu/types"
	"github.com/aayushkdev/perfmon/internal/model"
)

type GenericClassifier struct{}

func NewGeneric() GenericClassifier {
	return GenericClassifier{}
}

func (GenericClassifier) Name() string {
	return "generic"
}

func (GenericClassifier) Classify(core model.CPUCore, _ types.Info) model.CoreType {
	return classifyGenericTopologyType(core.TopologyType)
}

func classifyGenericTopologyType(raw string) model.CoreType {
	switch {
	case strings.Contains(raw, "performance"), raw == "core", strings.Contains(raw, "big"):
		return model.CorePerformance
	case strings.Contains(raw, "efficiency"), strings.Contains(raw, "atom"), strings.Contains(raw, "little"):
		return model.CoreEfficiency
	default:
		return model.CoreUnknown
	}
}
