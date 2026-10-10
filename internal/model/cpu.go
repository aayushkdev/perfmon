package model

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
