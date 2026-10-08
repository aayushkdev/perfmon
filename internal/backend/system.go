package backend

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/aayushkdev/perfmon/internal/backend/cpu"
	"github.com/aayushkdev/perfmon/internal/backend/cpu/amd"
	"github.com/aayushkdev/perfmon/internal/backend/cpu/generic"
	"github.com/aayushkdev/perfmon/internal/backend/cpu/intel"
	"github.com/aayushkdev/perfmon/internal/backend/gpu"
	"github.com/aayushkdev/perfmon/internal/model"
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
type ProcessLister interface {
	Processes() []model.Process
}

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

func (c *Collector) readCPUStats() (map[int]cpuStat, cpuStat, error) {
	data, err := os.ReadFile(filepath.Join(c.proc, "stat"))
	if err != nil {
		return nil, cpuStat{}, err
	}
	stats := make(map[int]cpuStat)
	var aggregate cpuStat

	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 8 || !strings.HasPrefix(fields[0], "cpu") {
			continue
		}
		stat, err := parseCPUStat(fields[1:])
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
		return nil, cpuStat{}, errors.New("no CPU stats found")
	}
	return stats, aggregate, nil
}

func parseCPUStat(fields []string) (cpuStat, error) {
	var values [10]uint64
	for i := range values {
		if i >= len(fields) {
			break
		}
		v, err := strconv.ParseUint(fields[i], 10, 64)
		if err != nil {
			return cpuStat{}, err
		}
		values[i] = v
	}
	idle := values[3] + values[4]
	total := uint64(0)
	for _, v := range values {
		total += v
	}
	return cpuStat{idle: idle, total: total}, nil
}

