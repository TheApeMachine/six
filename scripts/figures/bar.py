"""Grouped bar chart.

Spec shape (from Go projector.BarChart):
    {
        "title":   str,
        "x_axis":  [str, ...],            # category labels
        "series":  [{"name": str, "data": [float, ...]}, ...]
    }

Renders one bar per (series, category) cell. Series share x positions and
fan out side-by-side via a per-series offset so multi-series comparisons
stay readable.
"""

from __future__ import annotations

import numpy as np

import style


def render(spec: dict, out_path: str) -> None:
    title = spec.get("title", "")
    x_axis = spec.get("x_axis", []) or []
    series = spec.get("series", []) or []

    fig, ax = style.figure(width_in=6.8, height_in=3.8)

    n_groups = len(x_axis)
    n_series = max(1, len(series))
    x = np.arange(n_groups, dtype=float)
    # Total width per group <= 0.85 so adjacent groups breathe.
    bar_width = 0.85 / n_series

    for s_idx, s in enumerate(series):
        data = list(s.get("data", []) or [])
        # Pad / truncate to category length so a short series doesn't
        # silently shift downstream bars.
        if len(data) < n_groups:
            data = data + [0.0] * (n_groups - len(data))
        elif len(data) > n_groups:
            data = data[:n_groups]
        offset = (s_idx - (n_series - 1) / 2.0) * bar_width
        ax.bar(
            x + offset,
            data,
            width=bar_width * 0.92,
            label=s.get("name", f"series{s_idx}"),
            color=style.color_for(s_idx),
            edgecolor="white",
            linewidth=0.4,
        )

    ax.set_xticks(x)
    ax.set_xticklabels(x_axis, rotation=20 if any(len(c) > 8 for c in x_axis) else 0,
                       ha="right" if any(len(c) > 8 for c in x_axis) else "center")
    ax.set_axisbelow(True)
    if title:
        ax.set_title(title)
    if n_series > 1:
        ax.legend(loc="best")

    style.save(fig, out_path)
