package render

import (
	"fmt"
	"github.com/aayushkdev/perfmon/internal/model"
	"strings"
)

func GPU(gpus []model.GPU) string {
	if len(gpus) == 0 {
		return "[silver]No DRM GPU devices detected.[-]\n\n[silver]AMD/Intel metrics can be added through DRM/hwmon backends; NVIDIA can use an optional nvidia-smi backend.[-]"
	}
	lines := make([]string, 0, len(gpus)*2)
	for _, gpu := range gpus {
		// First line: show the product name if available, otherwise the vendor.
		lines = append(lines, fmt.Sprintf("[white::b]%s[-:-:-]", fallback(gpu.Name, gpu.Vendor)))
		if gpu.Driver != "" {
			lines = append(lines, fmt.Sprintf("[silver]driver[-] %s", gpu.Driver))
		}
		if gpu.PCIID != "" {
			lines = append(lines, fmt.Sprintf("[silver]PCI[-] %s", gpu.PCIID))
		}
		if len(gpu.Outputs) > 0 {
			lines = append(lines, fmt.Sprintf("[silver]outputs[-] %s", strings.Join(gpu.Outputs, ", ")))
		}
		metrics := make([]string, 0, 2)
		if gpu.TemperatureC != nil {
			metrics = append(metrics, fmt.Sprintf("[silver]temp[-] %.1fC", *gpu.TemperatureC))
		}
		if gpu.PowerW != nil {
			metrics = append(metrics, fmt.Sprintf("[silver]power[-] %.2fW", *gpu.PowerW))
		}
		if len(metrics) > 0 {
			lines = append(lines, strings.Join(metrics, "   "))
		}
	}
	return strings.Join(lines, "\n")
}
