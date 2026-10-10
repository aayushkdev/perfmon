package backend

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

func (c *Collector) SetCoreOnline(ctx context.Context, id int, online bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	if id == 0 && !online {
		return errors.New("cpu0 cannot be offlined safely")
	}
	path := c.coreOnlinePath(id)
	if !c.CanSetCoreOnline(id) {
		return fmt.Errorf("core %d online control is unavailable", id)
	}
	value := "0"
	if online {
		value = "1"
	}
	return os.WriteFile(path, []byte(value), 0o644)
}

func (c *Collector) CanSetCoreOnline(id int) bool {
	if id == 0 {
		return false
	}
	info, err := os.Stat(c.coreOnlinePath(id))
	return err == nil && !info.IsDir()
}

func (c *Collector) coreOnlinePath(id int) string {
	return filepath.Join(c.sys, "devices/system/cpu", fmt.Sprintf("cpu%d", id), "online")
}

func (c *Collector) SetCPUGovernor(ctx context.Context, governor string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.writeToCPUFiles(ctx, "scaling_governor", governor); err != nil {
		return err
	}
	return nil
}

func (c *Collector) SetEPP(ctx context.Context, preference string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if eppRequiresPowersaveGovernor(c.readCPUDriver()) && preference != "performance" {
		if err := c.writeToCPUFiles(ctx, "scaling_governor", "powersave"); err != nil {
			return fmt.Errorf("EPP %q requires the powersave governor: %w", preference, err)
		}
	}
	if err := c.writeToCPUFiles(ctx, "energy_performance_preference", preference); err != nil {
		return err
	}
	return nil
}

func eppRequiresPowersaveGovernor(driver string) bool {
	switch driver {
	case "intel_pstate", "amd-pstate", "amd-pstate-epp":
		return true
	default:
		return false
	}
}

func (c *Collector) SetTurboEnabled(ctx context.Context, enabled bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	for _, path := range []string{
		filepath.Join(c.sys, "devices/system/cpu/intel_pstate/no_turbo"),
		filepath.Join(c.sys, "devices/system/cpu/amd_pstate/no_turbo"),
		filepath.Join(c.sys, "devices/system/cpu/cpufreq/boost"),
	} {
		if _, err := os.Stat(path); err != nil {
			continue
		}
		value := "0"
		if !enabled {
			value = "1"
		}
		if strings.HasSuffix(path, "boost") {
			value = "0"
			if enabled {
				value = "1"
			}
		}
		if err := writeString(path, value); err == nil {
			return nil
		}
	}
	return errors.New("turbo control is unavailable")
}

func (c *Collector) SetPowerProfile(ctx context.Context, profile string) error {
	if path, value, ok := c.platformProfileWriteTarget(profile); ok {
		return writeString(path, value)
	}
	switch normalizePowerProfile(profile) {
	case "performance":
		if err := c.SetCPUGovernor(ctx, pickSupported("performance", c.readAvailableGovernors(), "schedutil", "ondemand")); err != nil {
			return err
		}
		if err := c.SetEPP(ctx, pickSupported("performance", c.readEPPChoices(), "balance_performance", "balance_power")); err != nil {
			return err
		}
		return c.SetTurboEnabled(ctx, true)
	case "balanced":
		if err := c.SetCPUGovernor(ctx, pickSupported("schedutil", c.readAvailableGovernors(), "ondemand", "powersave", "performance")); err != nil {
			return err
		}
		if err := c.SetEPP(ctx, pickSupported("balance_performance", c.readEPPChoices(), "balance_power", "performance")); err != nil {
			return err
		}
		return nil
	case "powersave":
		if err := c.SetCPUGovernor(ctx, pickSupported("powersave", c.readAvailableGovernors(), "schedutil", "ondemand")); err != nil {
			return err
		}
		if err := c.SetEPP(ctx, pickSupported("power", c.readEPPChoices(), "balance_power", "balance_performance")); err != nil {
			return err
		}
		return c.SetTurboEnabled(ctx, false)
	default:
		return fmt.Errorf("unsupported power profile %q", profile)
	}
}

