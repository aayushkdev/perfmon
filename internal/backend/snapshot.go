package backend

import (
	"context"
	"github.com/aayushkdev/perfmon/internal/backend/cpu"
	"github.com/aayushkdev/perfmon/internal/backend/gpu"
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

	stats, total, err := c.readCPUStats()
	if err != nil {
		return model.Snapshot{}, err
	}

	info := c.readCPUInfo()
	classifier := c.selectCPUClassifier(info)
	coreIDs := c.readCPUIDs()
	if len(coreIDs) == 0 {
		coreIDs = sortedStatIDs(stats)
	}
	cores := c.buildCores(coreIDs, stats, info, classifier)
	sort.Slice(cores, func(i, j int) bool { return cores[i].ID < cores[j].ID })
	thermals := c.readThermals()
	powerDomains := c.readPowerDomains()
	// Aggregate package power into a single CPU power estimate when available.
	var cpuPower float64
	var cpuPowerSeen bool
	for _, d := range powerDomains {
		if d.PowerW != nil {
			cpuPower += *d.PowerW
			cpuPowerSeen = true
		}
	}
	// cpuPower will be attached to the snapshot below when constructing the
	// model.CPU value.
	batteries, acOnline := c.readBatteries()

	snap := model.Snapshot{
		Timestamp: time.Now(),
		Host: model.Host{
			Kernel:       kernelRelease(),
			Architecture: machineArch(),
		},
		CPU: model.CPU{
			Vendor:       info.vendor,
			Model:        info.model,
			Architecture: machineArch(),
			Driver:       c.readCPUDriver(),
			Topology:     buildTopology(cores),
			Hybrid:       cpu.HasHybridHints(cores),
			HybridKnown:  hybridDetected(cores),
			UsagePercent: usage(c.prev[-1], total),
			PowerProfile: c.readPowerProfile(),
			Cores:        cores,
			TemperatureC: firstThermalTemperature(thermals),
			PowerW:       nil,
			Governors:    c.readAvailableGovernors(),
			EPPChoices:   c.readEPPChoices(),
			ActiveGov:    firstNonEmptyGovernor(cores),
			EPP:          firstNonEmptyEPP(cores),
			TurboEnabled: c.readTurboEnabled(),
		},
		Thermals:     thermals,
		Power:        powerDomains,
		Batteries:    batteries,
		ACOnline:     acOnline,
		Memory:       c.readMemory(),
		GPUs:         gpu.Read(c.sys),
		Capabilities: c.capabilities(),
	}

	// attach aggregated CPU power if available
	if cpuPowerSeen {
		snap.CPU.PowerW = &cpuPower
	}

	// update processes snapshot (best-effort)
	c.updateProcesses()
	snap.Processes = c.processes

	for id, stat := range stats {
		c.prev[id] = stat
	}
	c.prev[-1] = total
	return snap, nil
}

// Processes returns a copy of the last collected process list. It allows the
// UI to query process data directly if needed.
func (c *Collector) Processes() []model.Process {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]model.Process, len(c.processes))
	copy(out, c.processes)
	return out
}
