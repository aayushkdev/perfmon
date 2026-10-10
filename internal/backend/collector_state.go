package backend

import (
	"github.com/aayushkdev/perfmon/internal/backend/cpu"
	"github.com/aayushkdev/perfmon/internal/backend/power"
	"github.com/aayushkdev/perfmon/internal/model"
	"sync"
)

type Collector struct {
	proc             string
	sys              string
	mu               sync.Mutex
	prev             map[int]cpu.Stat
	raplPrev         map[string]power.Sample
	prevProcJiffies  map[int]uint64
	prevTotalJiffies uint64
	processes        []model.Process
}

func NewCollector(proc, sys string) *Collector {
	return &Collector{
		proc:            proc,
		sys:             sys,
		prev:            make(map[int]cpu.Stat),
		raplPrev:        make(map[string]power.Sample),
		prevProcJiffies: make(map[int]uint64),
	}
}
