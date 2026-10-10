package backend

import (
	"github.com/aayushkdev/perfmon/internal/backend/memory"
	"github.com/aayushkdev/perfmon/internal/model"
)

func (c *Collector) readMemory() model.Memory {
	return memory.Read(c.proc, c.sys)
}
