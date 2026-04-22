"""Confusion matrix.

Spec shape (from Go projector.ConfusionMatrix):
    {
        "title":      str,
        "labels":     [str, ...],
        "matrix":     [[int, ...], ...],   # matrix[true][predicted] = count
        "accuracy":   float,
        "macro_f1":   float,
        "resonance":  float
    }

Renders a row-normalised heatmap with overlaid raw counts and per-cell
percentages, plus a small badge in the upper-right with accuracy / F1 /
mean resonance.
"""

from __future__ import annotations

import numpy as np

import style


def render(spec: dict, out_path: str) -> None:
    title = spec.get("title", "")
    labels = spec.get("labels", []) or []
    matrix = spec.get("matrix", []) or []
    acc = float(spec.get("accuracy", 0.0) or 0.0)
    f1 = float(spec.get("macro_f1", 0.0) or 0.0)
    res = float(spec.get("resonance", 0.0) or 0.0)

    n = len(labels)
    if n == 0:
        # Empty matrix: write a placeholder pdf so callers don't crash.
        fig, ax = style.figure(5, 4)
        ax.text(0.5, 0.5, "no data", ha="center", va="center")
        ax.axis("off")
        style.save(fig, out_path)
        return

    M = np.zeros((n, n), dtype=float)
    for i in range(min(n, len(matrix))):
        row = matrix[i] or []
        for j in range(min(n, len(row))):
            try:
                M[i, j] = float(row[j])
            except (TypeError, ValueError):
                pass

    row_totals = M.sum(axis=1, keepdims=True)
    row_totals[row_totals == 0] = 1.0
    norm = M / row_totals

    side = max(4.5, min(9.0, 0.55 * n + 3.0))
    fig, ax = style.figure(width_in=side, height_in=side)

    im = ax.imshow(norm, cmap=style.CONFUSION_CMAP, vmin=0.0, vmax=1.0)

    ax.set_xticks(np.arange(n))
    ax.set_yticks(np.arange(n))
    ax.set_xticklabels(labels, rotation=30, ha="right")
    ax.set_yticklabels(labels)
    ax.set_xlabel("Predicted")
    ax.set_ylabel("True")
    ax.grid(False)
    ax.tick_params(top=False, right=False)

    # Per-cell text: count on top, percentage below in muted tone.
    for i in range(n):
        for j in range(n):
            cnt = int(M[i, j])
            pct = norm[i, j] * 100.0
            text_color = "white" if norm[i, j] > 0.55 else style.TEXT_COLOR
            muted = "#e0e7ff" if norm[i, j] > 0.55 else style.MUTED_COLOR
            ax.text(j, i - 0.12, f"{cnt:d}", ha="center", va="center",
                    fontsize=9, color=text_color)
            ax.text(j, i + 0.22, f"{pct:.1f}%", ha="center", va="center",
                    fontsize=7, color=muted)

    cbar = fig.colorbar(im, ax=ax, fraction=0.04, pad=0.02)
    cbar.outline.set_visible(False)
    cbar.ax.tick_params(labelsize=8)
    cbar.set_label("Row-normalised", fontsize=8)

    badge = f"acc {acc:.3f}    F1 {f1:.3f}    res {res:.3f}"
    fig.text(
        0.99, 0.985, badge,
        ha="right", va="top",
        fontsize=8, color=style.MUTED_COLOR,
        family="monospace",
    )

    if title:
        ax.set_title(title, pad=12)

    style.save(fig, out_path)
