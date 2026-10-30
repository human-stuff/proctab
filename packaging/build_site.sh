#!/usr/bin/env bash
# Build the GitHub Pages site that serves:
#   apt/      a signed APT repository   (sudo apt install proctab)
#   simple/   a PEP 503 pip index       (pip install proctab --index-url .../simple/)
#
# usage: build_site.sh <dir-with-debs> <dir-with-wheels> <out-dir>
# env:   GPG_KEY_ID  key to sign the APT repo with (must be in the keyring)
#        SITE_URL    public URL of the site (for the landing page)
set -euo pipefail

debs=$(realpath "$1") wheels=$(realpath "$2") site=$3
url=${SITE_URL:-https://human-stuff.github.io/proctab}
key=${GPG_KEY_ID:-}

rm -rf "$site"
mkdir -p "$site/apt/pool/main" "$site/simple/proctab"
site=$(realpath "$site")
touch "$site/.nojekyll"

# ---- APT repository -------------------------------------------------------
cp "$debs"/*.deb "$site/apt/pool/main/"
cd "$site/apt"
archs=$(for f in pool/main/*.deb; do dpkg-deb -f "$f" Architecture; done | sort -u | tr '\n' ' ')
for arch in $archs; do
	d=dists/stable/main/binary-$arch
	mkdir -p "$d"
	dpkg-scanpackages --multiversion --arch "$arch" pool/ /dev/null >"$d/Packages" 2>/dev/null
	gzip -9kn "$d/Packages"
done

hashes() { # $1 = hash command
	find dists/stable -type f -name 'Packages*' | sort | while read -r f; do
		printf ' %s %s %s\n' "$($1 "$f" | cut -d' ' -f1)" "$(stat -c%s "$f")" "${f#dists/stable/}"
	done
}
{
	echo "Origin: proctab"
	echo "Label: proctab"
	echo "Suite: stable"
	echo "Codename: stable"
	echo "Architectures: ${archs% }"
	echo "Components: main"
	echo "Description: proctab APT repository"
	echo "Date: $(LC_ALL=C date -Ru)"
	echo "MD5Sum:"
	hashes md5sum
	echo "SHA256:"
	hashes sha256sum
} >dists/stable/Release

if [ -n "$key" ]; then
	gpg --batch --yes --local-user "$key" --armor --detach-sign -o dists/stable/Release.gpg dists/stable/Release
	gpg --batch --yes --local-user "$key" --clearsign -o dists/stable/InRelease dists/stable/Release
	gpg --batch --yes --armor --export "$key" >"$site/proctab.asc"
else
	echo "warning: GPG_KEY_ID not set, APT repo is unsigned" >&2
fi

# ---- pip index (PEP 503) ----------------------------------------------------
cd "$site/simple"
cp "$wheels"/*.whl proctab/
{
	echo '<!DOCTYPE html><html><body>'
	for w in proctab/*.whl; do
		f=$(basename "$w")
		echo "<a href=\"$f#sha256=$(sha256sum "$w" | cut -d' ' -f1)\" data-requires-python=\"&gt;=3.7\">$f</a><br>"
	done
	echo '</body></html>'
} >proctab/index.html
echo '<!DOCTYPE html><html><body><a href="proctab/">proctab</a></body></html>' >index.html

# ---- landing page -------------------------------------------------------------
cat >"$site/index.html" <<EOF
<!DOCTYPE html>
<html><head><meta charset="utf-8"><title>proctab packages</title>
<style>body{font-family:monospace;max-width:52em;margin:2em auto;padding:0 1em}pre{background:#111;color:#ddd;padding:1em;overflow-x:auto}</style>
</head><body>
<h1>proctab</h1>
<p>Lightweight htop-style terminal monitor for processes, ports, Docker and system info.
Source: <a href="https://github.com/human-stuff/proctab">github.com/human-stuff/proctab</a></p>
<h2>apt (Debian / Ubuntu)</h2>
<pre>sudo mkdir -p /etc/apt/keyrings
sudo curl -fsSL $url/proctab.asc -o /etc/apt/keyrings/proctab.asc
echo "deb [signed-by=/etc/apt/keyrings/proctab.asc] $url/apt stable main" | sudo tee /etc/apt/sources.list.d/proctab.list
sudo apt update
sudo apt install proctab</pre>
<h2>pip / pipx</h2>
<pre>pipx install proctab --index-url $url/simple/
# or
pip install proctab --index-url $url/simple/</pre>
</body></html>
EOF
echo "site built in $site (archs: $archs)"
