package backend

import "github.com/aayushkdev/perfmon/internal/backend/process"

func (c *Collector) updateProcesses() {
	procs, prev, total := process.Scan(c.proc, c.prevProcJiffies, c.prevTotalJiffies)
	c.processes = procs
	c.prevProcJiffies = prev
	c.prevTotalJiffies = total
}
