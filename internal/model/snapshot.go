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
