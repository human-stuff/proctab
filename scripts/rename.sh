#!/usr/bin/env bash
# Rename the project: ./scripts/rename.sh <name> [module-path]
# e.g. ./scripts/rename.sh hoptop github.com/you/hoptop
set -euo pipefail
new=${1:?usage: rename.sh <name> [module-path]}
mod=${2:-$new}
cd "$(dirname "$0")/.."
old=$(awk '/^module /{print $2}' go.mod)
oldname=$(awk -F'"' '/const appName/{print $2}' cmd/*/main.go)
[[ "$new" =~ ^[a-z][a-z0-9-]*$ ]] || { echo "name must be lowercase letters/digits/-"; exit 1; }

go mod edit -module "$mod"
grep -rl --include='*.go' "\"$old/" . | xargs -r sed -i "s#\"$old/#\"$mod/#g"
[ -d "cmd/$oldname" ] && git mv "cmd/$oldname" "cmd/$new" 2>/dev/null || mv "cmd/$oldname" "cmd/$new"
sed -i "s/const appName = \"$oldname\"/const appName = \"$new\"/; s/Command $oldname /Command $new /" "cmd/$new/main.go"
sed -i "s/\b$oldname\b/$new/g" Makefile .goreleaser.yaml README.md LICENSE
gofmt -w cmd internal
go build ./... && echo "renamed to $new (module $mod)"
