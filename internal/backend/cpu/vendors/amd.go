package vendors

import (
	"strings"

	"github.com/aayushkdev/perfmon/internal/backend/cpu/types"
	"github.com/aayushkdev/perfmon/internal/model"
)

type AMDClassifier struct{}

func NewAMD() AMDClassifier {
	return AMDClassifier{}
}

func (AMDClassifier) Name() string {
	return "amd"
}

func (AMDClassifier) Classify(core model.CPUCore, _ types.Info) model.CoreType {
	switch classifyAMDTopologyType(core.TopologyType) {
	case model.CorePerformance, model.CoreEfficiency:
		return classifyAMDTopologyType(core.TopologyType)
	}
	return model.CoreUnknown
}

func classifyAMDTopologyType(raw string) model.CoreType {
	switch {
	case strings.Contains(raw, "performance"), raw == "core", strings.Contains(raw, "big"):
		return model.CorePerformance
	case strings.Contains(raw, "efficiency"), strings.Contains(raw, "atom"), strings.Contains(raw, "little"):
		return model.CoreEfficiency
	default:
		return model.CoreUnknown
	}
}

func SupportsAMD(info types.Info) bool {
	vendor := strings.ToLower(info.Vendor)
	return strings.Contains(vendor, "amd") || strings.Contains(vendor, "authenticamd")
}
