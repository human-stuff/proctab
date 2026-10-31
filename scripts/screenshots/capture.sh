#!/usr/bin/env bash
# Regenerate docs/screenshots/*.svg and docs/png/*.png from a clean demo
# environment. Needs docker, tmux, python3 and Pillow (PNG step).   usage: scripts/screenshots/capture.sh
set -euo pipefail
cd "$(dirname "$0")/../.."
out=docs/screenshots
mkdir -p "$out"
S=shot
names="demo-web demo-cache demo-api demo-old proctab-demo"

version=$(git describe --tags --abbrev=0 2>/dev/null || echo dev)
docker build -q --build-arg VERSION="$version" -f scripts/screenshots/Dockerfile -t proctab-demo . >/dev/null
docker rm -f $names >/dev/null 2>&1 || true
docker run -d --name demo-web -p 127.0.0.1:18080:80 proctab-demo web >/dev/null
docker run -d --name demo-cache proctab-demo cache >/dev/null
docker run -d --name demo-api -p 127.0.0.1:18000:8000 proctab-demo api >/dev/null
docker run --name demo-old proctab-demo old >/dev/null 2>&1 || true

tmux kill-session -t $S 2>/dev/null || true
tmux new-session -d -s $S -x 150 -y 42 \
	"docker run --rm -it --name proctab-demo --hostname demo-server --cap-add SYS_PTRACE -v /var/run/docker.sock:/var/run/docker.sock proctab-demo"
trap 'tmux kill-session -t $S 2>/dev/null; docker rm -f $names >/dev/null 2>&1' EXIT

keys() { tmux send-keys -t $S "$@"; }
text() { tmux send-keys -t $S -l "$1"; }
shot() { # name, title
	sleep "${3:-1.5}"
	tmux capture-pane -ept $S >"$out/$1.ansi"
	python3 scripts/screenshots/ansi2svg.py "$out/$1.ansi" "$out/$1.svg" --title "$2"
	rm "$out/$1.ansi"
	echo "$out/$1.svg"
}

sleep 6
shot processes "proctab — Processes"
keys F5; shot tree "proctab — Processes (tree view, F5)"
keys F5 2
shot ports "proctab — Ports" 2.5
keys / && sleep 0.3 && text nginx && keys Enter Enter
shot port-detail "proctab — Ports → Enter on nginx :80"
keys Escape 3 F4 && sleep 0.3 && text demo && keys Enter
shot docker "proctab — Docker (filter: demo)" 5
keys / && sleep 0.3 && text demo-web && keys Enter Enter
shot container "proctab — Docker → Enter on demo-web" 2.5
keys Escape 4
shot system "proctab — System" 2.5
keys F1; shot help "proctab — Help (F1)"

python3 scripts/screenshots/svg2png.py docs/png "$out"/*.svg
