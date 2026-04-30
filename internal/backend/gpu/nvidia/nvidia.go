package nvidia

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/aayushkdev/perfmon/internal/model"
)

// Enrich attempts to populate NVIDIA GPU fields using nvidia-smi.
func Enrich(sys, device string, g *model.GPU) error {
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	cmd := exec.CommandContext(ctx, "nvidia-smi", "--query-gpu=index,name,utilization.gpu,temperature.gpu,power.draw,clocks.sm", "--format=csv,noheader,nounits")
	out, err := cmd.Output()
	if err != nil {
		return err
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	// try to match by name first
	for _, line := range lines {
		fields := splitCSVLine(line)
		if len(fields) < 6 {
			continue
		}
		name := strings.TrimSpace(fields[1])
		if name == "" {
			continue
		}
		if strings.Contains(strings.ToLower(name), strings.ToLower(g.Name)) || strings.Contains(strings.ToLower(g.Name), strings.ToLower(name)) {
			if v, err := parseFloat(fields[2]); err == nil {
				g.UtilPercent = &v
			}
			if t, err := parseFloat(fields[3]); err == nil {
				g.TemperatureC = &t
			}
			if p, err := parseFloat(fields[4]); err == nil {
				g.PowerW = &p
			}
			if c, err := parseInt(fields[5]); err == nil {
				ci := int(c)
				g.ClockMHz = &ci
			}
			return nil
		}
	}
	// fallback: try to match by index ordering
	for _, line := range lines {
		fields := splitCSVLine(line)
		if len(fields) < 6 {
			continue
		}
		idxStr := strings.TrimSpace(fields[0])
		idx, err := strconv.Atoi(idxStr)
		if err != nil {
			continue
		}
		if strings.HasSuffix(g.ID, fmt.Sprintf("card%d", idx)) || strings.HasPrefix(filepath.Base(device), fmt.Sprintf("card%d", idx)) {
			if v, err := parseFloat(fields[2]); err == nil {
				g.UtilPercent = &v
			}
			if t, err := parseFloat(fields[3]); err == nil {
				g.TemperatureC = &t
			}
			if p, err := parseFloat(fields[4]); err == nil {
				g.PowerW = &p
			}
			if c, err := parseInt(fields[5]); err == nil {
				ci := int(c)
				g.ClockMHz = &ci
			}
			return nil
		}
	}
	return nil
}

func splitCSVLine(line string) []string {
	parts := make([]string, 0)
	for _, p := range strings.Split(line, ",") {
		parts = append(parts, strings.TrimSpace(p))
	}
	return parts
}

func parseFloat(s string) (float64, error) {
	s = strings.TrimSpace(s)
	return strconv.ParseFloat(s, 64)
}

func parseInt(s string) (int64, error) {
	s = strings.TrimSpace(s)
	return strconv.ParseInt(s, 10, 64)
}
