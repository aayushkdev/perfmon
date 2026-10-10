package backend

import (
	"context"

	"github.com/aayushkdev/perfmon/internal/backend/cpu/controls"
)

func (c *Collector) cpuControls() *controls.Controller { return &controls.Controller{Sys: c.sys} }

func (c *Collector) SetCoreOnline(ctx context.Context, id int, online bool) error {
	return c.cpuControls().SetCoreOnline(ctx, id, online)
}

func (c *Collector) CanSetCoreOnline(id int) bool {
	return c.cpuControls().CanSetCoreOnline(id)
}

func (c *Collector) SetCPUGovernor(ctx context.Context, governor string) error {
	return c.cpuControls().SetCPUGovernor(ctx, governor)
}

func (c *Collector) SetEPP(ctx context.Context, preference string) error {
	return c.cpuControls().SetEPP(ctx, preference)
}

func (c *Collector) SetTurboEnabled(ctx context.Context, enabled bool) error {
	return c.cpuControls().SetTurboEnabled(ctx, enabled)
}

func (c *Collector) SetPowerProfile(ctx context.Context, profile string) error {
	return c.cpuControls().SetPowerProfile(ctx, profile)
}
