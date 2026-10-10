package tui

import (
	"context"
	"fmt"
	"github.com/aayushkdev/perfmon/internal/model"
	"strings"
)

func (a *App) toggleSelectedCore(ctx context.Context) {
	if a.controls == nil {
		a.flashFooter("[yellow]Core online control is not supported by this collector.[-]")
		return
	}
	row, _ := a.coreTable.GetSelection()
	if row <= 0 || row-1 >= len(a.last.CPU.Cores) {
		a.flashFooter("[yellow]Select a core row first.[-]")
		return
	}
	core := a.last.CPU.Cores[row-1]
	targetOnline := !core.Online
	action := "online"
	if !targetOnline {
		action = "offline"
	}
	if !a.controls.CanSetCoreOnline(core.ID) {
		a.flashFooter(fmt.Sprintf("[yellow]Core %d cannot be toggled on this system.[-]", core.ID))
		return
	}
	go a.applyControl(ctx, fmt.Sprintf("core %d %s", core.ID, action), func(runCtx context.Context) error {
		return a.controls.SetCoreOnline(runCtx, core.ID, targetOnline)
	})
}

func (a *App) toggleTurbo(ctx context.Context) {
	if a.controls == nil {
		a.flashFooter("[yellow]Turbo control is not supported by this collector.[-]")
		return
	}
	enabled := a.last.CPU.TurboEnabled != nil && *a.last.CPU.TurboEnabled
	target := !enabled
	go a.applyControl(ctx, fmt.Sprintf("turbo %s", onOff(target)), func(runCtx context.Context) error {
		return a.controls.SetTurboEnabled(runCtx, target)
	})
}

func (a *App) chooseGovernor(ctx context.Context) {
	if a.controls == nil {
		a.flashFooter("[yellow]Governor control is not supported by this collector.[-]")
		return
	}
	if len(a.last.CPU.Governors) == 0 {
		a.flashFooter("[yellow]No governors detected on this system.[-]")
		return
	}
	choice := nextValue(a.last.CPU.ActiveGov, a.last.CPU.Governors)
	go a.applyControl(ctx, fmt.Sprintf("governor %s", choice), func(runCtx context.Context) error {
		return a.controls.SetCPUGovernor(runCtx, choice)
	})
}

func (a *App) chooseEPP(ctx context.Context) {
	if a.controls == nil {
		a.flashFooter("[yellow]EPP control is not supported by this collector.[-]")
		return
	}
	options := a.last.CPU.EPPChoices
	if len(options) == 0 {
		a.flashFooter("[yellow]No EPP preferences are exposed by the CPU driver.[-]")
		return
	}
	choice := nextValue(a.last.CPU.EPP, options)
	go a.applyControl(ctx, fmt.Sprintf("EPP %s", choice), func(runCtx context.Context) error {
		return a.controls.SetEPP(runCtx, choice)
	})
}

func (a *App) choosePowerProfile(ctx context.Context) {
	if a.controls == nil {
		a.flashFooter("[yellow]Power profile control is not supported by this collector.[-]")
		return
	}
	choice := nextValue(a.last.CPU.PowerProfile, []string{"powersave", "balanced", "performance"})
	go a.applyControl(ctx, fmt.Sprintf("power mode %s", choice), func(runCtx context.Context) error {
		return a.controls.SetPowerProfile(runCtx, choice)
	})
}

func (a *App) applyControl(ctx context.Context, label string, fn func(context.Context) error) {
	err := fn(ctx)
	if err != nil {
		a.app.QueueUpdateDraw(func() {
			a.flashFooter(fmt.Sprintf("[red]Failed to set %s: %s[-]", label, compactControlError(err)))
		})
		return
	}
	a.app.QueueUpdateDraw(func() {
		a.flashFooter(fmt.Sprintf("[green]Set %s.[-]", label))
	})
	a.refresh(ctx)
}

func compactControlError(err error) string {
	message := strings.Join(strings.Fields(err.Error()), " ")
	const maxLength = 112
	if len(message) > maxLength {
		return message[:maxLength-3] + "..."
	}
	return message
}

func nextValue(current string, options []string) string {
	if len(options) == 0 {
		return current
	}
	for i, option := range options {
		if option == current {
			return options[(i+1)%len(options)]
		}
	}
	return options[0]
}

func onOff(enabled bool) string {
	if enabled {
		return "on"
	}
	return "off"
}

func coreTypeLabel(core model.CPUCore) string {
	switch core.Type {
	case model.CorePerformance:
		return "P-core"
	case model.CoreEfficiency:
		return "E-core"
	default:
		return ""
	}
}
