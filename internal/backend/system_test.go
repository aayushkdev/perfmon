package backend

import (
	"testing"

	"github.com/aayushkdev/perfmon/internal/model"
)

func TestBuildTopology(t *testing.T) {
	cores := []model.CPUCore{
		{PackageID: 0, CoreID: 0, NodeID: 0},
		{PackageID: 0, CoreID: 1, NodeID: 0},
		{PackageID: 1, CoreID: 0, NodeID: 1},
	}

	top := buildTopology(cores)
	if !top.Known {
		t.Fatalf("expected topology to be known")
	}
	if top.Packages != 2 {
		t.Fatalf("packages = %d, want 2", top.Packages)
	}
	if top.NUMANodes != 2 {
		t.Fatalf("numa nodes = %d, want 2", top.NUMANodes)
	}
	if top.PhysicalCores != 3 {
		t.Fatalf("physical cores = %d, want 3", top.PhysicalCores)
	}
	if top.LogicalCores != 3 {
		t.Fatalf("logical cores = %d, want 3", top.LogicalCores)
	}
}

func TestNormalizeHybridTypesFromTopology(t *testing.T) {
	cores := []model.CPUCore{
		{TopologyType: "core"},
		{TopologyType: "atom"},
	}

	normalizeHybridTypes(cores)
	if cores[0].Type != model.CorePerformance {
		t.Fatalf("core 0 type = %q, want performance", cores[0].Type)
	}
	if cores[1].Type != model.CoreEfficiency {
		t.Fatalf("core 1 type = %q, want efficiency", cores[1].Type)
	}
}

func TestNormalizeHybridTypesFromCapacity(t *testing.T) {
	cores := []model.CPUCore{
		{Capacity: 200},
		{Capacity: 100},
	}

	normalizeHybridTypes(cores)
	if cores[0].Type != model.CorePerformance {
		t.Fatalf("core 0 type = %q, want performance", cores[0].Type)
	}
	if cores[1].Type != model.CoreEfficiency {
		t.Fatalf("core 1 type = %q, want efficiency", cores[1].Type)
	}
}
