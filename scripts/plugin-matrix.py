#!/usr/bin/env python3
"""Describe build inputs for runnable Go and Python plugins."""
import argparse
import json
from pathlib import Path
import sys


def describe(path):
    if (path / 'go.mod').is_file() and (path / 'main.go').is_file():
        return dict(plugin=path.name, runtime='go', context='.', dockerfile='Dockerfile')
    if ((path / 'manifest.yml').is_file() or (path / 'sokel.yaml').is_file()) and (path / 'main.py').is_file():
        if not (path / 'Dockerfile').is_file():
            raise ValueError(f'{path.name}: Python plugin requires its own Dockerfile')
        return dict(plugin=path.name, runtime='python', context=path.name, dockerfile=f'{path.name}/Dockerfile')
    return None


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--plugin', help='emit build step outputs for one release')
    args = parser.parse_args()
    entries = [entry for path in sorted(Path('.').iterdir()) if path.is_dir()
               if (entry := describe(path))]
    if args.plugin:
        entry = next((e for e in entries if e['plugin'] == args.plugin), None)
        if entry is None:
            sys.exit(f'no runnable plugin named {args.plugin!r}')
        for key, value in entry.items():
            print(f'{key}={value}')
    else:
        print(json.dumps({'include': entries}, separators=(',', ':')))


if __name__ == '__main__':
    main()
