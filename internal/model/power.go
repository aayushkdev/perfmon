package model

type PowerDomain struct {
	Name      string
	Source    string
	PowerW    *float64
	EnergyJ   *float64
	LimitW    *float64
	MaxW      *float64
	CriticalW *float64
}
