"""Combo chart: bars + lines on shared axes.

Spec shape (from Go projector.ComboChart):
    {
        "title":      str,
        "x_axis":     [str, ...],
        "x_name":     str,
        "y_name":     str,
        "y_min":      float,
        "y_max":      float,
        "series": [
            {
                "name":     str,
                "type":     "bar" | "line" | "dashed" | "dotted",
                "symbol":   "" | "circle" | "diamond" | "triangle",
                "barWidth": str,            # ignored, layout figured out per series
                "data":     [float, ...]
            },
            ...
        ]
    }

The mix-in semantics mirror the ECharts predecessor: bar series stack
side-by-side (their own x-offsets), line series ride on top with
markers / dash style mapped from the type+symbol fields.
"""

from __future__ import annotations

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

TYPE_TO_LINESTYLE = {
    "line": "-",
    "dashed": "--",
    "dotted": ":",
}


def render(spec: dict, out_path: str) -> None:
    title = spec.get("title", "")
    x_axis = spec.get("x_axis", []) or []
    x_name = spec.get("x_name", "") or ""
    y_name = spec.get("y_name", "") or ""
    y_min = float(spec.get("y_min", 0.0) or 0.0)
    y_max = float(spec.get("y_max", 0.0) or 0.0)
    series = spec.get("series", []) or []

    fig, ax = style.figure(width_in=7.0, height_in=4.0)

    n_groups = len(x_axis)
    x = np.arange(n_groups, dtype=float)

    bar_series = [s for s in series if (s.get("type") or "line") == "bar"]
    n_bars = max(1, len(bar_series))
    bar_width = 0.7 / n_bars

    bar_idx = 0
    color_idx = 0
    for s in series:
        s_type = (s.get("type") or "line").lower()
        data = list(s.get("data", []) or [])
        if len(data) < n_groups:
            data = data + [np.nan] * (n_groups - len(data))
        elif len(data) > n_groups:
            data = data[:n_groups]
        color = style.color_for(color_idx)
        color_idx += 1

        if s_type == "bar":
            offset = (bar_idx - (n_bars - 1) / 2.0) * bar_width
            ax.bar(
                x + offset,
                data,
                width=bar_width * 0.92,
                label=s.get("name", "bar"),
                color=color,
                edgecolor="white",
                linewidth=0.4,
                zorder=2,
            )
            bar_idx += 1
        else:
            linestyle = TYPE_TO_LINESTYLE.get(s_type, "-")
            marker = SYMBOL_TO_MARKER.get(
                (s.get("symbol") or "circle").lower(), "o"
            )
            ax.plot(
                x,
                data,
                label=s.get("name", "line"),
                color=color,
                linestyle=linestyle,
                marker=marker if marker else None,
                markersize=4,
                linewidth=1.6,
                zorder=3,
            )

    ax.set_xticks(x)
    ax.set_xticklabels(
        x_axis,
        rotation=20 if any(len(c) > 8 for c in x_axis) else 0,
        ha="right" if any(len(c) > 8 for c in x_axis) else "center",
    )
    if x_name:
        ax.set_xlabel(x_name)
    if y_name:
        ax.set_ylabel(y_name)
    if y_max > y_min:
        ax.set_ylim(y_min, y_max)

    ax.grid(True, axis="y", alpha=0.5)
    ax.set_axisbelow(True)
    if title:
        ax.set_title(title)
    if len(series) > 1:
        ax.legend(loc="best", ncol=min(3, len(series)))

    style.save(fig, out_path)