func (c *Collector) platformProfileWriteTarget(profile string) (string, string, bool) {
	path := filepath.Join(c.sys, "firmware/acpi/platform_profile")
	choices := c.readPowerProfileChoices()
	switch normalizePowerProfile(profile) {
	case "powersave":
		for _, choice := range choices {
			if normalizePowerProfile(choice) == "powersave" {
				return path, choice, true
			}
		}
	case "balanced":
		for _, choice := range choices {
			if normalizePowerProfile(choice) == "balanced" {
				return path, choice, true
			}
		}
	case "performance":
		for _, choice := range choices {
			if normalizePowerProfile(choice) == "performance" {
				return path, choice, true
			}
		}
	}
	return "", "", false
}

func (c *Collector) writeToCPUFiles(ctx context.Context, suffix, value string) error {
	files := make([]string, 0, 8)
	if matches, err := filepath.Glob(filepath.Join(c.sys, "devices/system/cpu/cpu*/cpufreq", suffix)); err == nil {
		files = append(files, matches...)
	}
	if matches, err := filepath.Glob(filepath.Join(c.sys, "devices/system/cpu/cpufreq/policy*/", suffix)); err == nil {
		files = append(files, matches...)
	}
	if len(files) == 0 {
		return fmt.Errorf("%s control is unavailable", suffix)
	}
	written := false
	var lastErr error
	failed := false
	for _, path := range uniqueParentFiles(files) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if err := writeString(path, value); err == nil {
			written = true
		} else {
			failed = true
			lastErr = fmt.Errorf("%s/%s: %w", filepath.Base(filepath.Dir(path)), filepath.Base(path), err)
		}
	}
	if failed {
		return fmt.Errorf("failed to apply %s=%q consistently: %w", suffix, value, lastErr)
	}
	if !written {
		if lastErr != nil {
			return fmt.Errorf("failed to write %s=%q: %w", suffix, value, lastErr)
		}
		return fmt.Errorf("failed to write %s=%q", suffix, value)
	}
	return nil
}

func uniqueParentFiles(paths []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		dir := filepath.Dir(path)
		if seen[dir] {
			continue
		}
		seen[dir] = true
		out = append(out, path)
	}
	return out
}

func normalizePowerProfile(profile string) string {
	switch strings.ToLower(strings.TrimSpace(profile)) {
	case "low-power", "powersave":
		return "powersave"
	case "balanced", "balance":
		return "balanced"
	case "performance", "perf":
		return "performance"
	default:
		return strings.ToLower(strings.TrimSpace(profile))
	}
}

func pickSupported(preferred string, supported []string, fallbacks ...string) string {
	for _, item := range supported {
		if item == preferred {
			return preferred
		}
	}
	for _, fallback := range fallbacks {
		for _, item := range supported {
			if item == fallback {
				return item
			}
		}
	}
	if len(supported) > 0 {
		return supported[0]
	}
	return preferred
}

func parseCPUList(value string) []int {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	ids := make([]int, 0)
	for _, part := range strings.Split(value, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if start, end, ok := strings.Cut(part, "-"); ok {
			s, err1 := strconv.Atoi(strings.TrimSpace(start))
			e, err2 := strconv.Atoi(strings.TrimSpace(end))
			if err1 != nil || err2 != nil || e < s {
				continue
			}
			for i := s; i <= e; i++ {
				ids = append(ids, i)
			}
			continue
		}
		id, err := strconv.Atoi(part)
		if err == nil {
			ids = append(ids, id)
		}
	}
	return ids
}

func firstFieldsFromGlobs(patterns ...string) []string {
	for _, pattern := range patterns {
		matches, _ := filepath.Glob(pattern)
		for _, path := range matches {
			fields := strings.Fields(readString(path))
			if len(fields) > 0 {
				return fields
			}
		}
	}
	return nil
}

func dedupeSortedFieldsFromGlobs(patterns ...string) []string {
	seen := map[string]struct{}{}
	for _, pattern := range patterns {
		matches, _ := filepath.Glob(pattern)
		for _, path := range matches {
			for _, field := range strings.Fields(readString(path)) {
				if field == "" {
					continue
				}
				seen[field] = struct{}{}
			}
		}
	}
	if len(seen) == 0 {
		return nil
	}
	out := make([]string, 0, len(seen))
	for field := range seen {
		out = append(out, field)
	}
	sort.Strings(out)
	return out
}
