package render

import (
	"fmt"
	"github.com/aayushkdev/perfmon/internal/model"
	"strings"
)

func Thermals(s model.Snapshot) string {
	if len(s.Thermals) == 0 {
		return "[silver]No thermal sensors detected.[-]\n\n[silver]Thermal zones (hwmon/thermal) are used when the kernel exposes them.[-]"
	}
	lines := make([]string, 0, len(s.Thermals)+1)
	lines = append(lines, "[white::b]Thermals[-:-:-]")
	var coreMax *float64
	for _, sensor := range s.Thermals {
		lower := strings.ToLower(sensor.Name)
		if strings.HasPrefix(lower, "coretemp: core ") {
			if sensor.TemperatureC != nil && (coreMax == nil || *sensor.TemperatureC > *coreMax) {
				value := *sensor.TemperatureC
				coreMax = &value
			}
			continue
		}
		name := sensor.Name
		if strings.HasPrefix(lower, "coretemp: package") {
			name = "CPU package"
		} else if strings.HasPrefix(lower, "nvme: ") {
			name = "NVMe " + sensor.Name[len("nvme: "):]
		}
		lines = append(lines, fmt.Sprintf("[white]%s[-] %s", name, floatPtr(sensor.TemperatureC, "C")))
	}
	if coreMax != nil {
		lines = append(lines, fmt.Sprintf("[white]CPU cores max[-] %.1fC", *coreMax))
	}
	return strings.Join(lines, "\n")
}