func usage(prev, cur cpuStat) float64 {
	if prev.total == 0 || cur.total <= prev.total {
		return 0
	}
	totalDelta := cur.total - prev.total
	idleDelta := cur.idle - prev.idle
	if totalDelta == 0 || idleDelta > totalDelta {
		return 0
	}
	return float64(totalDelta-idleDelta) * 100 / float64(totalDelta)
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

func (c *Collector) SetCoreOnline(ctx context.Context, id int, online bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	if id == 0 && !online {
		return errors.New("cpu0 cannot be offlined safely")
	}
	path := c.coreOnlinePath(id)
	if !c.CanSetCoreOnline(id) {
		return fmt.Errorf("core %d online control is unavailable", id)
	}
	value := "0"
	if online {
		value = "1"
	}
	return os.WriteFile(path, []byte(value), 0o644)
}

func (c *Collector) CanSetCoreOnline(id int) bool {
	if id == 0 {
		return false
	}
	info, err := os.Stat(c.coreOnlinePath(id))
	return err == nil && !info.IsDir()
}

func (c *Collector) coreOnlinePath(id int) string {
	return filepath.Join(c.sys, "devices/system/cpu", fmt.Sprintf("cpu%d", id), "online")
}

func (c *Collector) SetCPUGovernor(ctx context.Context, governor string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.writeToCPUFiles(ctx, "scaling_governor", governor); err != nil {
		return err
	}
	return nil
}

func (c *Collector) SetEPP(ctx context.Context, preference string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if eppRequiresPowersaveGovernor(c.readCPUDriver()) && preference != "performance" {
		if err := c.writeToCPUFiles(ctx, "scaling_governor", "powersave"); err != nil {
			return fmt.Errorf("EPP %q requires the powersave governor: %w", preference, err)
		}
	}
	if err := c.writeToCPUFiles(ctx, "energy_performance_preference", preference); err != nil {
		return err
	}
	return nil
}

func eppRequiresPowersaveGovernor(driver string) bool {
	switch driver {
	case "intel_pstate", "amd-pstate", "amd-pstate-epp":
		return true
	default:
		return false
	}
}

func (c *Collector) SetTurboEnabled(ctx context.Context, enabled bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	for _, path := range []string{
		filepath.Join(c.sys, "devices/system/cpu/intel_pstate/no_turbo"),
		filepath.Join(c.sys, "devices/system/cpu/amd_pstate/no_turbo"),
		filepath.Join(c.sys, "devices/system/cpu/cpufreq/boost"),
	} {
		if _, err := os.Stat(path); err != nil {
			continue
		}
		value := "0"
		if !enabled {
			value = "1"
		}
		if strings.HasSuffix(path, "boost") {
			value = "0"
			if enabled {
				value = "1"
			}
		}
		if err := writeString(path, value); err == nil {
			return nil
		}
	}
	return errors.New("turbo control is unavailable")
}

func (c *Collector) SetPowerProfile(ctx context.Context, profile string) error {
	if path, value, ok := c.platformProfileWriteTarget(profile); ok {
		return writeString(path, value)
	}
	switch normalizePowerProfile(profile) {
	case "performance":
		if err := c.SetCPUGovernor(ctx, pickSupported("performance", c.readAvailableGovernors(), "schedutil", "ondemand")); err != nil {
			return err
		}
		if err := c.SetEPP(ctx, pickSupported("performance", c.readEPPChoices(), "balance_performance", "balance_power")); err != nil {
			return err
		}
		return c.SetTurboEnabled(ctx, true)
	case "balanced":
		if err := c.SetCPUGovernor(ctx, pickSupported("schedutil", c.readAvailableGovernors(), "ondemand", "powersave", "performance")); err != nil {
			return err
		}
		if err := c.SetEPP(ctx, pickSupported("balance_performance", c.readEPPChoices(), "balance_power", "performance")); err != nil {
			return err
		}
		return nil
	case "powersave":
		if err := c.SetCPUGovernor(ctx, pickSupported("powersave", c.readAvailableGovernors(), "schedutil", "ondemand")); err != nil {
			return err
		}
		if err := c.SetEPP(ctx, pickSupported("power", c.readEPPChoices(), "balance_power", "balance_performance")); err != nil {
			return err
		}
		return c.SetTurboEnabled(ctx, false)
	default:
		return fmt.Errorf("unsupported power profile %q", profile)
	}
}

func (c *Collector) platformProfileWriteTarget(profile string) (string, string, bool) {
	path := filepath.Join(c.sys, "firmware/acpi/platform_profile")
	choices := c.readPowerProfileChoices()
	switch normalizePowerProfile(profile) {
	case "powersave":
		for _, choice := range choices {
			if normalizePowerProfile(choice) == "powersave" {
				return path, choice, true
			}
		}
	case "balanced":
		for _, choice := range choices {
			if normalizePowerProfile(choice) == "balanced" {
				return path, choice, true
			}
		}
	case "performance":
		for _, choice := range choices {
			if normalizePowerProfile(choice) == "performance" {
				return path, choice, true
			}
		}
	}
	return "", "", false
}

func (c *Collector) writeToCPUFiles(ctx context.Context, suffix, value string) error {
	files := make([]string, 0, 8)
	if matches, err := filepath.Glob(filepath.Join(c.sys, "devices/system/cpu/cpu*/cpufreq", suffix)); err == nil {
		files = append(files, matches...)
	}
	if matches, err := filepath.Glob(filepath.Join(c.sys, "devices/system/cpu/cpufreq/policy*/", suffix)); err == nil {
		files = append(files, matches...)
	}
	if len(files) == 0 {
		return fmt.Errorf("%s control is unavailable", suffix)
	}
	written := false
	var lastErr error
	failed := false
	for _, path := range uniqueParentFiles(files) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if err := writeString(path, value); err == nil {
			written = true
		} else {
			failed = true
			lastErr = fmt.Errorf("%s/%s: %w", filepath.Base(filepath.Dir(path)), filepath.Base(path), err)
		}
	}
	if failed {
		return fmt.Errorf("failed to apply %s=%q consistently: %w", suffix, value, lastErr)
	}
	if !written {
		if lastErr != nil {
			return fmt.Errorf("failed to write %s=%q: %w", suffix, value, lastErr)
		}
		return fmt.Errorf("failed to write %s=%q", suffix, value)
	}
	return nil
}

func uniqueParentFiles(paths []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		dir := filepath.Dir(path)
		if seen[dir] {
			continue
		}
		seen[dir] = true
		out = append(out, path)
	}
	return out
}

func normalizePowerProfile(profile string) string {
	switch strings.ToLower(strings.TrimSpace(profile)) {
	case "low-power", "powersave":
		return "powersave"
	case "balanced", "balance":
		return "balanced"
	case "performance", "perf":
		return "performance"
	default:
		return strings.ToLower(strings.TrimSpace(profile))
	}
}

