package model

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
