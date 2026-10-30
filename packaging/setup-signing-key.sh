#!/usr/bin/env bash
# One-time setup, run by a repo admin (needs `gh` logged in):
#   1. creates a dedicated GPG key that signs the APT repository
#   2. stores the private key as the APT_GPG_PRIVATE_KEY Actions secret
#   3. keeps a backup at ~/.proctab-apt-signing-key.asc (keep it safe!)
#   4. enables GitHub Pages (source: GitHub Actions) for the repo
# Reusing the same key keeps users' apt trust working across releases.
set -euo pipefail
repo=${1:-human-stuff/proctab}
backup=~/.proctab-apt-signing-key.asc

GNUPGHOME=$(mktemp -d)
export GNUPGHOME
trap 'rm -rf "$GNUPGHOME"' EXIT

if [ -f "$backup" ]; then
	echo "reusing existing key from $backup"
	gpg --batch --import "$backup"
else
	gpg --batch --passphrase '' --quick-gen-key \
		"proctab APT repository <proctab@users.noreply.github.com>" ed25519 sign never
	(umask 077 && gpg --batch --armor --export-secret-keys >"$backup")
	echo "backup written to $backup"
fi
fpr=$(gpg --list-keys --with-colons | awk -F: '/^fpr/{print $10; exit}')

gh secret set APT_GPG_PRIVATE_KEY --repo "$repo" <"$backup"
echo "secret APT_GPG_PRIVATE_KEY set on $repo (key $fpr)"

gh api -X POST "repos/$repo/pages" -f build_type=workflow >/dev/null 2>&1 ||
	gh api -X PUT "repos/$repo/pages" -f build_type=workflow >/dev/null
echo "GitHub Pages enabled: https://${repo%%/*}.github.io/${repo##*/}/"
