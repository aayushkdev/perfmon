package vendors

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/aayushkdev/perfmon/internal/model"
)

// Enrich attempts to read common AMD GPU sysfs attributes to fill metrics.
func EnrichAMD(sys, device string, g *model.GPU) error {
	// gpu_busy_percent is sometimes provided by drivers
	if s, err := os.ReadFile(filepath.Join(device, "gpu_busy_percent")); err == nil {
		if v, err := strconv.ParseFloat(strings.TrimSpace(string(s)), 64); err == nil {
			g.UtilPercent = &v
		}
	}
	// power: try a couple of plausible paths
	if s, err := os.ReadFile(filepath.Join(device, "power", "average")); err == nil {
		if v, err := strconv.ParseFloat(strings.TrimSpace(string(s)), 64); err == nil {
			w := v / 1e6
			g.PowerW = &w
		}
	}
	if s, err := os.ReadFile(filepath.Join(device, "power", "power_state")); err == nil {
		if v, err := strconv.ParseFloat(strings.TrimSpace(string(s)), 64); err == nil {
			w := v / 1e6
			g.PowerW = &w
		}
	}
	// clocks: try common attribute
	if s, err := os.ReadFile(filepath.Join(device, "gpu_clk")); err == nil {
		if v, err := strconv.ParseFloat(strings.TrimSpace(string(s)), 64); err == nil {
			ci := int(v)
			g.ClockMHz = &ci
		}
	}
	return nil
}
