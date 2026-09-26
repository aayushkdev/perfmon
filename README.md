# perfmon

perfmon is a terminal-based system performance monitor and control utility for Linux. It reads kernel telemetry from `/proc` and `/sys` and provides a compact TUI showing CPU, memory, thermal, power and GPU information. When run with appropriate privileges it can also adjust CPU governor, energy-performance preference (EPP), turbo and core online state.

**Highlights**
- Live TUI with CPU, memory, thermals, batteries, and GPU panels.
- Optional writable controls for governors, EPP, turbo, power profile, and core online.
- Designed to work by reading standard kernel interfaces (`/proc`, `/sys`, `firmware/acpi`, hwmon, DRM).

**Quick build & run**

Requirements:
- Go 1.20+ (module-enabled)

Install with `go install`:

```bash
go install github.com/aayushkdev/perfmon/cmd/perfmon@latest
```

Build from source:

```bash
git clone https://github.com/aayushkdev/perfmon.git
cd perfmon
go build -o perfmon ./cmd/perfmon
```

Run (recommended without writable controls):

```bash
./perfmon
```

Run with writable controls (turbo, toggling cores, power profiles requires root privileges):

```bash
sudo ./perfmon
```

Controls (keyboard)
- `q`: quit
- `r`: refresh
- `g`: cycle CPU governor
- `p`: toggle between cores and processes view
- `c`: cycle process sort order (mem → cpu → pid → name)
- `s`: toggle ascending/descending for the current process sort column
- `e`: cycle EPP (energy-performance preference)
- `m`: cycle power profile (powersave / balanced / performance)
- `t`: toggle turbo
- `o`: toggle selected core online/offline (requires kernel support and privileges)

Development
- Run tests:

```bash
go test ./...
```

- Run the app from source:

```bash
go run ./cmd/perfmon
```

Notes
- The collector inspects `/proc` and `/sys` and will show reduced information if run inside limited environments (containers or non-Linux systems).
- Writable controls write to sysfs and firmware interfaces; they may be unavailable depending on kernel configuration or platform. Use with caution.

Contributing
- Contributions, bug reports and feature requests are welcome. Open issues or pull requests with focused changes.
