package tui

import (
	"fmt"
	"github.com/aayushkdev/perfmon/internal/model"
	"strings"
	"time"
)

func (a *App) renderFooter(s model.Snapshot, message string) {
	if message == "" {
		message = footerHint
	}
	if a.activeMiddle == "procs" && (a.procSearch || a.procQuery != "") {
		query := a.procQuery
		if query == "" {
			query = "_"
		}
		searchHint := fmt.Sprintf("[aqua]Search[-] %q", query)
		if a.procSearch {
			searchHint += "  [silver]Enter next  Esc clear[-]"
		} else {
			searchHint += "  [silver]/ edit  Esc clear[-]"
		}
		if message == footerHint {
			message = searchHint
		} else {
			message += "  " + searchHint
		}
	}
	a.status.SetText(message + "\n" + a.controlLegend(s))
}

// flashFooter shows a transient status message above the legend for two
// seconds before render reverts to the default hint.
func (a *App) flashFooter(message string) {
	a.flashMsg = message
	a.flashUntil = time.Now().Add(2 * time.Second)
	a.renderFooter(a.last, message)
}

func (a *App) controlLegend(s model.Snapshot) string {
	parts := make([]string, 0, 8)
	key := func(k, label string) string { return "[aqua]" + k + "[-] " + label }
	if a.controls != nil {
		if capabilityEnabled(s, "CPU governor") {
			parts = append(parts, key("g", "governor"))
		}
		if capabilityEnabled(s, "CPU EPP") {
			parts = append(parts, key("e", "epp"))
		}
		if capabilityEnabled(s, "CPU turbo") {
			parts = append(parts, key("t", "turbo"))
		}
		parts = append(parts, key("m", "mode"))
	}
	// Mode-specific controls: only advertise what the active view responds to.
	if a.activeMiddle == "procs" {
		parts = append(parts, key("/", "search"), key("c", "sort"), key("s", "dir"), key("⏎", "signal"), key("k", "term"), key("K", "kill"))
		parts = append(parts, key("p", "cores"))
	} else {
		if a.controls != nil {
			parts = append(parts, key("o", "core"))
		}
		parts = append(parts, key("p", "processes"))
	}
	parts = append(parts, key("r", "refresh"), key("q", "quit"))
	return "[silver]" + strings.Join(parts, "  ") + "[-]"
}

func renderControlsBox(s model.Snapshot, supported bool) string {
	governorCap := capabilityByName(s, "CPU governor")
	eppCap := capabilityByName(s, "CPU EPP")
	turboCap := capabilityByName(s, "CPU turbo")
	coreCap := capabilityByName(s, "CPU core online")
	modeCap := powerModeCapability(s)
	if !supported {
		lines := []string{
			"[white::b]Controls[-:-:-]",
			"",
			"[silver]mode   [-] " + fallback(s.CPU.PowerProfile, "unavailable"),
			"[silver]governor[-] " + controlValue(fallback(s.CPU.ActiveGov, "unavailable"), governorCap),
			"[silver]EPP    [-] " + controlValue(fallback(s.CPU.EPP, "unavailable"), eppCap),
			"[silver]turbo  [-] " + controlValue(boolPtr(s.CPU.TurboEnabled), turboCap),
			"[silver]core   [-] " + controlValue(coreControlSummary(s), coreCap),
			"",
			"[silver]No writable control backend is available.[-]",
		}
		return strings.Join(lines, "\n")
	}
	lines := []string{
		"[white::b]Controls[-:-:-]",
		"",
		"[silver]mode   [-] " + fallback(s.CPU.PowerProfile, "unavailable"),
		"[silver]governor[-] " + controlValue(fallback(s.CPU.ActiveGov, "unavailable"), governorCap),
		"[silver]EPP    [-] " + controlValue(fallback(s.CPU.EPP, "unavailable"), eppCap),
		"[silver]turbo  [-] " + controlValue(boolPtr(s.CPU.TurboEnabled), turboCap),
		"[silver]core   [-] " + controlValue(coreControlSummary(s), coreCap),
		"",
		"[silver]g[-] cycle governor" + capabilityState(governorCap),
		"[silver]e[-] cycle EPP" + capabilityState(eppCap),
		"[silver]t[-] toggle turbo" + capabilityState(turboCap),
		"[silver]m[-] cycle power mode" + capabilityState(modeCap),
		"[silver]o[-] toggle selected core" + capabilityState(coreCap),
		"",
		"[silver]Topology and capability scope come from kernel topology and sysfs target discovery.[-]",
	}
	return strings.Join(lines, "\n")
}

