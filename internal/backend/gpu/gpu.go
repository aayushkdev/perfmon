package gpu

import (
	"path/filepath"

	"github.com/aayushkdev/perfmon/internal/backend/gpu/amd"
	"github.com/aayushkdev/perfmon/internal/backend/gpu/drm"
	"github.com/aayushkdev/perfmon/internal/backend/gpu/nvidia"
	"github.com/aayushkdev/perfmon/internal/model"
)

// Read discovers GPUs via DRM and applies vendor-specific enrichers when available.
func Read(sys string) []model.GPU {
	gpus := drm.New(sys).Read()
	for i := range gpus {
		// device sysfs path for this DRM card
		device := filepath.Join(sys, "class/drm", gpus[i].ID, "device")
		switch gpus[i].Vendor {
		case "NVIDIA":
			_ = nvidia.Enrich(sys, device, &gpus[i])
		case "AMD":
			_ = amd.Enrich(sys, device, &gpus[i])
		case "Intel":
			// Intel: rely on DRM/hwmon reads already performed
		}
	}
	return gpus
}
