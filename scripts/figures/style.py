"""Shared matplotlib styling for every paper figure.

Single source of truth for fonts, palette, sizes, and layout so every
figure that lands in paper/include/ reads as part of the same publication
rather than a grab-bag of dashboards. Keep this module dependency-free
beyond matplotlib + numpy.
"""

from __future__ import annotations

import matplotlib as mpl
import matplotlib.pyplot as plt


# Curated, perceptually-uniform-ish categorical palette. Index 0..7 are the
# default series colors; further indices recycle. Ordering chosen so the
# first three are visually distinct in print and accessible to most forms
# of color blindness.
PALETTE = [
    "#3b82f6",  # blue 500
    "#ef4444",  # red 500
    "#10b981",  # emerald 500
    "#f59e0b",  # amber 500
    "#8b5cf6",  # violet 500
    "#06b6d4",  # cyan 500
    "#ec4899",  # pink 500
    "#64748b",  # slate 500
]

# Heatmap colormaps by name. The keys mirror the strings the Go layer
# already passes ("viridis", "magma", "plasma"); anything else falls back
# to viridis.
HEATMAP_CMAPS = {
    "viridis": "viridis",
    "magma": "magma",
    "plasma": "plasma",
    "cividis": "cividis",
    "inferno": "inferno",
}

# Confusion matrices read better with a sequential blue ramp by convention.
CONFUSION_CMAP = "Blues"

# Grid color (light, low-contrast so the data dominates).
GRID_COLOR = "#e2e8f0"
AXIS_COLOR = "#475569"
TEXT_COLOR = "#0f172a"
MUTED_COLOR = "#64748b"


def apply_paper_style() -> None:
    """Apply the global rcParams used by every figure."""
    mpl.rcParams.update(
        {
            # Fonts: ship a serif fallback chain so the figures sit
            # cleanly inside a Computer-Modern paper without forcing
            # usetex (which would require a full LaTeX install at
            # render time).
            "font.family": "serif",
            "font.serif": [
                "DejaVu Serif",
                "STIXGeneral",
                "Bitstream Vera Serif",
                "Computer Modern Roman",
                "serif",
            ],
            "mathtext.fontset": "stix",
            "font.size": 10,
            "axes.titlesize": 11,
            "axes.labelsize": 10,
            "xtick.labelsize": 9,
            "ytick.labelsize": 9,
            "legend.fontsize": 9,
            "figure.titlesize": 12,
            # Layout / output.
            "figure.dpi": 150,
            "savefig.dpi": 200,
            "savefig.bbox": "tight",
            "savefig.pad_inches": 0.05,
            "pdf.fonttype": 42,  # TrueType, embeddable, selectable in Acrobat.
            "ps.fonttype": 42,
            # Axes / spines / grid.
            "axes.edgecolor": AXIS_COLOR,
            "axes.labelcolor": TEXT_COLOR,
            "axes.titlecolor": TEXT_COLOR,
            "axes.linewidth": 0.8,
            "axes.spines.top": False,
            "axes.spines.right": False,
            "axes.grid": True,
            "axes.grid.axis": "y",
            "grid.color": GRID_COLOR,
            "grid.linewidth": 0.6,
            "grid.linestyle": "-",
            "xtick.color": AXIS_COLOR,
            "ytick.color": AXIS_COLOR,
            "xtick.direction": "out",
            "ytick.direction": "out",
            # Legend.
            "legend.frameon": False,
            "legend.borderaxespad": 0.6,
            # Color cycle.
            "axes.prop_cycle": mpl.cycler(color=PALETTE),
        }
    )


def figure(width_in: float = 6.5, height_in: float = 4.0):
    """Create a figure with paper-friendly defaults.

    width_in defaults to a typical single-column-spread width (≈ 6.5 in
    at 300 dpi). Callers that need square / panel figures override.
    """
    fig, ax = plt.subplots(figsize=(width_in, height_in))
    return fig, ax


def color_for(index: int) -> str:
    return PALETTE[index % len(PALETTE)]


def cmap_for(name: str | None):
    """Resolve a string colormap name with a viridis fallback."""
    if not name:
        return mpl.colormaps["viridis"]
    return mpl.colormaps[HEATMAP_CMAPS.get(name.lower(), "viridis")]


def save(fig, out_path: str) -> None:
    """Save and close — every renderer should funnel through here so the
    pdf settings stay consistent.
    """
    fig.savefig(out_path, format="pdf")
    plt.close(fig)
