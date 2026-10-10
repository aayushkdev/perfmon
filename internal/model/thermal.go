package model

type ThermalSensor struct {
	Name         string
	Source       string
	TemperatureC *float64
	MaxC         *float64
	CriticalC    *float64
	Throttled    *bool
}
