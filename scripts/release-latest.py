#!/usr/bin/env python3
"""Whether a stable tag may advance latest; caller holds the release concurrency group."""
import json
import os
import re
import subprocess
import sys


def version(tag):
    if not re.fullmatch(r"v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)", tag):
        raise ValueError(f"Invalid stable release tag: {tag}")
    return tuple(map(int, tag[1:].split(".")))


def main():
    candidate = version(sys.argv[1])
    pages = json.loads(subprocess.check_output([
        "gh", "api", "--paginate", "--slurp",
        f"repos/{os.environ['GITHUB_REPOSITORY']}/releases",
    ], text=True))
    published = [version(release["tag_name"]) for page in pages for release in page
                 if not release["draft"] and not release["prerelease"]]
    print("true" if candidate >= max(published, default=(0, 0, 0)) else "false")


if __name__ == "__main__":
    main()
