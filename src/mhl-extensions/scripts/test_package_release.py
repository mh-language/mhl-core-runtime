import io
import json
from pathlib import Path
import subprocess
import tarfile
import tempfile
import unittest
from unittest.mock import patch

import package_release as release


class ReleaseTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.name = "mhl-store-example"
        self.platforms = ["linux/amd64", "windows/arm64"]
        source = self.root / self.name
        source.mkdir()
        (source / "extension.mh").write_text(
            'extensible store {\n    manifest: {\n'
            '        id: "dev.mhl.example",\n        version: "1.2.3",\n'
            '        api_version: "1",\n        executable: "bin/mhl-store-example"\n    }\n}\n'
        )
        (source / "main.go").write_text(
            'const (\n extID = "dev.mhl.example"\n extVersion = "1.2.3"\n apiVersion = "1"\n)\n'
        )
        (source / "README.md").write_text("Example package\n")
        self.dest = self.root / "dist/release"
        self.dest.mkdir(parents=True)
        self.write_archive()
        self.lists = patch.object(release, "make_list", side_effect=lambda target, root: (
            self.platforms if target == "platforms" else [self.name]
        ))
        self.lists.start()
        self.addCleanup(self.lists.stop)
        self.commands = patch.object(release, "output", return_value="abc123")
        self.commands.start()
        self.addCleanup(self.commands.stop)
        release.prepare("extensions-v1.0.0", Path("/fake/mhl"), self.root)

    def write_archive(self, missing=None, extra=None, mode=0o755):
        files = {
            f"{self.name}/{filename}": ((self.root / self.name / filename).read_bytes(), 0o644)
            for filename in ("extension.mh", "README.md")
        }
        for platform in self.platforms:
            filename = f"{self.name}/bin/{self.name}-{platform.replace('/', '-')}"
            if platform.startswith("windows/"):
                filename += ".exe"
            files[filename] = (b"test binary", mode)
        if missing:
            del files[missing]
        if extra:
            files[extra] = (b"unexpected", 0o644)
        with tarfile.open(self.dest / f"{self.name}.tar.gz", "w:gz") as archive:
            for filename, (content, permissions) in files.items():
                member = tarfile.TarInfo(filename)
                member.size = len(content)
                member.mode = permissions
                archive.addfile(member, io.BytesIO(content))

    def verify(self):
        return release.verify("extensions-v1.0.0", self.root)

    def test_complete_bundle(self):
        self.assertEqual(self.verify()["extensions"][0]["version"], "1.2.3")

    def test_missing_checksum_is_rejected_even_when_archive_exists(self):
        path = self.dest / "SHA256SUMS"
        path.write_text(next(line for line in path.read_text().splitlines(True) if "release.json" in line))
        with self.assertRaisesRegex(ValueError, "every official asset"):
            self.verify()

    def test_duplicate_checksum_is_rejected(self):
        path = self.dest / "SHA256SUMS"
        path.write_text(path.read_text() * 2)
        with self.assertRaisesRegex(ValueError, "duplicate"):
            self.verify()

    def test_changed_asset_is_rejected(self):
        with (self.dest / f"{self.name}.tar.gz").open("ab") as stream:
            stream.write(b"changed")
        with self.assertRaisesRegex(ValueError, "checksum mismatch"):
            self.verify()

    def test_missing_platform_is_rejected(self):
        self.write_archive(missing=f"{self.name}/bin/{self.name}-windows-arm64.exe")
        with self.assertRaisesRegex(ValueError, "incomplete archive"):
            self.verify()

    def test_extra_archive_entry_is_rejected(self):
        self.write_archive(extra="../outside")
        with self.assertRaisesRegex(ValueError, "unexpected archive entry"):
            self.verify()

    def test_binary_requires_executable_permission(self):
        self.write_archive(mode=0o644)
        with self.assertRaisesRegex(ValueError, "not executable"):
            self.verify()

    def test_manifest_and_wire_version_must_match(self):
        path = self.root / self.name / "main.go"
        path.write_text(path.read_text().replace('"1.2.3"', '"1.2.4"'))
        with self.assertRaisesRegex(ValueError, "differs from binary source"):
            self.verify()

    def test_tag_and_commit_must_match(self):
        with self.assertRaisesRegex(ValueError, "bundle tag mismatch"):
            release.verify("extensions-v2.0.0", self.root)
        self.commands.stop()
        with patch.object(release, "output", return_value="different-commit"):
            with self.assertRaisesRegex(ValueError, "different commit"):
                self.verify()

    def test_metadata_cannot_override_package_versions(self):
        path = self.dest / "release.json"
        data = json.loads(path.read_text())
        data["extensions"][0]["version"] = "9.0.0"
        path.write_text(json.dumps(data))
        checksums = self.dest / "SHA256SUMS"
        lines = checksums.read_text().splitlines()
        checksums.write_text("\n".join(
            f"{release.sha256(path)}  release.json" if line.endswith("  release.json") else line
            for line in lines
        ) + "\n")
        with self.assertRaisesRegex(ValueError, "metadata does not match"):
            self.verify()


class TagTests(unittest.TestCase):
    def test_tag_namespace_and_semver(self):
        for tag in ("extensions-v0.1.0", "extensions-v1.2.3-rc.1", "extensions-v1.2.3+build.4"):
            with self.subTest(tag=tag):
                release.check_tag(tag)
        for tag in ("v1.2.3", "main", "extensions-v01.2.3", "extensions-v1.2.3-01",
                    "extensions-v1.2.3;echo bad", "extensions-v1.2", "extensions-v1.2.3\n"):
            with self.subTest(tag=tag), self.assertRaises(ValueError):
                release.check_tag(tag)

    def test_runtime_version_ignores_extension_tags(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            makefile = release.ROOT.parent / "mhl-runtime/Makefile"
            (root / "Makefile").write_text(makefile.read_text() + '\nprint-version:\n\t@echo "$(VERSION)"\n')
            def git(*args):
                return subprocess.check_output(["git", *args], cwd=root, text=True, stderr=subprocess.DEVNULL)
            git("init")
            git("config", "user.name", "Release test")
            git("config", "user.email", "test@example.invalid")
            git("add", "Makefile")
            git("commit", "-m", "runtime")
            git("tag", "v1.2.3")
            git("commit", "--allow-empty", "-m", "extensions")
            git("tag", "extensions-v9.0.0")
            version = subprocess.check_output(["make", "-s", "print-version"], cwd=root, text=True).strip()
            self.assertRegex(version, r"^v1\.2\.3-1-g[0-9a-f]+$")


if __name__ == "__main__":
    unittest.main()
