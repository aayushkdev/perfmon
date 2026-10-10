package backend

import (
	"github.com/aayushkdev/perfmon/internal/backend/thermal"
	"github.com/aayushkdev/perfmon/internal/model"
)

func (c *Collector) readThermals() []model.ThermalSensor {
	return thermal.Read(c.sys)
}
