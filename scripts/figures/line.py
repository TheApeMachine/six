"""Line chart with optional fixed Y-range.

Spec shape (from Go projector.LineChart):
    {
        "title":   str,
        "x_axis":  [str, ...],
        "series":  [{"name": str, "data": [float, ...]}, ...],
        "y_min":   float,
        "y_max":   float
    }

When y_min == y_max == 0 the y-range is auto-fitted (matches the Go-side
default sentinel).
"""

from __future__ import annotations

import numpy as np

import style


def render(spec: dict, out_path: str) -> None:
    title = spec.get("title", "")
    x_axis = spec.get("x_axis", []) or []
    series = spec.get("series", []) or []
    y_min = float(spec.get("y_min", 0.0) or 0.0)
    y_max = float(spec.get("y_max", 0.0) or 0.0)

    fig, ax = style.figure(width_in=6.8, height_in=3.8)

    x_labels = [str(v) for v in x_axis]
    # If x labels look numeric, plot against parsed floats so the spacing
    # is meaningful; otherwise fall back to integer slots and label later.
    numeric_x = []
    use_numeric = True
    for v in x_labels:
        try:
            numeric_x.append(float(v))
        except (TypeError, ValueError):
            use_numeric = False
            break
    x_vals = np.array(numeric_x) if use_numeric and numeric_x else np.arange(len(x_labels))

    for s_idx, s in enumerate(series):
        data = list(s.get("data", []) or [])
        if len(data) < len(x_vals):
            data = data + [np.nan] * (len(x_vals) - len(data))
        elif len(data) > len(x_vals):
            data = data[: len(x_vals)]
        ax.plot(
            x_vals,
            data,
            label=s.get("name", f"series{s_idx}"),
            color=style.color_for(s_idx),
            marker="o",
            markersize=3.5,
            linewidth=1.5,
        )

    if not use_numeric:
        ax.set_xticks(np.arange(len(x_labels)))
        ax.set_xticklabels(
            x_labels,
            rotation=20 if any(len(c) > 8 for c in x_labels) else 0,
            ha="right" if any(len(c) > 8 for c in x_labels) else "center",
        )

    if y_max > y_min:
        ax.set_ylim(y_min, y_max)

    ax.grid(True, axis="both", alpha=0.5)
    ax.set_axisbelow(True)
    if title:
        ax.set_title(title)
    if len(series) > 1:
        ax.legend(loc="best")

    style.save(fig, out_path)
