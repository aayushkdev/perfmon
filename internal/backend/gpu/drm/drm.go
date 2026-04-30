package drm

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/aayushkdev/perfmon/internal/model"
)

type Reader struct {
	sys string
}

func New(sys string) Reader {
	return Reader{sys: sys}
}

func (r Reader) Read() []model.GPU {
	cards, _ := filepath.Glob(filepath.Join(r.sys, "class/drm/card[0-9]*"))
	gpus := make([]model.GPU, 0, len(cards))
	for _, card := range cards {
		device := filepath.Join(card, "device")
		vendor := vendorName(readString(filepath.Join(device, "vendor")))
		if vendor == "" {
			continue
		}
		gpu := model.GPU{
			ID:     filepath.Base(card),
			Vendor: vendor,
			Name:   readString(filepath.Join(device, "product_name")),
		}
		// Try to read hwmon temperature and power exposed under the device
		// e.g. /sys/class/drm/cardX/device/hwmon/hwmon*/temp*_input
		if temp := readFirstMilliTemperature(filepath.Join(device, "hwmon", "hwmon*", "temp*_input")); temp != nil {
			gpu.TemperatureC = temp
		}
		// power files in hwmon are typically in microwatts
		if p := readFirstMicroPower(filepath.Join(device, "hwmon", "hwmon*", "power*_input")); p != nil {
			gpu.PowerW = p
		}
		gpus = append(gpus, gpu)
	}
	return gpus
}

func vendorName(raw string) string {
	switch strings.ToLower(raw) {
	case "0x1002":
		return "AMD"
	case "0x10de":
		return "NVIDIA"
	case "0x8086":
		return "Intel"
	default:
		return strings.TrimSpace(raw)
	}
}

func readString(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func readFirstMilliTemperature(pattern string) *float64 {
	matches, _ := filepath.Glob(pattern)
	for _, path := range matches {
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		s := strings.TrimSpace(string(raw))
		if s == "" {
			continue
		}
		v, err := strconv.ParseFloat(s, 64)
		if err != nil || v <= 0 {
			continue
		}
		c := v / 1000.0
		return &c
	}
	return nil
}

func readFirstMicroPower(pattern string) *float64 {
	matches, _ := filepath.Glob(pattern)
	for _, path := range matches {
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		s := strings.TrimSpace(string(raw))
		if s == "" {
			continue
		}
		v, err := strconv.ParseFloat(s, 64)
		if err != nil || v <= 0 {
			continue
		}
		w := v / 1e6
		return &w
	}
	return nil
}
