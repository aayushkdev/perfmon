package model

type CapabilityStatus string

const (
	CapabilityAvailable   CapabilityStatus = "available"
	CapabilityUnavailable CapabilityStatus = "unavailable"
	CapabilityConditional CapabilityStatus = "conditional"
)

type Capability struct {
	Name        string
	Status      CapabilityStatus
	Description string
	Path        string
	Scope       string
	Targets     int
	Paths       []string
}
