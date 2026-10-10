package gpu

import (
	"path/filepath"

	"github.com/aayushkdev/perfmon/internal/backend/gpu/sources"
	"github.com/aayushkdev/perfmon/internal/backend/gpu/vendors"
	"github.com/aayushkdev/perfmon/internal/model"
)

func Read(sys string) []model.GPU {
	gpus := sources.New(sys).Read()
	for i := range gpus {
		device := filepath.Join(sys, "class/drm", gpus[i].ID, "device")
		switch gpus[i].Vendor {
		case "NVIDIA":
			_ = vendors.EnrichNVIDIA(sys, device, &gpus[i])
		case "AMD":
			_ = vendors.EnrichAMD(sys, device, &gpus[i])
		case "Intel":
		}
	}
	return gpus
}
