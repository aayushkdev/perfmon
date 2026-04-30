package backend

import (
	"context"

	"github.com/aayushkdev/perfmon/internal/model"
)

type SnapshotCollector interface {
	Snapshot(ctx context.Context) (model.Snapshot, error)
}

type CoreController interface {
	SetCoreOnline(ctx context.Context, id int, online bool) error
	CanSetCoreOnline(id int) bool
	SetCPUGovernor(ctx context.Context, governor string) error
	SetEPP(ctx context.Context, preference string) error
	SetTurboEnabled(ctx context.Context, enabled bool) error
	SetPowerProfile(ctx context.Context, profile string) error
}
