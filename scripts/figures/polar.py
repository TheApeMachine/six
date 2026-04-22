"""2x2 polar constraint figure.

Spec shape (from Go projector.PolarConstraintData):
    {
        "title":  str,
        "width":  int,        # CSS pixels at 96 DPI
        "height": int,
        "snapshots": [
            {
                "title":    str,
                "channels": [float, ...],     # angles in degrees
                "points": [
                    {"label": str, "angle": float, "radius": float,
                     "color": str (optional hex)},
                    ...
                ],
            },
            ... (up to 4)
        ]
    }

Each snapshot becomes one polar subplot in a 2x2 grid (or fewer if there
are <4 snapshots). Channels render as thin grey radial guidelines;
points render as labelled scatter dots.
"""

from __future__ import annotations

import math

import numpy as np

import style


SUSPECT_COLORS = ["#3b82f6", "#ec4899", "#8b5cf6", "#10b981"]
EVIDENCE_COLOR = "#1e293b"


def render(spec: dict, out_path: str) -> None:
    import matplotlib.pyplot as plt

    title = spec.get("title", "")
    snapshots = spec.get("snapshots", []) or []
    width_px = int(spec.get("width", 1200) or 1200)
    height_px = int(spec.get("height", 900) or 900)

    width_in = max(5.0, width_px / 96.0)
    height_in = max(4.0, height_px / 96.0)

    n = max(1, len(snapshots))
    cols = 2 if n > 1 else 1
    rows = max(1, math.ceil(n / cols))

    fig = plt.figure(figsize=(width_in, height_in))

    for idx, snap in enumerate(snapshots):
        ax = fig.add_subplot(rows, cols, idx + 1, projection="polar")
        _render_snapshot(ax, snap, idx)

    if title:
        fig.suptitle(title, fontsize=12, y=0.995)
        fig.subplots_adjust(top=0.92)

    fig.subplots_adjust(wspace=0.45, hspace=0.45)
    style.save(fig, out_path)


def _render_snapshot(ax, snap: dict, idx: int) -> None:
    ax.set_theta_zero_location("N")
    ax.set_theta_direction(-1)  # clockwise to mirror compass convention
    ax.set_ylim(0, 1.0)
    ax.set_yticks([0.25, 0.5, 0.75, 1.0])
    ax.set_yticklabels([f"{v:.2f}" for v in (0.25, 0.5, 0.75, 1.0)],
                       fontsize=7, color=style.MUTED_COLOR)
    ax.set_xticks(np.deg2rad(np.arange(0, 360, 45)))
    ax.set_xticklabels([f"{a}°" for a in range(0, 360, 45)],
                       fontsize=7, color=style.MUTED_COLOR)
    ax.grid(True, color=style.GRID_COLOR, linewidth=0.6, linestyle="--")
    ax.spines["polar"].set_color(style.AXIS_COLOR)
    ax.spines["polar"].set_linewidth(0.8)

    snap_title = snap.get("title", "")
    if snap_title:
        ax.set_title(
            f"({chr(65 + idx)})  {snap_title}",
            fontsize=10, pad=10, color=style.TEXT_COLOR,
        )

    # Channel guidelines.
    for angle_deg in snap.get("channels", []) or []:
        try:
            theta = math.radians(float(angle_deg))
        except (TypeError, ValueError):
            continue
        ax.plot([theta, theta], [0, 1], color="#94a3b8", linewidth=0.8, alpha=0.6)

    # Entity dots.
    points = snap.get("points", []) or []
    for p_idx, pt in enumerate(points):
        label = pt.get("label", "")
        try:
            theta = math.radians(float(pt.get("angle", 0.0)))
            radius = float(pt.get("radius", 0.0))
        except (TypeError, ValueError):
            continue
        is_special = label in ("EVIDENCE", "MID")
        color = pt.get("color") or (
            EVIDENCE_COLOR if is_special
            else SUSPECT_COLORS[p_idx % len(SUSPECT_COLORS)]
        )
        size = 70 if is_special else 90
        ax.scatter(
            [theta], [radius],
            s=size, color=color, edgecolors="white",
            linewidths=0.8, zorder=3,
        )
        if label:
            # Offset label radially outward, clamped inside the disk so it
            # doesn't get clipped by the spine.
            label_r = min(0.95, radius + 0.08)
            ax.text(
                theta, label_r, label,
                fontsize=8, color=color, fontweight="bold",
                ha="left", va="center",
            )
