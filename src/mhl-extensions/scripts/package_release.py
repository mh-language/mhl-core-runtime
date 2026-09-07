#!/usr/bin/env python3
"""Validate official archives, record a bundle, and smoke-test its HTTP assets.

Uses only the Python standard library. The Makefile owns the package/platform
lists; extension.mh owns package metadata. Only the official manifests' literal
metadata fields are read here; mhl validates their full syntax during installation.
"""

import argparse
import functools
import hashlib
import http.server
import json
import os
from pathlib import Path
import re
import subprocess
import tarfile
import threading


ROOT = Path(__file__).resolve().parents[1]
SEMVER = re.compile(
    r"(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)"
    r"(?:-((?:0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*)"
    r"(?:\.(?:0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*))*))?"
    r"(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?"
)


def check_tag(tag):
    if not tag.startswith("extensions-v") or not SEMVER.fullmatch(tag[12:]):
        raise ValueError("expected extensions-vMAJOR.MINOR.PATCH[-prerelease][+build]")


def output(*args, cwd=ROOT):
    return subprocess.check_output(args, cwd=cwd, text=True).strip()


def make_list(target, root=ROOT):
    return output("make", "-s", "--no-print-directory", target, cwd=root).split()


def sha256(path):
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def literal(text, field, separator):
    matches = re.findall(r'^\s*' + field + r'\s*' + separator + r'\s*"([^"\n]+)"', text, re.M)
    if len(matches) != 1:
        raise ValueError(f"expected exactly one literal {field}")
    return matches[0]


def inspect_archive(root, name, platforms):
    source = root / name
    manifest = (source / "extension.mh").read_text()
    main = (source / "main.go").read_text()
    metadata = {key: literal(manifest, key, ":") for key in ("id", "version", "api_version")}
    if not SEMVER.fullmatch(metadata["version"]):
        raise ValueError(f"{name}: invalid package version")
    for key, const in (("id", "extID"), ("version", "extVersion"), ("api_version", "apiVersion")):
        if metadata[key] != literal(main, const, "="):
            raise ValueError(f"{name}: manifest {key} differs from binary source")
    if literal(manifest, "executable", ":") != f"bin/{name}":
        raise ValueError(f"{name}: unexpected executable stem")

    binaries = {
        f"{name}/bin/{name}-{platform.replace('/', '-')}" +
        (".exe" if platform.startswith("windows/") else "")
        for platform in platforms
    }
    expected = binaries | {f"{name}/extension.mh", f"{name}/README.md"}
    archive = root / "dist/release" / f"{name}.tar.gz"
    if archive.stat().st_size > 256 * 1024 * 1024:
        raise ValueError(f"{name}: archive exceeds runtime download limit")
    with tarfile.open(archive, "r:gz") as tar:
        files = {}
        for member in tar:
            if member.isdir() and member.name in (name, f"{name}/bin"):
                continue
            if not member.isfile() or member.name not in expected or member.name in files:
                raise ValueError(f"{name}: unexpected archive entry {member.name}")
            if member.size <= 0 or member.size > 256 * 1024 * 1024:
                raise ValueError(f"{name}: invalid size for {member.name}")
            if member.name in binaries and not member.mode & 0o111:
                raise ValueError(f"{name}: binary is not executable: {member.name}")
            files[member.name] = member
        if files.keys() != expected:
            raise ValueError(f"{name}: incomplete archive; missing {sorted(expected - files.keys())}")
        for filename in ("extension.mh", "README.md"):
            if tar.extractfile(files[f"{name}/{filename}"]).read() != (source / filename).read_bytes():
                raise ValueError(f"{name}: packaged {filename} differs from source")
    return {"name": name, **metadata, "archive": archive.name, "sha256": sha256(archive)}


def packages(root=ROOT):
    platforms = make_list("platforms", root)
    return platforms, [inspect_archive(root, name, platforms) for name in make_list("list", root)]


def prepare(tag, runtime, root=ROOT):
    if tag != "snapshot":
        check_tag(tag)
    platforms, extensions = packages(root)
    commit = output("git", "rev-parse", "HEAD", cwd=root)
    data = {
        "schema_version": 1,
        "tag": tag,
        "source_commit": commit,
        "runtime": {"version": output(str(runtime), "version"), "commit": commit},
        "go_version": output("go", "version"),
        "platforms": platforms,
        "extensions": extensions,
    }
    dest = root / "dist/release"
    (dest / "release.json").write_text(json.dumps(data, indent=2) + "\n")
    assets = sorted([ext["archive"] for ext in extensions] + ["release.json"])
    (dest / "SHA256SUMS").write_text("".join(f"{sha256(dest / name)}  {name}\n" for name in assets))
    verify(tag, root)