func pickSupported(preferred string, supported []string, fallbacks ...string) string {
	for _, item := range supported {
		if item == preferred {
			return preferred
		}
	}
	for _, fallback := range fallbacks {
		for _, item := range supported {
			if item == fallback {
				return item
			}
		}
	}
	if len(supported) > 0 {
		return supported[0]
	}
	return preferred
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

func firstFieldsFromGlobs(patterns ...string) []string {
	for _, pattern := range patterns {
		matches, _ := filepath.Glob(pattern)
		for _, path := range matches {
			fields := strings.Fields(readString(path))
			if len(fields) > 0 {
				return fields
			}
		}
	}
	return nil
}

func dedupeSortedFieldsFromGlobs(patterns ...string) []string {
	seen := map[string]struct{}{}
	for _, pattern := range patterns {
		matches, _ := filepath.Glob(pattern)
		for _, path := range matches {
			for _, field := range strings.Fields(readString(path)) {
				if field == "" {
					continue
				}
				seen[field] = struct{}{}
			}
		}
	}
	if len(seen) == 0 {
		return nil
	}
	out := make([]string, 0, len(seen))
	for field := range seen {
		out = append(out, field)
	}
	sort.Strings(out)
	return out
}

func (c *Collector) readMemory() model.Memory {
	values := readMeminfo(filepath.Join(c.proc, "meminfo"))
	total := values["MemTotal"] * 1024
	available := values["MemAvailable"] * 1024
	free := values["MemFree"] * 1024
	if available == 0 {
		available = free
	}
	swapTotal := values["SwapTotal"] * 1024
	swapFree := values["SwapFree"] * 1024
	mem := model.Memory{
		TotalBytes:     total,
		AvailableBytes: available,
		SwapTotalBytes: swapTotal,
	}
	if total > available {
		mem.UsedBytes = total - available
	}
	if swapTotal > swapFree {
		mem.SwapUsedBytes = swapTotal - swapFree
	}
	// PSI removed: do not populate pressure fields
	// Best-effort: try to detect DIMM/module count and speed from EDAC sysfs
	// entries (varies by kernel and platform). This is non-fatal — if nothing
	// is found the fields remain nil.
	if modules, speed := c.readMemoryModules(); modules > 0 || speed > 0 {
		if modules > 0 {
			mem.ModuleCount = &modules
		}
		if speed > 0 {
			mem.SpeedMHz = &speed
		}
	} else {
		// If sysfs didn't yield results, try dmidecode as a root-only fallback.
		if mod, spd := c.dmidecodeFallback(); mod > 0 || spd > 0 {
			if mod > 0 {
				mem.ModuleCount = &mod
			}
			if spd > 0 {
				mem.SpeedMHz = &spd
			}
		}
	}
	return mem
}

// updateProcesses scans /proc and computes simple CPU% and memory metrics.
// This is intentionally conservative: it reads minimal per-PID data and
// tolerates permission errors. It keeps previous jiffies to compute deltas.
func (c *Collector) updateProcesses() {
	// read total jiffies from /proc/stat
	data, err := os.ReadFile(filepath.Join(c.proc, "stat"))
	if err != nil {
		return
	}
	var totalNow uint64
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "cpu ") {
			fields := strings.Fields(line)
			for _, f := range fields[1:] {
				if v, err := strconv.ParseUint(f, 10, 64); err == nil {
					totalNow += v
				}
			}
			break
		}
	}
	// read mem total
	memTotal := uint64(0)
	memData, _ := os.ReadFile(filepath.Join(c.proc, "meminfo"))
	for _, line := range strings.Split(string(memData), "\n") {
		if strings.HasPrefix(line, "MemTotal:") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				if v, err := strconv.ParseUint(parts[1], 10, 64); err == nil {
					// value is in kB
					memTotal = v * 1024
				}
			}
			break
		}
	}

	pids, _ := filepath.Glob(filepath.Join(c.proc, "[0-9]*"))
	procs := make([]model.Process, 0, len(pids))
	procJiffies := make(map[int]uint64)
	for _, p := range pids {
		pidStr := filepath.Base(p)
		pid, err := strconv.Atoi(pidStr)
		if err != nil {
			continue
		}
		statPath := filepath.Join(p, "stat")
		bs, err := os.ReadFile(statPath)
		if err != nil {
			continue
		}
		// parse /proc/<pid>/stat robustly: comm can contain spaces and is
		// enclosed in parentheses. We'll extract comm and then split the
		// remaining fields.
		utime, stime, rssPages, name, ok := parseProcStat(bs)
		if !ok {
			continue
		}
		// compute jiffies
		pj := utime + stime
		procJiffies[pid] = pj
		// compute deltas
		var cpuPct float64
		if prevTotal := c.prevTotalJiffies; prevTotal > 0 && totalNow > prevTotal {
			deltaTotal := float64(totalNow - prevTotal)
			prevP := c.prevProcJiffies[pid]
			deltaP := float64(0)
			if pj > prevP {
				deltaP = float64(pj - prevP)
			}
			if deltaTotal > 0 {
				cpuPct = 100.0 * deltaP / deltaTotal
			}
		}
		// rss: pages -> bytes. Use the system page size instead of assuming 4KiB.
		rssBytes := uint64(0)
		if rssPages > 0 {
			rssBytes = rssPages * uint64(syscall.Getpagesize())
		}
		memPct := 0.0
		if memTotal > 0 {
			memPct = float64(rssBytes) * 100.0 / float64(memTotal)
		}
		procs = append(procs, model.Process{
			PID:        pid,
			Name:       name,
			CPUPercent: cpuPct,
			MemPercent: memPct,
			RSSBytes:   rssBytes,
		})
	}
	// sort by memory (RSS) desc by default
	sort.Slice(procs, func(i, j int) bool { return procs[i].RSSBytes > procs[j].RSSBytes })
	c.processes = procs
	c.prevProcJiffies = procJiffies
	c.prevTotalJiffies = totalNow
}

