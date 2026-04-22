"""Heatmap.

Spec shape (from Go projector.HeatMap):
    {
        "title":   str,
        "x_axis":  [str, ...],
        "y_axis":  [str, ...],
        "data":    [[xi, yi, value], ...],     # sparse triplets
        "v_min":   float,
        "v_max":   float,
        "cmap":    str       # optional, default viridis
    }
"""

from __future__ import annotations

import numpy as np

import style


def render(spec: dict, out_path: str) -> None:
    title = spec.get("title", "")
    x_axis = spec.get("x_axis", []) or []
    y_axis = spec.get("y_axis", []) or []
    triplets = spec.get("data", []) or []
    v_min = float(spec.get("v_min", 0.0) or 0.0)
    v_max = float(spec.get("v_max", 1.0) or 1.0)
    cmap = style.cmap_for(spec.get("cmap"))

    nx = max(1, len(x_axis))
    ny = max(1, len(y_axis))

    grid = np.full((ny, nx), np.nan, dtype=float)
    for triple in triplets:
        if not isinstance(triple, (list, tuple)) or len(triple) < 3:
            continue
        xi, yi, val = triple[0], triple[1], triple[2]
        try:
            xi_i = int(xi)
            yi_i = int(yi)
            v = float(val)
        except (TypeError, ValueError):
            continue
        if 0 <= xi_i < nx and 0 <= yi_i < ny:
            grid[yi_i, xi_i] = v

    width = max(5.0, min(10.0, 0.45 * nx + 2.5))
    height = max(3.5, min(9.0, 0.45 * ny + 2.0))
    fig, ax = style.figure(width_in=width, height_in=height)

    if v_max <= v_min:
        finite = grid[np.isfinite(grid)]
        if finite.size:
            v_min, v_max = float(finite.min()), float(finite.max())
            if v_max <= v_min:
                v_max = v_min + 1.0
        else:
            v_max = v_min + 1.0

    im = ax.imshow(
        grid,
        aspect="auto",
        cmap=cmap,
        vmin=v_min,
        vmax=v_max,
        origin="lower",
        interpolation="nearest",
    )

    ax.set_xticks(np.arange(nx))
    ax.set_xticklabels(x_axis, rotation=30, ha="right")
    ax.set_yticks(np.arange(ny))
    ax.set_yticklabels(y_axis)
    ax.grid(False)
    ax.tick_params(top=False, right=False)

    cbar = fig.colorbar(im, ax=ax, fraction=0.04, pad=0.02)
    cbar.outline.set_visible(False)
    cbar.ax.tick_params(labelsize=8)

    if title:
        ax.set_title(title)

    style.save(fig, out_path)