def verify(tag=None, root=ROOT):
    platforms, extensions = packages(root)
    dest = root / "dist/release"
    assets = {ext["archive"] for ext in extensions} | {"release.json"}
    if {p.name for p in dest.iterdir()} != assets | {"SHA256SUMS"}:
        raise ValueError("release directory must contain exactly the official assets")
    checksums = {}
    for line in (dest / "SHA256SUMS").read_text().splitlines():
        match = re.fullmatch(r"([0-9a-f]{64})  ([A-Za-z0-9_.-]+)", line)
        if not match or match[2] in checksums:
            raise ValueError("malformed or duplicate checksum entry")
        checksums[match[2]] = match[1]
    if checksums.keys() != assets:
        raise ValueError("checksums must cover every official asset exactly once")
    for name, digest in checksums.items():
        if sha256(dest / name) != digest:
            raise ValueError(f"checksum mismatch: {name}")
    data = json.loads((dest / "release.json").read_text())
    if data["tag"] != "snapshot":
        check_tag(data["tag"])
    if tag is not None and data["tag"] != tag:
        raise ValueError("bundle tag mismatch")
    commit = output("git", "rev-parse", "HEAD", cwd=root)
    if data["source_commit"] != commit or data["runtime"]["commit"] != commit:
        raise ValueError("bundle was built from a different commit")
    if data["schema_version"] != 1 or data["platforms"] != platforms or data["extensions"] != extensions:
        raise ValueError("bundle metadata does not match packages")
    return data


def smoke(runtime, selected=None):
    data = verify()
    names = {ext["name"] for ext in data["extensions"]}
    if selected and selected not in names:
        raise ValueError(f"unknown extension: {selected}")
    handler = functools.partial(http.server.SimpleHTTPRequestHandler, directory=str(ROOT / "dist/release"))
    with http.server.ThreadingHTTPServer(("127.0.0.1", 0), handler) as server:
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        try:
            for ext in data["extensions"]:
                if selected and selected != ext["name"]:
                    continue
                url = f"http://127.0.0.1:{server.server_port}/{ext['archive']}#sha256={ext['sha256']}"
                env = dict(os.environ, MHL=str(runtime), EXTENSION_SOURCE=url)
                subprocess.run(["bash", str(ROOT / ext["name"] / "smoke.sh")], env=env, check=True, timeout=180)
        finally:
            server.shutdown()
            thread.join()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest="command", required=True)
    sub.add_parser("check-tag").add_argument("tag")
    prep = sub.add_parser("prepare")
    prep.add_argument("--tag", default="snapshot")
    prep.add_argument("--runtime", type=Path, required=True)
    sub.add_parser("verify").add_argument("--tag")
    test = sub.add_parser("smoke")
    test.add_argument("--runtime", type=Path, required=True)
    test.add_argument("--extension", help="Run one package locally; CI tests all packages")
    sub.add_parser("notes")
    args = parser.parse_args()
    if args.command == "check-tag":
        check_tag(args.tag)
    elif args.command == "prepare":
        prepare(args.tag, args.runtime.resolve())
    elif args.command == "verify":
        verify(args.tag)
    elif args.command == "smoke":
        smoke(args.runtime.resolve(), args.extension)
    else:
        data = verify()
        print(f"Official extension bundle `{data['tag']}`.\n")
        print(f"Tested with runtime `{data['runtime']['version']}` at commit `{data['source_commit']}`.\n")
        print("Platforms: " + ", ".join(data["platforms"]) + ".\n")
        print("| Package | Version | API |\n| --- | --- | --- |")
        for ext in data["extensions"]:
            print(f"| {ext['name']} | {ext['version']} | {ext['api_version']} |")
        print("\nInstall a .tar.gz release asset with `mhl extension install <asset-url>#sha256=<SHA256SUMS entry>`.")
        print("The runtime selects the host binary. See release.json for package versions and hashes.")


if __name__ == "__main__":
    main()
