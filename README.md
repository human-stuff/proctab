# proctab

> Name candidates considered: [NAMES.md](NAMES.md).

A lightweight, htop-style terminal monitor for **processes, ports, Docker
containers and system info**, in one static binary with no runtime
dependencies.

Example screen (Ports tab):

```
  0[|||||||||||||               20.3%]  4[||||||||||||||||||||        64.9%]
  1[|||||||||||||               19.3%]  5[||||||||||||                37.2%]
 Mem[|||||||||||||||||||||10.7G/14.9G]  Tasks: 404, 2030 thr; 2 running
 Swp[|||||||                 1.2G/4.0G]  Load average: 3.82 2.46 1.97
                                         Uptime: 4 days, 00:14:29
 1 Processes   2 Ports   3 Docker   4 System
┌ Services: 3 up / 18 down ───────────────────────────────────────────────┐
│● ssh :22        ○ http          ○ https        ● nginx :80,443          │
│○ mysql          ● postgres :5432 ○ redis       ◐ docker (no port)       │
└─────────────────────────────────────────────────────────────────────────┘
PROTO PORT ADDRESS          SERVICE   PID USER     PROCESS  CONTAINER COMMAND
TCP     22 *,[::]           ssh       812 root     sshd               sshd: /usr/sbin/sshd -D
TCP     80 *                nginx    1290 root     nginx              nginx: master process
TCP   5432 *                postgres 2211 -        docker-proxy  db   /usr/bin/docker-proxy ...
F1Help  F3SearchF4FilterF5All   F9Kill  F10Quit  sSvcs  EnterInfo
```

## Why

`htop` shows processes, `ss -tlnp` shows ports, `docker ps` shows containers,
and you end up jumping between all three. This tool puts them in one
htop-like screen. Select any port, process or container and press **Enter**
to see:

