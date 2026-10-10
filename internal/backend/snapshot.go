package backend

import (
	"context"
	"github.com/aayushkdev/perfmon/internal/backend/battery"
	"github.com/aayushkdev/perfmon/internal/backend/capabilities"
	"github.com/aayushkdev/perfmon/internal/backend/cpu"
	"github.com/aayushkdev/perfmon/internal/backend/fs"
	"github.com/aayushkdev/perfmon/internal/backend/gpu"
	"github.com/aayushkdev/perfmon/internal/backend/memory"
	"github.com/aayushkdev/perfmon/internal/backend/power"
	"github.com/aayushkdev/perfmon/internal/backend/process"
	"github.com/aayushkdev/perfmon/internal/backend/thermal"
	"github.com/aayushkdev/perfmon/internal/model"
	"sort"
	"time"
)

func (c *Collector) Snapshot(ctx context.Context) (model.Snapshot, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	select {
	case <-ctx.Done():
		return model.Snapshot{}, ctx.Err()
	default:
	}

	cpuReader := cpu.Reader{Proc: c.proc, Sys: c.sys, Previous: c.prev}
	stats, total, err := cpuReader.ReadStats()
	if err != nil {
		return model.Snapshot{}, err
	}

	info := cpuReader.ReadInfo()
	classifier := cpuReader.SelectCPUClassifier(info)
	coreIDs := cpuReader.ReadCPUIDs()
	if len(coreIDs) == 0 {
		coreIDs = cpu.SortedStatIDs(stats)
	}
	cores := cpuReader.BuildCores(coreIDs, stats, info, classifier)
	sort.Slice(cores, func(i, j int) bool { return cores[i].ID < cores[j].ID })
	thermals := thermal.Read(c.sys)
	powerDomains := power.Read(c.sys, c.raplPrev)
	var cpuPower float64
	var cpuPowerSeen bool
	for _, d := range powerDomains {
		if d.PowerW != nil {
			cpuPower += *d.PowerW
			cpuPowerSeen = true
		}
	}
	batteries, acOnline := battery.Read(c.sys)

	snap := model.Snapshot{
		Timestamp: time.Now(),
		Host: model.Host{
			Kernel:       fs.KernelRelease(),
			Architecture: fs.MachineArch(),
		},
		CPU: model.CPU{
			Vendor:       info.Public().Vendor,
			Model:        info.Public().Model,
			Architecture: fs.MachineArch(),
			Driver:       c.readCPUDriver(),
			Topology:     cpu.BuildTopology(cores),
			Hybrid:       cpu.HasHybridHints(cores),
			HybridKnown:  cpu.HybridDetected(cores),
			UsagePercent: cpu.Usage(c.prev[-1], total),
			PowerProfile: c.readPowerProfile(),
			Cores:        cores,
			TemperatureC: thermal.FirstTemperature(thermals),
			PowerW:       nil,
			Governors:    c.readAvailableGovernors(),
			EPPChoices:   c.readEPPChoices(),
			ActiveGov:    fs.FirstNonEmptyGovernor(cores),
			EPP:          fs.FirstNonEmptyEPP(cores),
			TurboEnabled: c.readTurboEnabled(),
		},
		Thermals:     thermals,
		Power:        powerDomains,
		Batteries:    batteries,
		ACOnline:     acOnline,
		Memory:       memory.Read(c.proc, c.sys),
		GPUs:         gpu.Read(c.sys),
		Capabilities: capabilities.Read(c.sys),
	}

	if cpuPowerSeen {
		snap.CPU.PowerW = &cpuPower
	}

	processes, prevProcJiffies, prevTotalJiffies := process.Scan(c.proc, c.prevProcJiffies, c.prevTotalJiffies)
	c.processes = processes
	c.prevProcJiffies = prevProcJiffies
	c.prevTotalJiffies = prevTotalJiffies
	snap.Processes = c.processes

	for id, stat := range stats {
		c.prev[id] = stat
	}
	c.prev[-1] = total
	return snap, nil
}

func (c *Collector) Processes() []model.Process {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]model.Process, len(c.processes))
	copy(out, c.processes)
	return out
}
