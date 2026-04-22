"""Multi-panel figure (mix of heatmaps and line/bar panels).

Spec shape (from Go projector.MultiPanel):
    {
        "title":  str,
        "width":  int,         # CSS pixels at 96 DPI; converted to inches
        "height": int,
        "panels": [
            {
                "kind":  "heatmap" | "chart",
                "title": str,
                "x_labels":   [str, ...],
                "x_axis_name": str,
                "x_show":     bool,
                "y_labels":   [str, ...],     # heatmap only
                "y_axis_name": str,
                "heat_data":  [[xi, yi, val], ...],
                "heat_min":   float,
                "heat_max":   float,
                "cmap":       str,
                "series": [
                    {"name": str, "kind": "line"|"bar"|"dashed"|"dotted",
                     "symbol": str, "area": bool, "data": [float],
                     "color": str (optional hex)},
                    ...
                ],
                "y_min":  float|null,
                "y_max":  float|null
            },
            ...
        ]
    }

Panels lay out in a grid sized as close to square as possible. Heatmaps
get their own colorbar; chart panels share the global color cycle.
"""

from __future__ import annotations

import math

import numpy as np

import style


SYMBOL_TO_MARKER = {
    "": "o",
    "none": "",
    "circle": "o",
    "diamond": "D",
    "triangle": "^",
    "square": "s",
}

KIND_TO_LINESTYLE = {
    "line": "-",
    "dashed": "--",
    "dotted": ":",
}


def render(spec: dict, out_path: str) -> None:
    title = spec.get("title", "")
    panels = spec.get("panels", []) or []
    width_px = int(spec.get("width", 1200) or 1200)
    height_px = int(spec.get("height", 800) or 800)

    # CSS pixels at 96 DPI → inches.
    width_in = max(4.0, width_px / 96.0)
    height_in = max(3.0, height_px / 96.0)

    n = len(panels)
    if n == 0:
        fig, ax = style.figure(width_in, height_in)
        ax.text(0.5, 0.5, "no panels", ha="center", va="center")
        ax.axis("off")
        style.save(fig, out_path)
        return

    # Grid sizing: cols ≈ ceil(sqrt(n)) with rows filling.
    cols = max(1, int(math.ceil(math.sqrt(n))))
    rows = max(1, int(math.ceil(n / cols)))

    fig, axes = _make_grid(width_in, height_in, rows, cols)

    for idx, panel in enumerate(panels):
        ax = axes[idx]
        kind = (panel.get("kind") or "chart").lower()
        if kind == "heatmap":
            _render_heatmap_panel(fig, ax, panel)
        else:
            _render_chart_panel(ax, panel)

    # Hide unused axes.
    for k in range(n, rows * cols):
        axes[k].axis("off")

    if title:
        fig.suptitle(title, fontsize=12, y=0.995)
        fig.subplots_adjust(top=0.94)

    style.save(fig, out_path)


def _make_grid(width_in: float, height_in: float, rows: int, cols: int):
    import matplotlib.pyplot as plt

    fig, axarr = plt.subplots(
        rows, cols,
        figsize=(width_in, height_in),
        squeeze=False,
    )
    return fig, [axarr[r][c] for r in range(rows) for c in range(cols)]