// parseProcStat parses the raw contents of /proc/<pid>/stat. The comm field
// (process name) is enclosed in parentheses and may contain spaces, so we
// extract it carefully. Returns (utime, stime, rssPages, name, ok).
func parseProcStat(raw []byte) (uint64, uint64, uint64, string, bool) {
	// find the first '(' and last ')' which delimit comm
	l := bytes.IndexByte(raw, '(')
	r := bytes.LastIndexByte(raw, ')')
	if l < 0 || r < 0 || r <= l {
		return 0, 0, 0, "", false
	}
	name := string(raw[l+1 : r])
	// fields before '(' are pid, after ')' are the rest
	after := raw[r+1:]
	fields := strings.Fields(string(after))
	// according to procfs, utime is field 13, stime 14, rss is 24 relative to
	// the start of the whole line. After splitting like this, fields[11] is
	// utime (since fields starts at index 0 corresponding to field 3).
	// We need to ensure there are enough fields.
	if len(fields) < 22 { // need at least up to rss
		return 0, 0, 0, name, false
	}
	// utime: fields[11], stime: fields[12], rss: fields[21]
	utime, err1 := strconv.ParseUint(fields[11], 10, 64)
	stime, err2 := strconv.ParseUint(fields[12], 10, 64)
	rssPages, err3 := strconv.ParseUint(fields[21], 10, 64)
	if err1 != nil || err2 != nil || err3 != nil {
		return 0, 0, 0, name, false
	}
	return utime, stime, rssPages, name, true
}

// readMemoryModules attempts to read memory module count and common speed from
// EDAC-related sysfs entries. It returns (moduleCount, speedMHz). Both values
// are best-effort and may be zero when unavailable.
func (c *Collector) readMemoryModules() (int, int) {
	// Look for dimm entries under EDAC: devices/system/edac/mc*/csrow*/dimm*
	pattern := filepath.Join(c.sys, "devices", "system", "edac", "mc*", "csrow*", "dimm*")
	matches, _ := filepath.Glob(pattern)
	if len(matches) == 0 {
		return 0, 0
	}
	count := 0
	var speedFound int
	for _, dimm := range matches {
		// verify it's a directory
		if fi, err := os.Stat(dimm); err != nil || !fi.IsDir() {
			continue
		}
		count++
		// try common file names for speed
		for _, name := range []string{"speed", "dimm_speed", "max_speed", "bus_speed"} {
			path := filepath.Join(dimm, name)
			if raw := readString(path); raw != "" {
				// try parse MHz or kHz values
				if v, err := strconv.Atoi(strings.Fields(raw)[0]); err == nil && v > 0 {
					// if value looks like kHz (very large) convert
					if v > 10000 {
						v = v / 1000
					}
					speedFound = v
					break
				}
			}
		}
	}
	return count, speedFound
}

// dmidecodeFallback attempts to run dmidecode to extract memory device
// information. It requires root privileges. Returns (moduleCount, speedMHz)
// or (0,0) on failure.
func (c *Collector) dmidecodeFallback() (int, int) {
	out, err := exec.Command("dmidecode", "-t", "17").Output()
	if err != nil {
		return 0, 0
	}
	data := string(out)
	// parse blocks separated by blank lines; look for "Speed:" and count
	lines := strings.Split(data, "\n")
	moduleCount := 0
	speedMHz := 0
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "Speed:") {
			moduleCount++
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				// parts like: Speed: 2400 MHz or Speed: Unknown
				if v, err := strconv.Atoi(parts[1]); err == nil && v > 0 {
					speedMHz = v
				}
			}
		}
	}
	return moduleCount, speedMHz
}

