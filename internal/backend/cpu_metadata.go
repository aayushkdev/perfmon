package backend

import "github.com/aayushkdev/perfmon/internal/backend/cpu"

func (c *Collector) cpuMetadata() cpu.MetadataReader   { return cpu.MetadataReader{Sys: c.sys} }
func (c *Collector) readAvailableGovernors() []string  { return c.cpuMetadata().AvailableGovernors() }
func (c *Collector) readEPPChoices() []string          { return c.cpuMetadata().EPPChoices() }
func (c *Collector) readPowerProfile() string          { return c.cpuMetadata().PowerProfile() }
func (c *Collector) readCPUDriver() string             { return c.cpuMetadata().Driver() }
func (c *Collector) readPowerProfileChoices() []string { return c.cpuMetadata().PowerProfileChoices() }
func (c *Collector) readTurboEnabled() *bool           { return c.cpuMetadata().TurboEnabled() }