- PID, parent, user, state, threads, start time
- the full command line, executable path and working directory
- listening and connected sockets
- cgroup and container, with a link to the container
- environment variables (press `e`)
- related processes (parent, children, the container's processes), which you
  can open in turn. Esc goes back.

## Features

- **Processes**:
  - htop columns (PID USER PRI NI VIRT RES SHR S CPU% MEM% TIME+ Command)
  - tree view, sort, search, filter
  - kill with a chosen signal, renice
- **Ports**:
  - status grid of well-known services: **green** = listening, **yellow** =
    running without a port, **red** = down
  - every listening TCP/UDP socket with its owning PID, process, container
    and command
  - `a` shows all sockets, including established connections
- **Docker**:
  - every container with state, CPU%, memory and ports
  - Enter shows the inspect data (command, entrypoint, workdir, mounts,
    networks, ports, env) and the container's host PIDs
- **System**:
  - OS, kernel, uptime, CPU model, load
  - memory and swap meters
  - disks with usage bars
  - network interfaces with RX/TX rates and addresses
  - summary counts
- **Lightweight**:
  - reads `/proc` directly and talks to the Docker socket directly; no
    gopsutil, no Docker SDK
  - about 3% of one core and 17 MB RSS with 400 processes
- **Mouse**: click tabs, F-keys and column headers; double-click opens.

## Install

Every tagged release on GitHub has static binaries for linux amd64, arm64,
armv7, 386, riscv64, ppc64le, s390x and loong64, plus packages.

**apt (Debian / Ubuntu):**

```sh
sudo mkdir -p /etc/apt/keyrings
sudo curl -fsSL https://human-stuff.github.io/proctab/proctab.asc -o /etc/apt/keyrings/proctab.asc
echo "deb [signed-by=/etc/apt/keyrings/proctab.asc] https://human-stuff.github.io/proctab/apt stable main" \
  | sudo tee /etc/apt/sources.list.d/proctab.list
sudo apt update && sudo apt install proctab
```

`apt upgrade` then picks up new releases.

**pip / pipx** (the wheel contains the static binary, no Python code runs):

```sh
pipx install proctab --index-url https://human-stuff.github.io/proctab/simple/
# or
pip install proctab --index-url https://human-stuff.github.io/proctab/simple/
```

**Other options:**

```sh
# Debian / Ubuntu, single file
sudo apt install ./proctab_<version>_amd64.deb
# Fedora / RHEL / openSUSE
sudo rpm -i proctab-<version>.x86_64.rpm
# Alpine
sudo apk add --allow-untrusted proctab_<version>_x86_64.apk
# Arch
sudo pacman -U proctab-<version>-1-x86_64.pkg.tar.zst
# Any distro: plain binary
tar xzf proctab_<version>_linux_amd64.tar.gz && sudo install proctab /usr/local/bin/

# With Go
go install github.com/human-stuff/proctab/cmd/proctab@latest

# From source
make build && sudo make install
```

As a container, it needs the host's PID and network namespaces:

```sh
docker run --rm -it --pid=host --net=host --cap-add SYS_PTRACE \
  -v /var/run/docker.sock:/var/run/docker.sock -v /etc/passwd:/etc/passwd:ro \
  ghcr.io/human-stuff/proctab
```

Linux only for now; it needs `/proc`.

## Usage

```
proctab [-d seconds] [-t processes|ports|docker|system] [-v]
```

Run with `sudo` to see the owners of every socket and the cwd, exe and
environment of other users' processes. Without root, these show as `-` and the
footer tells you how many sockets are hidden.

Docker access needs membership of the `docker` group, or root. Rootless Docker
(`$XDG_RUNTIME_DIR/docker.sock`) and `DOCKER_HOST=unix://...` are supported.

## Keys

| Key | Action |
|-----|--------|
| `1` `2` `3` `4`, Tab / Shift-Tab | switch tab |
| F1 `?` `h` | help |
| F3 `/` | search (`n` finds the next match) |
| F4 `\` | filter (Esc in the prompt clears it) |
| Enter, double-click | drill down |
| Esc / Backspace | back |
| F10 `q` | quit |
| **Processes** | |
| F5 `t` | tree view |
| F6 `<` `>` | change sort column; click a header to sort |
| `P` `M` `T` `N` | sort by CPU, MEM, TIME, PID |
| `I` | invert sort |
| `K` | show or hide kernel threads |
| `u` | only my processes |
| F7 `]`, F8 `[` | nice −, nice + |
| F9 `k` | send a signal |
| **Ports** | |
| F5 `a` | toggle listening / all sockets |
| `s` | focus the service grid; Enter or click filters by that service |
| **Docker** | |
| F5 `a` | toggle all / running containers |
| **Detail** | |
| Tab | switch focus between the text and the related list |
| `e` | show or hide environment variables |
| F9 | kill |

## Layout

```
cmd/proctab/         entry point and flags
internal/proc/       /proc process scanner, CPU%, tree, socket inode cache
internal/netstat/    /proc/net parser, well-known services list
internal/sysinfo/    CPU, memory, load, disks, network
internal/docker/     minimal Docker Engine API client (unix socket)
internal/ui/         tview UI: header, tabs, views, detail stack
docs/DESIGN.md       research and design decisions
```

## Development and release

```sh
make build     # ./bin/proctab
make test      # unit tests
make vet       # gofmt + go vet
make snapshot  # local dry run of the release (needs goreleaser)
make docker    # local container image
```

GitHub Actions (`.github/workflows/`):

| Workflow | Trigger | What it does |
|----------|---------|--------------|
| `ci.yml` | push to `main`, pull requests | gofmt check, `go vet`, `go test -race`; cross-builds all 8 architectures; `goreleaser check` plus a snapshot build (packages uploaded as artifacts); multi-arch Docker build |
| `release.yml` | tag `v*` | tests, then GoReleaser publishes the GitHub Release (tar.gz, .deb, .rpm, .apk, Arch package, checksums, changelog); pushes a multi-arch image to `ghcr.io/human-stuff/proctab`; rebuilds the GitHub Pages site with the signed APT repo and pip index; optionally publishes to PyPI |

One-time setup before the first release (repo admin, needs `gh`):

```sh
./packaging/setup-signing-key.sh   # APT signing key -> APT_GPG_PRIVATE_KEY secret, enables Pages
```

For `pip install proctab` straight from PyPI:
1. Add a trusted publisher on pypi.org (workflow `release.yml`, environment `pypi`).
2. Set the repository variable `PUBLISH_PYPI=true`.

Dependabot keeps Go modules and the actions up to date.

To publish a release:

```sh
git tag v0.1.0
git push origin v0.1.0
```

Tags with a suffix (for example `v0.2.0-rc1`) are published as pre-releases
and don't move the `latest` image tag.

## License

MIT. See [LICENSE](LICENSE).
