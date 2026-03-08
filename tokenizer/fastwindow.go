package tokenizer

import "math"

/*
FastWindow replaces container/ring with an O(1) array-backed sliding window.
It includes anti-drift recalibration to prevent floating-point errors over long streams.
*/
type FastWindow struct {
	data   []float64
	size   int
	count  int
	head   int
	sum    float64
	sumSq  float64
	drifts int
}

func NewFastWindow(size int) *FastWindow {
	return &FastWindow{
		data: make([]float64, size),
		size: size,
	}
}

func (window *FastWindow) Push(val float64) {
	if window.count == window.size {
		old := window.data[window.head]
		window.sum -= old
		window.sumSq -= old * old
	} else {
		window.count++
	}

	window.data[window.head] = val
	window.sum += val
	window.sumSq += val * val
	window.head = (window.head + 1) % window.size

	// Prevent floating-point drift over millions of bytes
	window.drifts++
	if window.drifts >= window.size*2 {
		window.recalc()
	}
}

func (window *FastWindow) recalc() {
	sum := 0.0
	sumSq := 0.0
	for idx := 0; idx < window.count; idx++ {
		entry := window.data[idx]
		sum += entry
		sumSq += entry * entry
	}
	window.sum = sum
	window.sumSq = sumSq
	window.drifts = 0
}

func (window *FastWindow) SimulatePush(val float64) (mean, stddev float64) {
	simSum := window.sum + val
	simSumSq := window.sumSq + (val * val)
	simCount := window.count

	if window.count == window.size {
		old := window.data[window.head]
		simSum -= old
		simSumSq -= old * old
	} else {
		simCount++
	}

	count := float64(simCount)
	mean = simSum / count
	if simCount < 2 {
		return mean, 0
	}

	variance := (simSumSq - (simSum*simSum)/count) / float64(simCount-1)
	if variance > 0 {
		stddev = math.Sqrt(variance)
	}
	return mean, stddev
}

func (window *FastWindow) Warmed() bool {
	return window.count >= window.size
}

func (window *FastWindow) Stats() (mean, stddev float64) {
	if window.count == 0 {
		return 0, 0
	}
	count := float64(window.count)
	mean = window.sum / count

	if window.count < 2 {
		return mean, 0
	}

	variance := (window.sumSq - (window.sum*window.sum)/count) / float64(window.count-1)
	if variance > 0 {
		stddev = math.Sqrt(variance)
	}
	return
}
