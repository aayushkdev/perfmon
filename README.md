# perfmon

perfmon is a terminal system monitor for Linux. It shows live hardware stats and lets you control CPU settings and processes without leaving the terminal.

## What it does

- **Monitor:** View CPU usage and cores, memory, temperatures, power, battery, GPU, and running processes.
- **Control the CPU:** Cycle the governor, energy performance preference (EPP), and power mode. Toggle turbo or bring a selected CPU core online or offline.
- **Manage processes:** Search the process list and sort by PID, name, CPU, or memory. Switch between a flat list and a process tree, or send a signal to a selected process.

## Get started

Requires Go 1.26.2 or newer.

```bash
git clone https://github.com/aayushkdev/perfmon.git
cd perfmon
go build -o perfmon ./cmd/perfmon
./perfmon
```

You can monitor without root. CPU changes and signaling processes you do not own may require elevated privileges. To run with those privileges, use `sudo ./perfmon`. Available CPU controls depend on your hardware and kernel.

## Controls

### General

| Key | Action |
| --- | --- |
| `p` | Switch between cores and processes |
| `r` | Refresh |
| `q` | Quit |

### CPU

| Key | Action |
| --- | --- |
| `g` | Cycle CPU governor |
| `e` | Cycle EPP settings |
| `m` | Cycle power modes |
| `t` | Toggle turbo |
| `o` | Toggle the selected core online or offline |

### Processes

| Key | Action |
| --- | --- |
| `/` | Search processes |
| `s` | Cycle sort fields |
| `d` | Reverse sort order |
| `v` | Switch between list and tree views |
| `k` | Send SIGTERM to the selected process |
| `K` | Send SIGKILL to the selected process |
| `Enter` | Choose a signal for the selected process |
