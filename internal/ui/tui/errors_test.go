package tui

import (
	"errors"
	"fmt"
	"os"
	"testing"
)

func TestActionErrorPermission(t *testing.T) {
	err := fmt.Errorf("failed to apply scaling_governor: %w", &os.PathError{Op: "open", Path: "/sys/devices/system/cpu/cpu0/cpufreq/scaling_governor", Err: os.ErrPermission})
	got := actionError("set governor performance", err)
	want := "[red]Failed to set governor performance: permission denied[-]"
	if got != want {
		t.Fatalf("actionError() = %q, want %q", got, want)
	}
}

func TestActionErrorKeepsNonPermissionReason(t *testing.T) {
	got := actionError("signal 42 (worker)", errors.New("no such process"))
	want := "[red]Failed to signal 42 (worker): no such process[-]"
	if got != want {
		t.Fatalf("actionError() = %q, want %q", got, want)
	}
}
