package backend

import (
	"github.com/aayushkdev/perfmon/internal/backend/battery"
	"github.com/aayushkdev/perfmon/internal/model"
)

func (c *Collector) readBatteries() ([]model.Battery, *bool) {
	return battery.Read(c.sys)
}
