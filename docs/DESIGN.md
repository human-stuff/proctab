# Design notes

This file records the research behind the project and the decisions taken.

## Why another monitor?

| Tool | What it does well | What it lacks for us |
|------|-------------------|----------------------|
| htop | Processes, meters, F-key UX; a column shows which container a process is in | No port list (`l` runs `lsof` for one process); no container list or Docker stats |
| btop, bottom, gotop, zenith | Graphs, widgets | No socket→PID view, no Docker |
| glances | Docker plugin, web mode | Needs a Python runtime; no drill-down |
| ctop, lazydocker | Containers | No host processes or ports |
| ss, netstat, lsof | Have the raw data | Not interactive |
| portop, portctl, portwatch | Ports | Not a full process monitor, no system view |
| procs | Ports and container names in `ps` | Not a TUI with drill-down panes |

The gap we fill: one small static binary with:
- an htop-like process list
- listening ports, with a well-known-services status grid
- Docker containers
- system info

all sharing **one drill-down view** that shows PID, parent, user, full command
line, cwd, exe, start time, sockets, cgroup/container and environment.

## Decisions

1. **Go with tview/tcell.**
   - tview provides Table, Pages, Flex, Modal and InputField, with mouse
     support, at the smallest code cost.
   - bubbletea would mean more boilerplate and pulls toward a "modern" look.
   - termui sits on unmaintained termbox.
   - Raw tcell would mean writing tables and scrolling from scratch.

2. **No gopsutil and no Docker SDK.**
   - Everything is read from `/proc`.
   - Docker is reached with `net/http` over the unix socket.
   - This keeps the dependency tree to tview + tcell + x/sys and the binary
     static (`CGO_ENABLED=0`).

3. **htop look and keys.**
   - Header with per-core CPU meters plus Mem and Swp.
   - Tasks, Load average and Uptime on the right.
   - Black-on-green column header, black-on-cyan selection, F-key bar at the
     bottom.
   - htop key aliases: `/` `\` `t` `P` `M` `T` `I` `K` `k` `[` `]` `q`.
   - The terminal's default background is kept.

4. **Tabs.**
   - Processes, Ports, Docker and System, switched with `1`–`4`, Tab, or a
     mouse click.
   - F1–F10 mean the same thing in every tab.
   - Enter always drills down; Esc always goes back.

5. **Navigation stack in the detail view.** You can follow the chain
   container → init PID → child → parent, and Esc walks back through it.

6. **Service grid colours.**
   - green ● : something listens on the service's port, or a process with the
     service's name holds a listening socket
   - yellow ◐ : the daemon is running but holds no port
   - red ○ : not running

## Linux internals used

- **Sockets.** Read `/proc/net/{tcp,tcp6,udp,udp6}`.
  - Addresses are hex. **Each 32-bit word is little-endian**, which matters
    for IPv6.
  - TCP state `0A` means LISTEN. UDP has no LISTEN state: a bound UDP socket
    with state `07` and remote port 0 counts as listening.
- **Socket → PID.** Use the inode column, then look for `socket:[inode]` links
  under `/proc/*/fd/*`.
  - Walking every fd is the most expensive step, so results are cached.
  - A rescan runs only when an unknown inode appears, and it stops once every
    wanted inode is found.
  - Sockets whose owner can't be read (non-root) are cached as PID 0, so they
    don't trigger a rescan every tick.
- **Processes.**
  - `/proc/[pid]/stat`: split after the **last** `)`, because `comm` may
    contain spaces or parentheses.
  - `/proc/[pid]/statm` for RES and SHR.
  - The owner of `/proc/[pid]` gives the uid without parsing `status`.
  - cmdline is cached per (pid, starttime) and refreshed every 10 scans.
  - Raw `syscall.Open`/`Read` into a reused buffer halves scan time compared
    with `os.ReadFile`.
- **CPU%.** Δ(utime+stime) per process ÷ Δ(total jiffies / ncpu). 100% means
  one core, as in htop. Samples are keyed by (pid, starttime) so a reused PID
  isn't diffed against a different process.
- **Containers.** A process is in a container if its `/proc/[pid]/cgroup`
  contains a 64-hex ID (docker, containerd, crio or podman layouts).
  - Published ports are matched to `docker-proxy` listeners.
  - `/containers/{id}/top` lists the container's processes as host PIDs.
- **Docker API.**
  - Endpoints: `/containers/json?all=1`, `/containers/{id}/json`,
    `/containers/{id}/top`, `/containers/{id}/stats?stream=false`.
  - Stats calls are slow (~1–2 s each), so they run concurrently and only
    while the Docker tab is open.

## Permissions

As a non-root user, `/proc/[pid]/{fd,cwd,exe,environ}` of other users' processes
can't be read.
- The UI degrades gracefully: PID shows as `-`, and the owner is taken from the
  socket table's uid column.
- The footer counts sockets with a hidden owner.
- The detail view lists exactly which fields were unreadable.
- `sudo` gives the full picture.

## Measured footprint

Measured on a desktop with ~400 processes at a 1.5 s refresh:
- ~3% of one core on the Processes tab, ~3.5% on the Ports tab
- ~17 MB RSS
- 7.4 MB static binary (`-s -w`)

## Roadmap ideas

- Config file (`$XDG_CONFIG_HOME/<name>/config`) for the service list, colours
  and refresh rate
- Per-process I/O (`/proc/[pid]/io`) and per-port connection counts
- Docker actions: logs, restart, stop
- Podman socket support
- Optional history sparklines in the System tab
- FreeBSD/macOS backends (sysctl) behind the same interfaces
