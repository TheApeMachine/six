package viz

import (
	"fmt"
	"hash/fnv"
	"math"
	"strconv"
)

/*
Layout meta keys (viz_lx, viz_ly, viz_band) let the canvas cluster particles by
subsystem instead of inferring layout from ids alone. Values are normalized
[0,1] with origin top-left; the React layer maps them with margin insets.

Bands are nominal vertical anchors; scatterKey spreads horizontally and jitters
slightly vertically so stacks remain readable.
*/

const (
	metaVizLX    = "viz_lx"
	metaVizLY    = "viz_ly"
	metaVizBand  = "viz_band"
	metaVizStage = "viz_stage"
)

// Nominal vertical anchors (0 top of field, 1 bottom).
const (
	vizBandPrompt       = 0.07
	vizBandIngest       = 0.15
	vizBandTokenizerVal = 0.28
	vizBandTrie         = 0.36
	vizBandBeam         = 0.42
	vizBandCompute      = 0.50
	vizBandPool         = 0.56
	vizBandQueue        = 0.64
	vizBandOrchestrator = 0.76
	vizBandProgram      = 0.88
	vizBandDHT          = 0.22
)

func ensureMeta(ev *Event) {
	if ev.Meta == nil {
		ev.Meta = make(map[string]string)
	}
}

func scatter01(key string) float64 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(key))
	return float64(h.Sum32()) / float64(^uint32(0))
}

func layoutFromBand(band float64, scatterKey string) (lx, ly float64) {
	u := scatter01(scatterKey)
	w := scatter01(scatterKey + "|y")
	lx = 0.04 + 0.92*u
	jitter := (w - 0.5) * 0.07
	ly = band + jitter

	if ly < 0.02 {
		ly = 0.02
	}

	if ly > 0.98 {
		ly = 0.98
	}

	return lx, ly
}

func formatVizUnit(v float64) string {
	return strconv.FormatFloat(v, 'f', 5, 64)
}

/*
applyVizLayout writes viz_lx, viz_ly, viz_band, and viz_stage into ev.Meta.
band must be a nominal vertical anchor (use vizBand* constants).
*/
func applyVizLayout(ev *Event, stageName string, band float64, scatterKey string) {
	ensureMeta(ev)
	lx, ly := layoutFromBand(band, scatterKey)
	ev.Meta[metaVizLX] = formatVizUnit(lx)
	ev.Meta[metaVizLY] = formatVizUnit(ly)
	ev.Meta[metaVizBand] = formatVizUnit(band)
	ev.Meta[metaVizStage] = stageName
}

/*
applyVizLayoutQueue is like applyVizLayout but nudges the band downward slightly
as inflight work stacks so the queue lane shows load visually.
*/
func applyVizLayoutQueue(ev *Event, inflight int64, valueID uint64) {
	band := vizBandQueue + math.Min(0.09, float64(inflight)*0.0035)

	if band > 0.82 {
		band = 0.82
	}

	applyVizLayout(ev, "queue", band, strconv.FormatUint(valueID, 16))
}

func applyVizLayoutCommunity(ev *Event, communityID int, suffix string) {
	key := fmt.Sprintf("c%d|%s", communityID, suffix)
	applyVizLayout(ev, "orchestrator", vizBandOrchestrator, key)
}

func applyVizLayoutProgram(ev *Event, communityID int, program string, idHex string) {
	key := fmt.Sprintf("%d|%s|%s", communityID, program, idHex)
	applyVizLayout(ev, "program", vizBandProgram, key)
}
