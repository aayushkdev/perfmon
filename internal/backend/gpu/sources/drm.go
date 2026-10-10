package sources

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
	entries, _ := os.ReadDir(filepath.Join(r.sys, "class/drm"))
	gpus := make([]model.GPU, 0, len(entries))
	for _, entry := range entries {
		if !isCardName(entry.Name()) {
			continue
		}
		card := filepath.Join(r.sys, "class/drm", entry.Name())
		device := filepath.Join(card, "device")
		vendor := vendorName(readString(filepath.Join(device, "vendor")))
		if vendor == "" {
			continue
		}
		uevent := readKeyValues(filepath.Join(device, "uevent"))
		gpu := model.GPU{
			ID:      entry.Name(),
			Vendor:  vendor,
			Name:    deviceName(device, vendor),
			Driver:  uevent["DRIVER"],
			PCIID:   uevent["PCI_ID"],
			Outputs: connectorOutputs(filepath.Join(r.sys, "class/drm"), entry.Name()),
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

func connectorOutputs(drmPath, card string) []string {
	entries, _ := os.ReadDir(drmPath)
	prefix := card + "-"
	outputs := make([]string, 0)
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), prefix) {
			continue
		}
		connector := filepath.Join(drmPath, entry.Name())
		status := readString(filepath.Join(connector, "status"))
		if status != "connected" {
			continue
		}
		name := strings.TrimPrefix(entry.Name(), prefix)
		name += " (connected)"
		outputs = append(outputs, name)
	}
	return outputs
}

func isCardName(name string) bool {
	if !strings.HasPrefix(name, "card") || len(name) == len("card") {
		return false
	}
	_, err := strconv.Atoi(name[len("card"):])
	return err == nil
}

func deviceName(device, vendor string) string {
	for _, name := range []string{"product_name", "name", "model_name", "label"} {
		if value := readString(filepath.Join(device, name)); value != "" {
			return value
		}
	}
	return vendor + " GPU"
}

func readKeyValues(path string) map[string]string {
	values := make(map[string]string)
	data, err := os.ReadFile(path)
	if err != nil {
		return values
	}
	for _, line := range strings.Split(string(data), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if ok && strings.TrimSpace(key) != "" {
			values[strings.TrimSpace(key)] = strings.TrimSpace(value)
		}
	}
	return values
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
