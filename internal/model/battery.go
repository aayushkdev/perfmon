package model

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
