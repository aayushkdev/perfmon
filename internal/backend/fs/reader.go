package fs

import (
	"os"
	"strconv"
	"strings"
	"syscall"

	"github.com/aayushkdev/perfmon/internal/model"
)

func KHzAsMHz(path string) int {
	raw := ReadString(path)
	if raw == "" {
		return 0
	}
	khz, err := strconv.Atoi(raw)
	if err != nil {
		return 0
	}
	return khz / 1000
}

func ReadString(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func ReadInt(path string, fallback int) int {
	raw := ReadString(path)
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return value
}

func FirstExistingPath(paths ...string) string {
	for _, path := range paths {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ""
}

func WriteString(path string, value string) error {
	return os.WriteFile(path, []byte(value), 0o644)
}

func FirstNonEmptyGovernor(cores []model.CPUCore) string {
	for _, core := range cores {
		if core.Governor != "" {
			return core.Governor
		}
	}
	return ""
}

func FirstNonEmptyEPP(cores []model.CPUCore) string {
	for _, core := range cores {
		if core.EPP != "" {
			return core.EPP
		}
	}
	return ""
}

func KernelRelease() string {
	var uname syscall.Utsname
	if err := syscall.Uname(&uname); err != nil {
		return ""
	}
	return int8SliceToString(uname.Release[:])
}

func MachineArch() string {
	var uname syscall.Utsname
	if err := syscall.Uname(&uname); err != nil {
		return ""
	}
	return int8SliceToString(uname.Machine[:])
}

func int8SliceToString(in []int8) string {
	var b strings.Builder
	for _, v := range in {
		if v == 0 {
			break
		}
		b.WriteByte(byte(v))
	}
	return b.String()
}
