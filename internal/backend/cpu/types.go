package cpu

import (
	"github.com/aayushkdev/perfmon/internal/backend/cpu/types"
	"github.com/aayushkdev/perfmon/internal/model"
)

type Info = types.Info
type Classifier = types.Classifier

func HasHybridHints(cores []model.CPUCore) bool {
	seen := map[model.CoreType]bool{}
	for _, core := range cores {
		if core.Type != model.CoreUnknown {
			seen[core.Type] = true
		}
	}
	return seen[model.CorePerformance] && seen[model.CoreEfficiency]
}