func readMeminfo(path string) map[string]uint64 {
	data, err := os.ReadFile(path)
	if err != nil {
		return map[string]uint64{}
	}
	values := map[string]uint64{}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		key := strings.TrimSuffix(fields[0], ":")
		value, err := strconv.ParseUint(fields[1], 10, 64)
		if err == nil {
			values[key] = value
		}
	}
	return values
}

// PSI support removed. The kernel pressure files are not parsed anymore.

func (c *Collector) readCPUTemperature() *float64 {
	thermals := c.readThermals()
	return firstThermalTemperature(thermals)
}

func (c *Collector) readThermals() []model.ThermalSensor {
	sensors := make([]model.ThermalSensor, 0, 8)
	seenNames := make(map[string]bool)
	for _, path := range c.thermalSensorPaths() {
		base := filepath.Dir(path)
		name := readString(filepath.Join(base, "name"))
		if name == "" {
			name = readString(filepath.Join(base, "type"))
		}
		if name == "" {
			name = filepath.Base(base)
		}
		temp := readMilliTemperature(path)
		if temp == nil {
			continue
		}
		prefix := strings.TrimSuffix(filepath.Base(path), "_input")
		if label := readString(filepath.Join(base, prefix+"_label")); label != "" {
			name += ": " + label
		}
		sensor := model.ThermalSensor{
			Name:         name,
			Source:       thermalSourceLabel(base),
			TemperatureC: temp,
			MaxC:         readMilliTemperature(firstExistingPath(filepath.Join(base, prefix+"_max"), filepath.Join(base, "trip_point_0_temp"))),
			CriticalC:    readMilliTemperature(firstExistingPath(filepath.Join(base, prefix+"_crit"), filepath.Join(base, "trip_point_1_temp"))),
			Throttled:    readBoolField(firstExistingPath(filepath.Join(base, prefix+"_alarm"), filepath.Join(base, "temp_alarm"))),
		}
		key := strings.ToLower(strings.TrimSpace(sensor.Name))
		if sensor.Source == "thermal" && seenNames[key] {
			continue
		}
		seenNames[key] = true
		sensors = append(sensors, sensor)
	}
	return sensors
}

func (c *Collector) thermalSensorPaths() []string {
	matches := make([]string, 0, 16)
	if hwmon, _ := filepath.Glob(filepath.Join(c.sys, "class/hwmon/hwmon*/temp*_input")); len(hwmon) > 0 {
		matches = append(matches, hwmon...)
	}
	if zones, _ := filepath.Glob(filepath.Join(c.sys, "class/thermal/thermal_zone*/temp")); len(zones) > 0 {
		matches = append(matches, zones...)
	}
	return matches
}

func thermalSourceLabel(base string) string {
	switch {
	case strings.Contains(base, "hwmon"):
		return "hwmon"
	case strings.Contains(base, "thermal_zone"):
		return "thermal"
	default:
		return "sensor"
	}
}

func readMilliTemperature(path string) *float64 {
	raw := readString(path)
	if raw == "" {
		return nil
	}
	milli, err := strconv.ParseFloat(raw, 64)
	if err != nil || milli <= 0 {
		return nil
	}
	value := milli / 1000
	return &value
}

func firstThermalTemperature(sensors []model.ThermalSensor) *float64 {
	for _, sensor := range sensors {
		if sensor.TemperatureC != nil {
			return sensor.TemperatureC
		}
	}
	return nil
}

func (c *Collector) readPowerDomains() []model.PowerDomain {
	domains := make([]model.PowerDomain, 0, 8)
	matches, _ := filepath.Glob(filepath.Join(c.sys, "class/powercap/intel-rapl:*"))
	for _, base := range matches {
		energy := readMicroEnergy(filepath.Join(base, "energy_uj"))
		if energy == nil {
			continue
		}
		name := readString(filepath.Join(base, "name"))
		if name == "" {
			name = filepath.Base(base)
		}
		domain := model.PowerDomain{
			Name:      name,
			Source:    "rapl",
			PowerW:    c.readRAPLPower(base, energy),
			EnergyJ:   ujToJ(energy),
			LimitW:    readMicroPower(filepath.Join(base, "constraint_0_power_limit_uw")),
			MaxW:      readMicroPower(filepath.Join(base, "constraint_0_max_power_uw")),
			CriticalW: readMicroPower(filepath.Join(base, "constraint_0_crit_power_uw")),
		}
		domains = append(domains, domain)
	}
	return domains
}

