package cpu

import "github.com/aayushkdev/perfmon/internal/model"

type Info struct {
	Vendor string
	Model  string
}

type Classifier interface {
	Name() string
	Classify(core model.CPUCore, info Info) model.CoreType
}

func HasHybridHints(cores []model.CPUCore) bool {
	seen := map[model.CoreType]bool{}
	for _, core := range cores {
		if core.Type != model.CoreUnknown {
			seen[core.Type] = true
		}
	}
	return seen[model.CorePerformance] && seen[model.CoreEfficiency]
}
