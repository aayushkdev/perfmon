package model

import "time"

type Snapshot struct {
	Timestamp    time.Time
	Host         Host
	CPU          CPU
	Thermals     []ThermalSensor
	Power        []PowerDomain
	Batteries    []Battery
	ACOnline     *bool
	GPUs         []GPU
	Processes    []Process
	Memory       Memory
	Capabilities []Capability
	Warnings     []string
}

type Host struct {
	Kernel       string
	Architecture string
}

type CPU struct {
	Vendor       string
	Model        string
	Architecture string
	Driver       string
	Topology     CPUTopology
	Hybrid       bool
	HybridKnown  bool
	UsagePercent float64
	PowerProfile string
	Cores        []CPUCore
	TemperatureC *float64
	PowerW       *float64
	Governors    []string
	EPPChoices   []string
	ActiveGov    string
	EPP          string
	TurboEnabled *bool
}

type CPUTopology struct {
	Known          bool
	Packages       int
	NUMANodes      int
	PhysicalCores  int
	LogicalCores   int
	ThreadsPerCore float64
}

type CPUCore struct {
	ID              int
	Online          bool
	Type            CoreType
	PackageID       int
	CoreID          int
	DieID           int
	NodeID          int
	Capacity        int
	TopologyType    string
	ThreadSiblings  []int
	CoreSiblings    []int
	UsagePercent    float64
	FrequencyMHz    int
	MinFrequencyMHz int
	MaxFrequencyMHz int
	Governor        string
	EPP             string
}

type CoreType string

const (
	CoreUnknown     CoreType = "unknown"
	CorePerformance CoreType = "performance"
	CoreEfficiency  CoreType = "efficiency"
)

type GPU struct {
	ID           string
	Vendor       string
	Name         string
	Driver       string
	PCIID        string
	Outputs      []string
	UtilPercent  *float64
	ClockMHz     *int
	TemperatureC *float64
	PowerW       *float64
}

// Process describes a single process snapshot used by the optional process
// lister UI. Fields are intentionally minimal to keep the model small.
type Process struct {
	PID        int
	Name       string
	Cmdline    string
	CPUPercent float64
	MemPercent float64
	RSSBytes   uint64
}

type ThermalSensor struct {
	Name         string
	Source       string
	TemperatureC *float64
	MaxC         *float64
	CriticalC    *float64
	Throttled    *bool
}

type PowerDomain struct {
	Name      string
	Source    string
	PowerW    *float64
	EnergyJ   *float64
	LimitW    *float64
	MaxW      *float64
	CriticalW *float64
}

type Battery struct {
	Name            string
	Status          string
	CapacityPercent *float64
	HealthPercent   *float64
	CycleCount      *float64
	EnergyNowWh     *float64
	EnergyFullWh    *float64
	PowerW          *float64
	VoltageV        *float64
	Present         *bool
}

type Memory struct {
	TotalBytes     uint64
	UsedBytes      uint64
	AvailableBytes uint64
	SwapTotalBytes uint64
	SwapUsedBytes  uint64
	// ModuleCount is the number of detected memory modules (DIMMs). May be nil
	// if the collector could not determine the value on this system.
	ModuleCount *int
	// SpeedMHz is the common memory speed in MHz when detectable. May be
	// nil if unknown.
	SpeedMHz *int
}
