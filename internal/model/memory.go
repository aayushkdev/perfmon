package model

type Memory struct {
	TotalBytes     uint64
	UsedBytes      uint64
	AvailableBytes uint64
	SwapTotalBytes uint64
	SwapUsedBytes  uint64
	// ModuleCount and SpeedMHz are nil when unavailable.
	ModuleCount *int
	SpeedMHz    *int
}
