package model

type Process struct {
	PID        int
	PPID       int
	StartTime  uint64
	Name       string
	Cmdline    string
	CPUPercent float64
	MemPercent float64
	RSSBytes   uint64
}
