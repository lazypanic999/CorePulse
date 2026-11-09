#!/usr/bin/env python3
"""Execute a trusted release archive on its matching native runner before publish."""
import argparse
import os
from pathlib import Path
import platform
import subprocess
import sys
import tarfile
import tempfile
import zipfile


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--archive', type=Path, required=True)
    parser.add_argument('--version', required=True)
    parser.add_argument('--goos', choices=('linux', 'darwin', 'windows'), required=True)
    parser.add_argument('--goarch', choices=('amd64', 'arm64'), required=True)
    args = parser.parse_args()
    host_os = {'linux': 'linux', 'darwin': 'darwin', 'win32': 'windows'}.get(sys.platform)
    host_arch = {'x86_64': 'amd64', 'amd64': 'amd64', 'aarch64': 'arm64', 'arm64': 'arm64'}.get(platform.machine().lower())
    if (host_os, host_arch) != (args.goos, args.goarch):
        raise SystemExit(f'Archive needs {args.goos}/{args.goarch}; runner is {host_os}/{host_arch}')
    binary_name = 'corepulse.exe' if os.name == 'nt' else 'corepulse'
    expected = {binary_name, 'LICENSE', 'THIRD_PARTY_NOTICES.md'}
    with tempfile.TemporaryDirectory(prefix='corepulse-release-') as tmp:
        root = Path(tmp)
        if args.archive.suffix == '.zip':
            with zipfile.ZipFile(args.archive) as archive:
                names = archive.namelist()
                if len(names) != len(expected) or set(names) != expected:
                    raise SystemExit('Unexpected release ZIP members')
                for name in names:
                    (root / name).write_bytes(archive.read(name))
        else:
            with tarfile.open(args.archive, 'r:gz') as archive:
                members = archive.getmembers()
                if len(members) != len(expected) or {m.name for m in members} != expected or not all(m.isfile() for m in members):
                    raise SystemExit('Unexpected release tar members')
                for member in members:
                    with archive.extractfile(member) as source:
                        (root / member.name).write_bytes(source.read())
        binary = root / binary_name
        binary.chmod(0o755)
        actual = subprocess.check_output([str(binary), 'version'], text=True).strip()
        if actual != args.version:
            raise SystemExit(f'Wrong packaged version: expected {args.version}, got {actual}')
        repository = Path(__file__).resolve().parent.parent
        subprocess.run([sys.executable, str(repository / 'scripts/check-offline-example.py'), '--binary', str(binary)], cwd=repository, check=True)
    print(f'Native release archive passed: {args.goos}/{args.goarch} {args.version}')


if __name__ == '__main__':
    main()