// Note: hwmon-based CPU power reading was removed in favor of RAPL-only aggregation.

func (c *Collector) readRAPLPower(base string, energy *uint64) *float64 {
	key := filepath.Base(base)
	now := time.Now()
	prev, ok := c.raplPrev[key]
	c.raplPrev[key] = raplSample{energy: *energy, at: now}
	if !ok || prev.at.IsZero() || !now.After(prev.at) {
		return nil
	}
	delta := uint64Delta(*energy, prev.energy)
	seconds := now.Sub(prev.at).Seconds()
	if seconds <= 0 {
		return nil
	}
	watts := float64(delta) / 1e6 / seconds
	return &watts
}

func readMicroEnergy(path string) *uint64 {
	raw := readString(path)
	if raw == "" {
		return nil
	}
	value, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return nil
	}
	return &value
}

func readMicroPower(path string) *float64 {
	raw := readString(path)
	if raw == "" {
		return nil
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil || value <= 0 {
		return nil
	}
	watts := value / 1e6
	return &watts
}

func ujToJ(value *uint64) *float64 {
	if value == nil {
		return nil
	}
	j := float64(*value) / 1e6
	return &j
}

func uint64Delta(cur, prev uint64) uint64 {
	if cur >= prev {
		return cur - prev
	}
	return cur
}

func (c *Collector) readBatteries() ([]model.Battery, *bool) {
	matches, _ := filepath.Glob(filepath.Join(c.sys, "class/power_supply/*"))
	batteries := make([]model.Battery, 0, len(matches))
	var acOnline *bool
	for _, base := range matches {
		typ := strings.ToLower(readString(filepath.Join(base, "type")))
		name := filepath.Base(base)
		switch typ {
		case "battery":
			batt := model.Battery{
				Name:            name,
				Status:          readString(filepath.Join(base, "status")),
				CapacityPercent: readFloatField(filepath.Join(base, "capacity")),
				HealthPercent:   batteryHealthPercent(base),
				CycleCount:      readFloatField(filepath.Join(base, "cycle_count")),
				EnergyNowWh:     readEnergyWh(base, "energy_now", "charge_now"),
				EnergyFullWh:    readEnergyWh(base, "energy_full", "charge_full"),
				PowerW:          readPowerNowW(base),
				VoltageV:        readVoltageV(base),
				Present:         readBoolField(filepath.Join(base, "present")),
			}
			batteries = append(batteries, batt)
		case "mains", "usb", "ac":
			if online := readBoolField(filepath.Join(base, "online")); online != nil {
				acOnline = online
			}
		}
	}
	return batteries, acOnline
}

func batteryHealthPercent(base string) *float64 {
	full := readEnergyWh(base, "energy_full", "charge_full")
	design := readEnergyWh(base, "energy_full_design", "charge_full_design")
	if full == nil || design == nil || *design == 0 {
		return nil
	}
	value := (*full / *design) * 100
	return &value
}

func readEnergyWh(base string, names ...string) *float64 {
	for _, name := range names {
		path := filepath.Join(base, name)
		raw := readString(path)
		if raw == "" {
			continue
		}
		// parse as float to handle large values reliably
		v, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
		if err != nil {
			continue
		}
		// Common kernel semantics:
		// - energy_* fields are in micro-watt-hours (uWh)
		// - charge_* fields are in micro-ampere-hours (uAh)
		// If we find an energy field, convert uWh -> Wh by dividing by 1e6.
		// If we find a charge field, convert uAh -> Wh using the battery voltage
		// (voltage is provided in microvolts in sysfs so readVoltageV already
		// returns volts).
		lower := strings.ToLower(name)
		if strings.Contains(lower, "energy") {
			wh := v / 1e6
			return &wh
		}
		if strings.Contains(lower, "charge") {
			// need voltage to convert charge (uAh) to Wh: Wh = (uAh / 1e6) * V
			if volts := readVoltageV(base); volts != nil && *volts > 0 {
				wh := (v / 1e6) * (*volts)
				return &wh
			}
			// If voltage is unavailable, fall back to returning nil so callers
			// know the value is unreliable.
			return nil
		}
		// Unknown field name: treat as micro-watt-hours by default
		wh := v / 1e6
		return &wh
	}
	return nil
}

func readPowerNowW(base string) *float64 {
	if value := readMicroPower(filepath.Join(base, "power_now")); value != nil {
		return value
	}
	if value := readMicroPower(filepath.Join(base, "current_now")); value != nil {
		voltage := readVoltageV(base)
		if voltage != nil {
			watts := *value * *voltage
			return &watts
		}
		return value
	}
	return nil
}

func readVoltageV(base string) *float64 {
	for _, name := range []string{"voltage_now", "voltage_min_design", "voltage_max_design"} {
		raw := readString(filepath.Join(base, name))
		if raw == "" {
			continue
		}
		value, err := strconv.ParseFloat(raw, 64)
		if err != nil || value <= 0 {
			continue
		}
		volts := value / 1e6
		return &volts
	}
	return nil
}

func readFloatField(path string) *float64 {
	raw := readString(path)
	if raw == "" {
		return nil
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return nil
	}
	return &value
}

func readBoolField(path string) *bool {
	raw := readString(path)
	if raw == "" {
		return nil
	}
	switch strings.ToLower(raw) {
	case "1", "y", "yes", "true", "on":
		v := true
		return &v
	case "0", "n", "no", "false", "off":
		v := false
		return &v
	default:
		return nil
	}
}

func (c *Collector) readAvailableGovernors() []string {
	return dedupeSortedFieldsFromGlobs(
		filepath.Join(c.sys, "devices/system/cpu/cpu*/cpufreq/scaling_available_governors"),
		filepath.Join(c.sys, "devices/system/cpu/cpufreq/policy*/scaling_available_governors"),
	)
}

func (c *Collector) readEPPChoices() []string {
	return orderEPPChoices(dedupeSortedFieldsFromGlobs(
		filepath.Join(c.sys, "devices/system/cpu/cpu*/cpufreq/energy_performance_available_preferences"),
		filepath.Join(c.sys, "devices/system/cpu/cpufreq/policy*/energy_performance_available_preferences"),
	))
}

func orderEPPChoices(choices []string) []string {
	preferred := []string{"performance", "balance_performance", "balance_power", "power", "default"}
	available := make(map[string]bool, len(choices))
	for _, choice := range choices {
		available[choice] = true
	}
	ordered := make([]string, 0, len(choices))
	for _, choice := range preferred {
		if available[choice] {
			ordered = append(ordered, choice)
			delete(available, choice)
		}
	}
	remaining := make([]string, 0, len(available))
	for choice := range available {
		remaining = append(remaining, choice)
	}
	sort.Strings(remaining)
	return append(ordered, remaining...)
}

func (c *Collector) readPowerProfile() string {
	path := filepath.Join(c.sys, "firmware/acpi/platform_profile")
	return normalizePowerProfile(readString(path))
}

func (c *Collector) readCPUDriver() string {
	for _, path := range []string{
		filepath.Join(c.sys, "devices/system/cpu/intel_pstate/status"),
		filepath.Join(c.sys, "devices/system/cpu/amd_pstate/status"),
	} {
		if readString(path) != "" {
			switch {
			case strings.Contains(path, "intel_pstate"):
				return "intel_pstate"
			case strings.Contains(path, "amd_pstate"):
				return "amd_pstate"
			}
		}
	}
	for _, path := range []string{
		filepath.Join(c.sys, "devices/system/cpu/cpu0/cpufreq/scaling_driver"),
		filepath.Join(c.sys, "devices/system/cpu/cpufreq/policy0/scaling_driver"),
	} {
		if raw := readString(path); raw != "" {
			return raw
		}
	}
	if _, err := os.Stat(filepath.Join(c.sys, "devices/system/cpu/cpufreq")); err == nil {
		return "cpufreq"
	}
	return ""
}

func (c *Collector) readPowerProfileChoices() []string {
	path := filepath.Join(c.sys, "firmware/acpi/platform_profile_choices")
	return strings.Fields(readString(path))
}

func (c *Collector) readTurboEnabled() *bool {
	candidates := []string{
		filepath.Join(c.sys, "devices/system/cpu/intel_pstate/no_turbo"),
		filepath.Join(c.sys, "devices/system/cpu/amd_pstate/no_turbo"),
		filepath.Join(c.sys, "devices/system/cpu/cpufreq/boost"),
	}
	for _, path := range candidates {
		raw := readString(path)
		if raw == "" {
			continue
		}
		enabled := raw != "1"
		if strings.HasSuffix(path, "boost") {
			enabled = raw == "1"
		}
		return &enabled
	}
	return nil
}

func (c *Collector) capabilities() []model.Capability {
	return []model.Capability{
		capability("CPU governor", "standard cpufreq governor switching", "policy", countExistingPaths(
			filepath.Join(c.sys, "devices/system/cpu/cpu*/cpufreq/scaling_governor"),
			filepath.Join(c.sys, "devices/system/cpu/cpufreq/policy*/scaling_governor"),
		), filepath.Join(c.sys, "devices/system/cpu/cpu0/cpufreq/scaling_governor")),
		capability("CPU EPP", "energy performance preference", "policy", countExistingPaths(
			filepath.Join(c.sys, "devices/system/cpu/cpu*/cpufreq/energy_performance_preference"),
			filepath.Join(c.sys, "devices/system/cpu/cpufreq/policy*/energy_performance_preference"),
		), filepath.Join(c.sys, "devices/system/cpu/cpu0/cpufreq/energy_performance_preference")),
		capability("CPU core online", "per-core online/offline control", "core", countExistingPaths(
			filepath.Join(c.sys, "devices/system/cpu/cpu*/online"),
		), filepath.Join(c.sys, "devices/system/cpu/cpu1/online")),
		capability("CPU turbo", "boost/turbo switch exposed by driver", "system", 1, firstExistingPath(
			filepath.Join(c.sys, "devices/system/cpu/intel_pstate/no_turbo"),
			filepath.Join(c.sys, "devices/system/cpu/amd_pstate/no_turbo"),
			filepath.Join(c.sys, "devices/system/cpu/cpufreq/boost"),
		)),
		capability("Power profile", "ACPI platform profile selection", "system", len(c.readPowerProfileChoices()), filepath.Join(c.sys, "firmware/acpi/platform_profile")),
		capability("Thermal sensors", "hwmon and thermal zone telemetry", "sensor", countExistingPaths(
			filepath.Join(c.sys, "class/hwmon/hwmon*/temp*_input"),
			filepath.Join(c.sys, "class/thermal/thermal_zone*/temp"),
		), filepath.Join(c.sys, "class/hwmon")),
		capability("Package power", "RAPL powercap telemetry", "package", countExistingPaths(
			filepath.Join(c.sys, "class/powercap/intel-rapl:*"),
		), filepath.Join(c.sys, "class/powercap")),
		capability("Battery", "battery and AC power-supply telemetry", "supply", countExistingPaths(
			filepath.Join(c.sys, "class/power_supply/*"),
		), filepath.Join(c.sys, "class/power_supply")),
		capability("GPU DRM", "kernel DRM device discovery", "device", len(gpu.Read(c.sys)), filepath.Join(c.sys, "class/drm")),
	}
}

func capability(name, description, scope string, targets int, path string, extraPaths ...string) model.Capability {
	status := model.CapabilityUnavailable
	for _, candidate := range append([]string{path}, extraPaths...) {
		if candidate == "" {
			continue
		}
		if _, err := os.Stat(candidate); err == nil {
			status = model.CapabilityAvailable
			path = candidate
			break
		}
	}
	paths := append([]string{path}, extraPaths...)
	return model.Capability{Name: name, Status: status, Description: description, Path: path, Scope: scope, Targets: targets, Paths: paths}
}

func countExistingPaths(patterns ...string) int {
	total := 0
	for _, pattern := range patterns {
		matches, _ := filepath.Glob(pattern)
		total += len(matches)
	}
	return total
}

func readKHzAsMHz(path string) int {
	raw := readString(path)
	if raw == "" {
		return 0
	}
	khz, err := strconv.Atoi(raw)
	if err != nil {
		return 0
	}
	return khz / 1000
}

func readString(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func readInt(path string, fallback int) int {
	raw := readString(path)
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return value
}

func firstExistingPath(paths ...string) string {
	for _, path := range paths {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ""
}

func writeString(path string, value string) error {
	return os.WriteFile(path, []byte(value), 0o644)
}

func firstNonEmptyGovernor(cores []model.CPUCore) string {
	for _, core := range cores {
		if core.Governor != "" {
			return core.Governor
		}
	}
	return ""
}

func firstNonEmptyEPP(cores []model.CPUCore) string {
	for _, core := range cores {
		if core.EPP != "" {
			return core.EPP
		}
	}
	return ""
}

func kernelRelease() string {
	var uname syscall.Utsname
	if err := syscall.Uname(&uname); err != nil {
		return ""
	}
	return int8SliceToString(uname.Release[:])
}

func machineArch() string {
	var uname syscall.Utsname
	if err := syscall.Uname(&uname); err != nil {
		return ""
	}
	return int8SliceToString(uname.Machine[:])
}

func int8SliceToString(in []int8) string {
	var b strings.Builder
	for _, v := range in {
		if v == 0 {
			break
		}
		b.WriteByte(byte(v))
	}
	return b.String()
}
