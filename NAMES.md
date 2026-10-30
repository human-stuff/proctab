# Name candidates

**Chosen name: `proctab`.** (The earlier placeholder `tmon` clashes with the
Linux kernel's thermal monitor, `tools/thermal/tmon`.)

To rename again later:

```sh
./scripts/rename.sh <newname> [module-path]
# e.g. ./scripts/rename.sh hoptop github.com/<you>/hoptop
```

## Candidates

Availability was checked with web searches only (October 2026). Before you
commit to a name, check GitHub, Homebrew, AUR, Debian/Ubuntu packages and
pkg.go.dev directly.

| Name      | Status   | Meaning / pitch |
|-----------|----------|-----------------|
| **hoptop**  | clear    | Nods to htop, easy to say and remember. "hop" between procs, ports, containers. |
| **boxtop**  | clear    | "Everything on this box": host + containers in one view. |
| **pidtop**  | clear    | Ties every port and container back to a PID, which is the core feature. |
| **hostop**  | clear    | "host top". Plain and descriptive. |
| **whotop**  | clear    | "Who owns this port / process?" |
| **lsntop**  | clear    | From "listen top". Unusual, so easy to search for. |
| **tabtop**  | clear    | Refers to the tabbed design. |
| **proctab** | clear    | Quieter, ps-like name. |
| sysdeck   | unsure   | No exact match found, but "deck" names are crowded. |
| porttop   | unsure   | No hits, but one letter away from `portop`, so too confusing. |

## Avoid (already taken)

| Name | Taken by |
|------|----------|
| portop    | padovanl/portop: Go "htop for sockets", on Homebrew. A direct competitor. |
| socktop   | Rust remote monitor (crates.io) |
| dtop      | amir20/dtop: Docker TUI, also on PyPI |
| pstop     | "htop for Windows" (Rust crate) |
| lazyports | several Go repos |
| portwatch, portctl | Go port TUIs |
| tmon      | Linux kernel thermal monitor |

**Shortlist:** `hoptop`, `boxtop` or `pidtop`. Avoid names built on "port":
that space is crowded and `portop` already owns the obvious one.
