"""Image strip: rows of (original, masked, reconstructed) base64 images.

Spec shape (from Go projector.ImageStrip):
    {
        "title":  str,
        "rows":   [
            {"original": b64, "masked": b64, "reconstructed": b64,
             "label": str},
            ...
        ]
    }
"""

from __future__ import annotations

import base64
import io

import numpy as np

import style


COLUMN_TITLES = ["Original", "Masked", "Reconstructed"]


def _decode_image(b64: str):
    if not b64:
        return None
    try:
        # Allow data: URIs as well as bare base64.
        if "," in b64 and b64.startswith("data:"):
            b64 = b64.split(",", 1)[1]
        raw = base64.b64decode(b64)
    except Exception:
        return None
    try:
        from matplotlib.image import imread
        return imread(io.BytesIO(raw))
    except Exception:
        return None


def render(spec: dict, out_path: str) -> None:
    import matplotlib.pyplot as plt

    title = spec.get("title", "")
    rows = spec.get("rows", []) or []

    n_rows = max(1, len(rows))
    n_cols = 3

    width_in = 6.6
    height_in = 1.0 + n_rows * 1.9

    fig, axarr = plt.subplots(
        n_rows, n_cols,
        figsize=(width_in, height_in),
        squeeze=False,
    )

    for r_idx, row in enumerate(rows):
        cells = [
            row.get("original"),
            row.get("masked"),
            row.get("reconstructed"),
        ]
        for c_idx, b64 in enumerate(cells):
            ax = axarr[r_idx][c_idx]
            img = _decode_image(b64)
            if img is None:
                ax.text(0.5, 0.5, "—", ha="center", va="center",
                        color=style.MUTED_COLOR, fontsize=14)
                ax.set_facecolor("#f8fafc")
            else:
                # Squeeze single-channel to 2D so imshow renders grayscale correctly.
                if hasattr(img, "ndim") and img.ndim == 3 and img.shape[-1] == 1:
                    img = img.squeeze(-1)
                cmap = "gray" if (hasattr(img, "ndim") and img.ndim == 2) else None
                ax.imshow(np.asarray(img), cmap=cmap, interpolation="nearest")

            ax.set_xticks([])
            ax.set_yticks([])
            for spine in ax.spines.values():
                spine.set_edgecolor(style.GRID_COLOR)
                spine.set_linewidth(0.6)

            if r_idx == 0:
                ax.set_title(COLUMN_TITLES[c_idx], fontsize=10, pad=4)

        label = row.get("label") or ""
        if label:
            axarr[r_idx][0].set_ylabel(
                label, fontsize=9, rotation=0, ha="right", va="center",
                labelpad=10, color=style.TEXT_COLOR,
            )

    if title:
        fig.suptitle(title, fontsize=12, y=0.995)
        fig.subplots_adjust(top=0.93)

    fig.subplots_adjust(wspace=0.04, hspace=0.06)
    style.save(fig, out_path)
