package backend

import (
	"github.com/aayushkdev/perfmon/internal/backend/power"
	"github.com/aayushkdev/perfmon/internal/model"
)

func (c *Collector) readPowerDomains() []model.PowerDomain {
	return power.Read(c.sys, c.raplPrev)
}
