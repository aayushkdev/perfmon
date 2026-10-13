package controls

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetTurboEnabledReportsWriteFailure(t *testing.T) {
	sys := t.TempDir()
	path := filepath.Join(sys, "devices/system/cpu/intel_pstate/no_turbo")
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	err := (&Controller{Sys: sys}).SetTurboEnabled(context.Background(), true)
	if err == nil || strings.Contains(err.Error(), "unavailable") {
		t.Fatalf("expected write error, got %v", err)
	}
}
