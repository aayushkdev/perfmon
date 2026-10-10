package backend

import (
	"github.com/aayushkdev/perfmon/internal/model"
	"path/filepath"
	"strconv"
	"strings"
)

func (c *Collector) readCPUTemperature() *float64 {
	thermals := c.readThermals()
	return firstThermalTemperature(thermals)
}

func (c *Collector) readThermals() []model.ThermalSensor {
	sensors := make([]model.ThermalSensor, 0, 8)
	seenNames := make(map[string]bool)
	for _, path := range c.thermalSensorPaths() {
		base := filepath.Dir(path)
		name := readString(filepath.Join(base, "name"))
		if name == "" {
			name = readString(filepath.Join(base, "type"))
		}
		if name == "" {
			name = filepath.Base(base)
		}
		temp := readMilliTemperature(path)
		if temp == nil {
			continue
		}
		prefix := strings.TrimSuffix(filepath.Base(path), "_input")
		if label := readString(filepath.Join(base, prefix+"_label")); label != "" {
			name += ": " + label
		}
		sensor := model.ThermalSensor{
			Name:         name,
			Source:       thermalSourceLabel(base),
			TemperatureC: temp,
			MaxC:         readMilliTemperature(firstExistingPath(filepath.Join(base, prefix+"_max"), filepath.Join(base, "trip_point_0_temp"))),
			CriticalC:    readMilliTemperature(firstExistingPath(filepath.Join(base, prefix+"_crit"), filepath.Join(base, "trip_point_1_temp"))),
			Throttled:    readBoolField(firstExistingPath(filepath.Join(base, prefix+"_alarm"), filepath.Join(base, "temp_alarm"))),
		}
		key := strings.ToLower(strings.TrimSpace(sensor.Name))
		if sensor.Source == "thermal" && seenNames[key] {
			continue
		}
		seenNames[key] = true
		sensors = append(sensors, sensor)
	}
	return sensors
}

func (c *Collector) thermalSensorPaths() []string {
	matches := make([]string, 0, 16)
	if hwmon, _ := filepath.Glob(filepath.Join(c.sys, "class/hwmon/hwmon*/temp*_input")); len(hwmon) > 0 {
		matches = append(matches, hwmon...)
	}
	if zones, _ := filepath.Glob(filepath.Join(c.sys, "class/thermal/thermal_zone*/temp")); len(zones) > 0 {
		matches = append(matches, zones...)
	}
	return matches
}

func thermalSourceLabel(base string) string {
	switch {
	case strings.Contains(base, "hwmon"):
		return "hwmon"
	case strings.Contains(base, "thermal_zone"):
		return "thermal"
	default:
		return "sensor"
	}
}

func readMilliTemperature(path string) *float64 {
	raw := readString(path)
	if raw == "" {
		return nil
	}
	milli, err := strconv.ParseFloat(raw, 64)
	if err != nil || milli <= 0 {
		return nil
	}
	value := milli / 1000
	return &value
}

func firstThermalTemperature(sensors []model.ThermalSensor) *float64 {
	for _, sensor := range sensors {
		if sensor.TemperatureC != nil {
			return sensor.TemperatureC
		}
	}
	return nil
}
