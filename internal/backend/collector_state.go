package backend

import (
	"github.com/aayushkdev/perfmon/internal/model"
	"sync"
	"time"
)

type Collector struct {
	proc     string
	sys      string
	mu       sync.Mutex
	prev     map[int]cpuStat
	raplPrev map[string]raplSample
	// process accounting state
	prevProcJiffies  map[int]uint64
	prevTotalJiffies uint64
	processes        []model.Process
}

// Optional interface: collectors may implement this to expose a process list
// for the UI process viewer.

type cpuStat struct {
	idle  uint64
	total uint64
}

type raplSample struct {
	energy uint64
	at     time.Time
}

func NewCollector(proc, sys string) *Collector {
	return &Collector{
		proc:            proc,
		sys:             sys,
		prev:            make(map[int]cpuStat),
		raplPrev:        make(map[string]raplSample),
		prevProcJiffies: make(map[int]uint64),
	}
}