func capabilityByName(s model.Snapshot, name string) *model.Capability {
	for i := range s.Capabilities {
		if s.Capabilities[i].Name == name {
			return &s.Capabilities[i]
		}
	}
	return nil
}

func capabilityEnabled(s model.Snapshot, name string) bool {
	cap := capabilityByName(s, name)
	return cap != nil && cap.Status != model.CapabilityUnavailable
}

func powerModeCapability(s model.Snapshot) *model.Capability {
	if cap := capabilityByName(s, "Power profile"); cap != nil {
		return cap
	}
	if capabilityEnabled(s, "CPU governor") || capabilityEnabled(s, "CPU EPP") || capabilityEnabled(s, "CPU turbo") {
		return &model.Capability{
			Name:    "Power profile",
			Status:  model.CapabilityConditional,
			Scope:   "system",
			Targets: 1,
		}
	}
	return nil
}

func capabilityState(cap *model.Capability) string {
	if cap == nil {
		return "  [yellow]unavailable[-]"
	}
	label := statusLabel(cap.Status)
	if cap.Targets > 0 {
		return fmt.Sprintf("  %s [silver](%d target%s)[-]", label, cap.Targets, plural(cap.Targets))
	}
	return "  " + label
}

func statusLabel(status model.CapabilityStatus) string {
	switch status {
	case model.CapabilityAvailable:
		return "[green]available[-]"
	case model.CapabilityConditional:
		return "[yellow]conditional[-]"
	default:
		return "[yellow]unavailable[-]"
	}
}

func controlValue(value string, cap *model.Capability) string {
	if cap == nil {
		return value
	}
	switch cap.Status {
	case model.CapabilityAvailable:
		return value
	case model.CapabilityConditional:
		return "[yellow]" + value + "[-]"
	default:
		return "[silver]" + value + "[-]"
	}
}

func coreControlSummary(s model.Snapshot) string {
	cap := capabilityByName(s, "CPU core online")
	if cap == nil {
		return "unavailable"
	}
	return fmt.Sprintf("%s / %d online", statusLabel(cap.Status), onlineCores(s.CPU.Cores))
}

func topologySummary(top model.CPUTopology) string {
	if !top.Known {
		return "unavailable"
	}
	parts := make([]string, 0, 5)
	if top.Packages > 0 {
		parts = append(parts, fmt.Sprintf("%d pkg", top.Packages))
	}
	if top.NUMANodes > 0 {
		parts = append(parts, fmt.Sprintf("%d numa", top.NUMANodes))
	}
	if top.PhysicalCores > 0 {
		parts = append(parts, fmt.Sprintf("%d phys", top.PhysicalCores))
	}
	if top.LogicalCores > 0 {
		parts = append(parts, fmt.Sprintf("%d log", top.LogicalCores))
	}
	if top.ThreadsPerCore > 0 {
		parts = append(parts, fmt.Sprintf("%.1f tpc", top.ThreadsPerCore))
	}
	if len(parts) == 0 {
		return "known"
	}
	return strings.Join(parts, " / ")
}

func topologyLabel(prefix string, id int) string {
	if id < 0 {
		return "-"
	}
	return fmt.Sprintf("%s%d", prefix, id)
}

func plural(count int) string {
	if count == 1 {
		return ""
	}
	return "s"
}
