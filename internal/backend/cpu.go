package backend

import (
	"fmt"
	"github.com/aayushkdev/perfmon/internal/backend/cpu"
	"github.com/aayushkdev/perfmon/internal/backend/cpu/amd"
	"github.com/aayushkdev/perfmon/internal/backend/cpu/generic"
	"github.com/aayushkdev/perfmon/internal/backend/cpu/intel"
	"github.com/aayushkdev/perfmon/internal/model"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

func hybridDetected(cores []model.CPUCore) bool {
	seenPerf := false
	seenEff := false
	for _, core := range cores {
		if core.Type == model.CorePerformance {
			seenPerf = true
		}
		if core.Type == model.CoreEfficiency {
			seenEff = true
		}
		if seenPerf && seenEff {
			return true
		}
	}
	return false
}

type cpuInfo struct {
	vendor string
	model  string
}

func (i cpuInfo) public() cpu.Info {
	return cpu.Info{Vendor: i.vendor, Model: i.model}
}

func (c *Collector) readCPUInfo() cpuInfo {
	data, err := os.ReadFile(filepath.Join(c.proc, "cpuinfo"))
	if err != nil {
		return cpuInfo{}
	}
	info := cpuInfo{}
	for _, line := range strings.Split(string(data), "\n") {
		key, val, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		switch strings.TrimSpace(key) {
		case "vendor_id":
			if info.vendor == "" {
				info.vendor = strings.TrimSpace(val)
			}
		case "model name", "Hardware":
			if info.model == "" {
				info.model = strings.TrimSpace(val)
			}
		}
	}
	return info
}

func (c *Collector) buildCores(ids []int, stats map[int]cpuStat, info cpuInfo, classifier cpu.Classifier) []model.CPUCore {
	cores := make([]model.CPUCore, 0, len(ids))
	for _, id := range ids {
		stat, ok := stats[id]
		if !ok {
			stat = cpuStat{}
		}
		base := filepath.Join(c.sys, "devices/system/cpu", fmt.Sprintf("cpu%d", id))
		freqBase := filepath.Join(base, "cpufreq")
		topology := c.readCoreTopology(base)
		core := model.CPUCore{
			ID:              id,
			Online:          c.readOnline(base, id),
			Type:            model.CoreUnknown,
			PackageID:       topology.packageID,
			CoreID:          topology.coreID,
			DieID:           topology.dieID,
			NodeID:          topology.nodeID,
			Capacity:        topology.capacity,
			TopologyType:    topology.coreType,
			ThreadSiblings:  topology.threadSiblings,
			CoreSiblings:    topology.coreSiblings,
			UsagePercent:    usage(c.prev[id], stat),
			FrequencyMHz:    readKHzAsMHz(filepath.Join(freqBase, "scaling_cur_freq")),
			MinFrequencyMHz: readKHzAsMHz(filepath.Join(freqBase, "scaling_min_freq")),
			MaxFrequencyMHz: readKHzAsMHz(filepath.Join(freqBase, "scaling_max_freq")),
			Governor:        readString(filepath.Join(freqBase, "scaling_governor")),
			EPP:             readString(filepath.Join(filepath.Join(base, "cpufreq"), "energy_performance_preference")),
		}
		core.Type = classifier.Classify(core, info.public())
		cores = append(cores, core)
	}
	normalizeHybridTypes(cores)
	return cores
}

func (c *Collector) readCPUIDs() []int {
	for _, path := range []string{
		filepath.Join(c.sys, "devices/system/cpu/present"),
		filepath.Join(c.sys, "devices/system/cpu/possible"),
	} {
		ids := parseCPUList(readString(path))
		if len(ids) > 0 {
			return ids
		}
	}
	return nil
}

func sortedStatIDs(stats map[int]cpuStat) []int {
	ids := make([]int, 0, len(stats))
	for id := range stats {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	return ids
}

func (c *Collector) readOnline(base string, id int) bool {
	if id == 0 {
		return true
	}
	return readString(filepath.Join(base, "online")) != "0"
}

type coreTopology struct {
	packageID      int
	coreID         int
	dieID          int
	nodeID         int
	capacity       int
	coreType       string
	threadSiblings []int
	coreSiblings   []int
}

func (c *Collector) readCoreTopology(base string) coreTopology {
	top := coreTopology{
		packageID: -1,
		coreID:    -1,
		dieID:     -1,
		nodeID:    -1,
	}
	top.packageID = readInt(filepath.Join(base, "topology/physical_package_id"), -1)
	top.coreID = readInt(filepath.Join(base, "topology/core_id"), -1)
	top.dieID = readInt(filepath.Join(base, "topology/die_id"), -1)
	top.capacity = readInt(filepath.Join(base, "cpu_capacity"), 0)
	if top.capacity == 0 {
		top.capacity = readInt(filepath.Join(base, "topology/core_capacity"), 0)
	}
	top.coreType = strings.ToLower(readString(filepath.Join(base, "topology/core_type")))
	top.threadSiblings = parseCPUList(readString(filepath.Join(base, "topology/thread_siblings_list")))
	top.coreSiblings = parseCPUList(readString(filepath.Join(base, "topology/core_siblings_list")))
	top.nodeID = readNodeID(base)
	return top
}

func readNodeID(base string) int {
	matches, _ := filepath.Glob(filepath.Join(base, "node*"))
	for _, path := range matches {
		name := filepath.Base(path)
		if id, ok := strings.CutPrefix(name, "node"); ok {
			if parsed, err := strconv.Atoi(id); err == nil {
				return parsed
			}
		}
	}
	return -1
}

func normalizeHybridTypes(cores []model.CPUCore) {
	if hasKnownCoreTypes(cores) {
		return
	}
	if applyTypeHintsFromTopology(cores) {
		return
	}
	applyTypeHintsFromCapacity(cores)
}

func hasKnownCoreTypes(cores []model.CPUCore) bool {
	for _, core := range cores {
		if core.Type == model.CorePerformance || core.Type == model.CoreEfficiency {
			return true
		}
	}
	return false
}

func applyTypeHintsFromTopology(cores []model.CPUCore) bool {
	seenPerf := false
	seenEff := false
	for i := range cores {
		switch classifyTopologyHint(cores[i].TopologyType) {
		case model.CorePerformance:
			cores[i].Type = model.CorePerformance
			seenPerf = true
		case model.CoreEfficiency:
			cores[i].Type = model.CoreEfficiency
			seenEff = true
		}
	}
	return seenPerf && seenEff
}

func applyTypeHintsFromCapacity(cores []model.CPUCore) bool {
	minCap := 0
	maxCap := 0
	seen := false
	for _, core := range cores {
		if core.Capacity <= 0 {
			continue
		}
		if !seen || core.Capacity < minCap {
			minCap = core.Capacity
		}
		if !seen || core.Capacity > maxCap {
			maxCap = core.Capacity
		}
		seen = true
	}
	if !seen || minCap == maxCap {
		return false
	}
	seenPerf := false
	seenEff := false
	for i := range cores {
		switch {
		case cores[i].Capacity == maxCap:
			cores[i].Type = model.CorePerformance
			seenPerf = true
		case cores[i].Capacity == minCap:
			cores[i].Type = model.CoreEfficiency
			seenEff = true
		}
	}
	return seenPerf && seenEff
}

func buildTopology(cores []model.CPUCore) model.CPUTopology {
	topology := model.CPUTopology{}
	packages := map[int]struct{}{}
	nodes := map[int]struct{}{}
	phys := map[string]struct{}{}
	for _, core := range cores {
		if core.PackageID >= 0 {
			packages[core.PackageID] = struct{}{}
		}
		if core.NodeID >= 0 {
			nodes[core.NodeID] = struct{}{}
		}
		if core.PackageID >= 0 || core.CoreID >= 0 || core.DieID >= 0 {
			phys[fmt.Sprintf("%d:%d:%d", core.PackageID, core.DieID, core.CoreID)] = struct{}{}
		}
		if core.PackageID >= 0 || core.CoreID >= 0 || core.NodeID >= 0 || core.DieID >= 0 {
			topology.Known = true
		}
	}
	topology.Packages = len(packages)
	topology.NUMANodes = len(nodes)
	topology.PhysicalCores = len(phys)
	topology.LogicalCores = len(cores)
	if topology.PhysicalCores > 0 {
		topology.ThreadsPerCore = float64(topology.LogicalCores) / float64(topology.PhysicalCores)
	}
	return topology
}

func classifyTopologyHint(raw string) model.CoreType {
	switch {
	case strings.Contains(raw, "performance"), raw == "core", strings.Contains(raw, "big"):
		return model.CorePerformance
	case strings.Contains(raw, "efficiency"), strings.Contains(raw, "atom"), strings.Contains(raw, "little"):
		return model.CoreEfficiency
	case raw == "1", raw == "2":
		return model.CorePerformance
	case raw == "3", raw == "4":
		return model.CoreEfficiency
	default:
		return model.CoreUnknown
	}
}

func (c *Collector) selectCPUClassifier(info cpuInfo) cpu.Classifier {
	public := info.public()
	vendor := strings.ToLower(public.Vendor)
	if strings.Contains(vendor, "intel") || strings.Contains(vendor, "genuineintel") {
		return intel.New(c.sys)
	}
	if amd.Supports(public) {
		return amd.New()
	}
	return generic.New()
}