def _render_heatmap_panel(fig, ax, panel: dict) -> None:
    x_labels = panel.get("x_labels", []) or []
    y_labels = panel.get("y_labels", []) or []
    triplets = panel.get("heat_data", []) or []
    v_min = float(panel.get("heat_min", 0.0) or 0.0)
    v_max = float(panel.get("heat_max", 1.0) or 1.0)
    cmap = style.cmap_for(panel.get("cmap"))

    nx = max(1, len(x_labels))
    ny = max(1, len(y_labels))

    grid = np.full((ny, nx), np.nan, dtype=float)
    for triple in triplets:
        if not isinstance(triple, (list, tuple)) or len(triple) < 3:
            continue
        try:
            xi = int(triple[0]); yi = int(triple[1]); v = float(triple[2])
        except (TypeError, ValueError):
            continue
        if 0 <= xi < nx and 0 <= yi < ny:
            grid[yi, xi] = v

    if v_max <= v_min:
        finite = grid[np.isfinite(grid)]
        if finite.size:
            v_min, v_max = float(finite.min()), float(finite.max())
            if v_max <= v_min:
                v_max = v_min + 1.0
        else:
            v_max = v_min + 1.0

    im = ax.imshow(
        grid, aspect="auto", cmap=cmap,
        vmin=v_min, vmax=v_max,
        origin="lower", interpolation="nearest",
    )

    ax.set_xticks(np.arange(nx))
    ax.set_xticklabels(x_labels, rotation=30, ha="right", fontsize=8)
    ax.set_yticks(np.arange(ny))
    ax.set_yticklabels(y_labels, fontsize=8)
    ax.grid(False)
    ax.tick_params(top=False, right=False)
    if panel.get("x_axis_name"):
        ax.set_xlabel(panel["x_axis_name"], fontsize=9)
    if panel.get("y_axis_name"):
        ax.set_ylabel(panel["y_axis_name"], fontsize=9)
    if panel.get("title"):
        ax.set_title(panel["title"], fontsize=10)

    cbar = fig.colorbar(im, ax=ax, fraction=0.046, pad=0.02)
    cbar.outline.set_visible(False)
    cbar.ax.tick_params(labelsize=7)


def _render_chart_panel(ax, panel: dict) -> None:
    x_labels = panel.get("x_labels", []) or []
    series = panel.get("series", []) or []
    y_min = panel.get("y_min")
    y_max = panel.get("y_max")

    # When x_labels is provided it dictates the group count (the visible
    # category axis), but unlabeled chart panels — common for trace /
    # timeseries data where the x axis is just sample index — must size
    # to the longest series instead. Falling back to max(1, len(x_labels))
    # there would collapse every series to a single point.
    if x_labels:
        n_groups = len(x_labels)
    else:
        n_groups = max(
            1,
            max((len(s.get("data", []) or []) for s in series), default=1),
        )
    x = np.arange(n_groups, dtype=float)

    bar_series = [s for s in series if (s.get("kind") or "line") == "bar"]
    n_bars = max(1, len(bar_series))
    bar_width = 0.7 / n_bars

    bar_idx = 0
    color_idx = 0
    for s in series:
        s_kind = (s.get("kind") or "line").lower()
        data = list(s.get("data", []) or [])
        if len(data) < n_groups:
            data = data + [np.nan] * (n_groups - len(data))
        elif len(data) > n_groups:
            data = data[:n_groups]
        color = s.get("color") or style.color_for(color_idx)
        color_idx += 1

        if s_kind == "bar":
            offset = (bar_idx - (n_bars - 1) / 2.0) * bar_width
            ax.bar(
                x + offset, data,
                width=bar_width * 0.92,
                label=s.get("name", ""),
                color=color,
                edgecolor="white", linewidth=0.4,
                zorder=2,
            )
            bar_idx += 1
        else:
            linestyle = KIND_TO_LINESTYLE.get(s_kind, "-")
            marker_key = (s.get("symbol") or "circle").lower()
            marker = SYMBOL_TO_MARKER.get(marker_key, "o")
            line, = ax.plot(
                x, data,
                label=s.get("name", ""),
                color=color,
                linestyle=linestyle,
                marker=marker if marker else None,
                markersize=3.5,
                linewidth=1.4,
                zorder=3,
            )
            if s.get("area"):
                ax.fill_between(x, 0, data, color=color, alpha=0.15, zorder=1)

    if panel.get("x_show", True):
        ax.set_xticks(x)
        ax.set_xticklabels(
            x_labels,
            rotation=20 if any(len(c) > 6 for c in x_labels) else 0,
            ha="right" if any(len(c) > 6 for c in x_labels) else "center",
            fontsize=8,
        )
    else:
        ax.set_xticks([])
    if panel.get("x_axis_name"):
        ax.set_xlabel(panel["x_axis_name"], fontsize=9)
    if panel.get("y_axis_name"):
        ax.set_ylabel(panel["y_axis_name"], fontsize=9)
    if y_min is not None and y_max is not None and y_max > y_min:
        ax.set_ylim(float(y_min), float(y_max))

    ax.grid(True, axis="y", alpha=0.45)
    ax.set_axisbelow(True)
    if panel.get("title"):
        ax.set_title(panel["title"], fontsize=10)
    if any(s.get("name") for s in series):
        ax.legend(loc="best", fontsize=8, ncol=min(3, len(series)))
