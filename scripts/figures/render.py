"""Single entry point for paper figure rendering.

Reads a JSON spec on stdin, dispatches on the top-level "kind" field to
the matching per-kind renderer, and writes a PDF to the path given on
the command line:

    python3 render.py <out_path>

Spec envelope:
    {
        "kind": "bar" | "line" | "combo" | "heatmap" | "confusion" |
                "multipanel" | "imagestrip" | "polar",
        "spec": { ... per-kind spec ... }
    }

Exit code is 0 on success, 1 on failure (with a one-line error to stderr).
"""

from __future__ import annotations

import json
import os
import sys
import traceback


# Headless backend before pyplot import — never need a display.
os.environ.setdefault("MPLBACKEND", "pdf")

# Ensure matplotlib has a writable cache dir even on locked-down systems.
if not os.environ.get("MPLCONFIGDIR"):
    cache_dir = os.path.join(os.path.expanduser("~"), ".cache", "matplotlib-six")
    try:
        os.makedirs(cache_dir, exist_ok=True)
        os.environ["MPLCONFIGDIR"] = cache_dir
    except OSError:
        pass


import style  # noqa: E402

style.apply_paper_style()


def _load(name: str):
    """Lazy-import the per-kind module so the slowest renderers don't
    inflate startup cost for the common cases."""
    import importlib

    return importlib.import_module(name)


DISPATCH = {
    "bar":         lambda: _load("bar"),
    "line":        lambda: _load("line"),
    "combo":       lambda: _load("combo"),
    "heatmap":     lambda: _load("heatmap"),
    "confusion":   lambda: _load("confusion"),
    "multipanel":  lambda: _load("multipanel"),
    "imagestrip":  lambda: _load("imagestrip"),
    "polar":       lambda: _load("polar"),
}


def main(argv: list[str]) -> int:
    if len(argv) != 2:
        print("usage: render.py <out_path>", file=sys.stderr)
        return 1

    out_path = argv[1]

    try:
        envelope = json.load(sys.stdin)
    except json.JSONDecodeError as exc:
        print(f"render.py: invalid JSON on stdin: {exc}", file=sys.stderr)
        return 1

    kind = envelope.get("kind")
    if kind not in DISPATCH:
        print(f"render.py: unknown kind {kind!r}", file=sys.stderr)
        return 1

    spec = envelope.get("spec") or {}

    try:
        module = DISPATCH[kind]()
        module.render(spec, out_path)
    except Exception as exc:  # noqa: BLE001 — top-level guard.
        print(f"render.py: {kind} render failed: {exc}", file=sys.stderr)
        traceback.print_exc(file=sys.stderr)
        return 1

    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
