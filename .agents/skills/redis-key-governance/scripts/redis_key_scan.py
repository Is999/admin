#!/usr/bin/env python3
import argparse
import os
import re
import sys

SKIP_DIRS = {".git", "vendor", "node_modules", "dist", "build", "bin", ".turbo", "coverage"}
ALLOWED_PARTS = (
    "/common/keys/",
    "/common/rediskeys/",
    "/data/keys/",
    "/internal/rediskeys/",
    "/pkg/rediskeys/",
)
STRING_RE = re.compile(r'(["`])([^"`]{2,})\1')


def is_allowed(path: str) -> bool:
    normalized = path.replace(os.sep, "/")
    return any(part in normalized for part in ALLOWED_PARTS)


def should_scan_line(line: str) -> bool:
    low = line.lower()
    markers = ("redis", "cache", "key", "fmt.sprintf", ".set(", ".get(", ".del(", ".scan(", ".keys(")
    return any(marker in low for marker in markers)


def walk_files(root: str):
    for dirpath, dirnames, filenames in os.walk(root):
        dirnames[:] = [name for name in dirnames if name not in SKIP_DIRS]
        for name in filenames:
            if name.endswith((".go", ".lua")):
                yield os.path.join(dirpath, name)


def scan_file(path: str):
    findings = []
    allowed = is_allowed(path)
    try:
        with open(path, "r", encoding="utf-8", errors="ignore") as handle:
            for lineno, line in enumerate(handle, 1):
                stripped = line.strip()
                if not stripped or stripped.startswith("//"):
                    continue
                if re.search(r"\.(Scan|Keys)\s*\(", line):
                    findings.append((path, lineno, "wildcard Redis scan/keys call needs explicit review"))
                if allowed or not should_scan_line(line):
                    continue
                for _, literal in STRING_RE.findall(line):
                    if ":" in literal or "*" in literal:
                        findings.append((path, lineno, f"possible inline Redis key literal {literal!r}"))
    except OSError as exc:
        findings.append((path, 1, str(exc)))
    return findings


def main() -> int:
    parser = argparse.ArgumentParser(description="Advisory Redis key governance scan.")
    parser.add_argument("root", nargs="?", default=".")
    parser.add_argument("--advisory-exit-zero", action="store_true")
    args = parser.parse_args()

    findings = []
    for path in walk_files(args.root):
        findings.extend(scan_file(path))

    for path, lineno, message in findings:
        print(f"{path}:{lineno}: {message}")

    if findings and not args.advisory_exit_zero:
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
