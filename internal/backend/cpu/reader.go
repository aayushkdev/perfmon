package cpu

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/aayushkdev/perfmon/internal/backend/cpu/vendors"
	"github.com/aayushkdev/perfmon/internal/backend/fs"
	"github.com/aayushkdev/perfmon/internal/model"
)

type Reader struct {
	Proc     string
	Sys      string
	Previous map[int]Stat
}

type Stat struct {
	Idle  uint64
	Total uint64
}

func (r *Reader) ReadStats() (map[int]Stat, Stat, error) {
	data, err := os.ReadFile(filepath.Join(r.Proc, "stat"))
	if err != nil {
		return nil, Stat{}, err
	}
	stats := make(map[int]Stat)
	var aggregate Stat

	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 8 || !strings.HasPrefix(fields[0], "cpu") {
			continue
		}
		stat, err := parseStat(fields[1:])
		if err != nil {
			continue
		}
		if fields[0] == "cpu" {
			aggregate = stat
			continue
		}
		id, err := strconv.Atoi(strings.TrimPrefix(fields[0], "cpu"))
		if err == nil {
			stats[id] = stat
		}
	}
	if len(stats) == 0 {
		return nil, Stat{}, errors.New("no CPU stats found")
	}
	return stats, aggregate, nil
}

func parseStat(fields []string) (Stat, error) {
	var values [10]uint64
	for i := range values {
		if i >= len(fields) {
			break
		}
		v, err := strconv.ParseUint(fields[i], 10, 64)
		if err != nil {
			return Stat{}, err
		}
		values[i] = v
	}
	idle := values[3] + values[4]
	total := uint64(0)
	for _, v := range values {
		total += v
	}
	return Stat{Idle: idle, Total: total}, nil
}

func Usage(prev, cur Stat) float64 {
	if prev.Total == 0 || cur.Total <= prev.Total {
		return 0
	}
	totalDelta := cur.Total - prev.Total
	idleDelta := cur.Idle - prev.Idle
	if totalDelta == 0 || idleDelta > totalDelta {
		return 0
	}
	return float64(totalDelta-idleDelta) * 100 / float64(totalDelta)
}

func HybridDetected(cores []model.CPUCore) bool {
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

type ReaderInfo struct {
	vendor string
	model  string
}

func (i ReaderInfo) Public() Info {
	return Info{Vendor: i.vendor, Model: i.model}
}

func (r *Reader) ReadInfo() ReaderInfo {
	data, err := os.ReadFile(filepath.Join(r.Proc, "cpuinfo"))
	if err != nil {
		return ReaderInfo{}
	}
	info := ReaderInfo{}
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

func (r *Reader) BuildCores(ids []int, stats map[int]Stat, info ReaderInfo, classifier Classifier) []model.CPUCore {
	cores := make([]model.CPUCore, 0, len(ids))
	for _, id := range ids {
		stat, ok := stats[id]
		if !ok {
			stat = Stat{}
		}
		base := filepath.Join(r.Sys, "devices/system/cpu", fmt.Sprintf("cpu%d", id))
		freqBase := filepath.Join(base, "cpufreq")
		topology := r.readCoreTopology(base)
		core := model.CPUCore{
			ID:              id,
			Online:          r.readOnline(base, id),
			Type:            model.CoreUnknown,
			PackageID:       topology.packageID,
			CoreID:          topology.coreID,
			DieID:           topology.dieID,
			NodeID:          topology.nodeID,
			Capacity:        topology.capacity,
			TopologyType:    topology.coreType,
			ThreadSiblings:  topology.threadSiblings,
			CoreSiblings:    topology.coreSiblings,
			UsagePercent:    Usage(r.Previous[id], stat),
			FrequencyMHz:    fs.KHzAsMHz(filepath.Join(freqBase, "scaling_cur_freq")),
			MinFrequencyMHz: fs.KHzAsMHz(filepath.Join(freqBase, "scaling_min_freq")),
			MaxFrequencyMHz: fs.KHzAsMHz(filepath.Join(freqBase, "scaling_max_freq")),
			Governor:        fs.ReadString(filepath.Join(freqBase, "scaling_governor")),
			EPP:             fs.ReadString(filepath.Join(filepath.Join(base, "cpufreq"), "energy_performance_preference")),
		}
		core.Type = classifier.Classify(core, info.Public())
		cores = append(cores, core)
	}
	NormalizeHybridTypes(cores)
	return cores
}

func (r *Reader) ReadCPUIDs() []int {
	for _, path := range []string{
		filepath.Join(r.Sys, "devices/system/cpu/present"),
		filepath.Join(r.Sys, "devices/system/cpu/possible"),
	} {
		ids := parseCPUList(fs.ReadString(path))
		if len(ids) > 0 {
			return ids
		}
	}
	return nil
}

func parseCPUList(value string) []int {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	ids := make([]int, 0)
	for _, part := range strings.Split(value, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if start, end, ok := strings.Cut(part, "-"); ok {
			s, err1 := strconv.Atoi(strings.TrimSpace(start))
			e, err2 := strconv.Atoi(strings.TrimSpace(end))
			if err1 != nil || err2 != nil || e < s {
				continue
			}
			for i := s; i <= e; i++ {
				ids = append(ids, i)
			}
			continue
		}
		id, err := strconv.Atoi(part)
		if err == nil {
			ids = append(ids, id)
		}
	}
	return ids
}

func SortedStatIDs(stats map[int]Stat) []int {
	ids := make([]int, 0, len(stats))
	for id := range stats {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	return ids
}

func (r *Reader) readOnline(base string, id int) bool {
	if id == 0 {
		return true
	}
	return fs.ReadString(filepath.Join(base, "online")) != "0"
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

func (r *Reader) readCoreTopology(base string) coreTopology {
	top := coreTopology{
		packageID: -1,
		coreID:    -1,
		dieID:     -1,
		nodeID:    -1,
	}
	top.packageID = fs.ReadInt(filepath.Join(base, "topology/physical_package_id"), -1)
	top.coreID = fs.ReadInt(filepath.Join(base, "topology/core_id"), -1)
	top.dieID = fs.ReadInt(filepath.Join(base, "topology/die_id"), -1)
	top.capacity = fs.ReadInt(filepath.Join(base, "cpu_capacity"), 0)
	if top.capacity == 0 {
		top.capacity = fs.ReadInt(filepath.Join(base, "topology/core_capacity"), 0)
	}
	top.coreType = strings.ToLower(fs.ReadString(filepath.Join(base, "topology/core_type")))
	top.threadSiblings = parseCPUList(fs.ReadString(filepath.Join(base, "topology/thread_siblings_list")))
	top.coreSiblings = parseCPUList(fs.ReadString(filepath.Join(base, "topology/core_siblings_list")))
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

func NormalizeHybridTypes(cores []model.CPUCore) {
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

func BuildTopology(cores []model.CPUCore) model.CPUTopology {
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

func (r *Reader) SelectCPUClassifier(info ReaderInfo) Classifier {
	public := info.Public()
	vendor := strings.ToLower(public.Vendor)
	if strings.Contains(vendor, "intel") || strings.Contains(vendor, "genuineintel") {
		return vendors.NewIntel(r.Sys)
	}
	if vendors.SupportsAMD(public) {
		return vendors.NewAMD()
	}
	return vendors.NewGeneric()
}
