#!/usr/bin/env python3
"""Build pip wheels that ship the static proctab binary (stdlib only).

The binary goes into <name>-<ver>.data/scripts/, so pip installs it
straight into the environment's bin/ directory with the exec bit set,
the same way ruff and other binary-only packages do it.

usage: build_wheels.py --version v0.1.0 --dist dist/ --out wheels/
  --dist is a goreleaser dist/ dir (or any dir) containing the release
  archives proctab_<ver>_linux_<arch>.tar.gz
"""
import argparse
import base64
import hashlib
import io
import pathlib
import re
import sys
import tarfile
import zipfile

NAME = "proctab"
SUMMARY = "Lightweight htop-style terminal monitor for processes, ports, Docker and system info"
URL = "https://github.com/human-stuff/proctab"

# goreleaser archive arch -> wheel platform tags (static binary: any libc)
PLATFORMS = {
    "amd64": "manylinux_2_17_x86_64.manylinux2014_x86_64.musllinux_1_1_x86_64",
    "arm64": "manylinux_2_17_aarch64.manylinux2014_aarch64.musllinux_1_1_aarch64",
    "armv7": "manylinux_2_17_armv7l.manylinux2014_armv7l.musllinux_1_1_armv7l",
    "386": "manylinux_2_17_i686.manylinux2014_i686.musllinux_1_1_i686",
    "ppc64le": "manylinux_2_17_ppc64le.manylinux2014_ppc64le.musllinux_1_1_ppc64le",
    "s390x": "manylinux_2_17_s390x.manylinux2014_s390x.musllinux_1_1_s390x",
    "riscv64": "manylinux_2_31_riscv64.musllinux_1_1_riscv64",
}


def pep440(tag: str) -> str:
    """v0.2.0-rc1 -> 0.2.0rc1, v1.0.0 -> 1.0.0, 0.1.1-next -> 0.1.1.dev0"""
    v = tag.lstrip("v")
    m = re.fullmatch(r"(\d+(?:\.\d+)*)(?:-?(a|alpha|b|beta|rc)\.?(\d+))?(-next|-SNAPSHOT.*)?", v)
    if not m:
        sys.exit(f"cannot map version {tag!r} to PEP 440")
    base, pre, num, snap = m.groups()
    pre = {"alpha": "a", "beta": "b"}.get(pre, pre)
    out = base + (f"{pre}{num}" if pre else "")
    if snap:
        out += ".dev0"
    return out


def record_line(path: str, data: bytes) -> str:
    digest = base64.urlsafe_b64encode(hashlib.sha256(data).digest()).rstrip(b"=").decode()
    return f"{path},sha256={digest},{len(data)}"


def build(binary: bytes, version: str, plat: str, out: pathlib.Path, readme: str) -> pathlib.Path:
    tag = f"py3-none-{plat}"
    dist_info = f"{NAME}-{version}.dist-info"
    data_dir = f"{NAME}-{version}.data"
    files = {
        f"{NAME}/__init__.py": f'"""{SUMMARY}. The binary is installed as `{NAME}` on PATH."""\n__version__ = "{version}"\n'.encode(),
        f"{data_dir}/scripts/{NAME}": binary,
        f"{dist_info}/METADATA": (
            "Metadata-Version: 2.1\n"
            f"Name: {NAME}\nVersion: {version}\nSummary: {SUMMARY}\n"
            f"Home-page: {URL}\nLicense: MIT\nRequires-Python: >=3.7\n"
            "Classifier: Environment :: Console :: Curses\n"
            "Classifier: License :: OSI Approved :: MIT License\n"
            "Classifier: Operating System :: POSIX :: Linux\n"
            "Classifier: Topic :: System :: Monitoring\n"
            f"Project-URL: Source, {URL}\n"
            "Description-Content-Type: text/markdown\n\n" + readme
        ).encode(),
        f"{dist_info}/WHEEL": (
            "Wheel-Version: 1.0\nGenerator: proctab build_wheels.py\nRoot-Is-Purelib: false\n"
            + "".join(f"Tag: py3-none-{p}\n" for p in plat.split("."))
        ).encode(),
    }
    record = [record_line(p, d) for p, d in files.items()]
    record.append(f"{dist_info}/RECORD,,")
    files[f"{dist_info}/RECORD"] = ("\n".join(record) + "\n").encode()

    path = out / f"{NAME}-{version}-{tag}.whl"
    with zipfile.ZipFile(path, "w", zipfile.ZIP_DEFLATED) as z:
        for name, data in files.items():
            info = zipfile.ZipInfo(name, date_time=(1980, 1, 1, 0, 0, 0))
            mode = 0o755 if name.endswith(f"scripts/{NAME}") else 0o644
            info.external_attr = (0o100000 | mode) << 16
            info.compress_type = zipfile.ZIP_DEFLATED
            z.writestr(info, data)
    return path


def main() -> None:
    ap = argparse.ArgumentParser()
    ap.add_argument("--version", required=True, help="git tag, e.g. v0.1.0")
    ap.add_argument("--dist", required=True, type=pathlib.Path)
    ap.add_argument("--out", required=True, type=pathlib.Path)
    ap.add_argument("--readme", type=pathlib.Path, default=pathlib.Path("README.md"))
    a = ap.parse_args()

    version = pep440(a.version)
    a.out.mkdir(parents=True, exist_ok=True)
    readme = a.readme.read_text() if a.readme.exists() else SUMMARY
    built = 0
    for arch, plat in PLATFORMS.items():
        archives = sorted(a.dist.glob(f"{NAME}_*_linux_{arch}.tar.gz"))
        if not archives:
            print(f"skip {arch}: no archive", file=sys.stderr)
            continue
        with tarfile.open(archives[-1]) as t:
            member = next(m for m in t.getmembers() if pathlib.PurePath(m.name).name == NAME and m.isfile())
            binary = t.extractfile(member).read()
        print(build(binary, version, plat, a.out, readme))
        built += 1
    if not built:
        sys.exit("no wheels built")


if __name__ == "__main__":
    main()
