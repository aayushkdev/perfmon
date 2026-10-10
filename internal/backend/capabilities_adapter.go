package backend

import (
	"github.com/aayushkdev/perfmon/internal/backend/capabilities"
	"github.com/aayushkdev/perfmon/internal/model"
)

func (c *Collector) capabilities() []model.Capability {
	return capabilities.Read(c.sys)
}
