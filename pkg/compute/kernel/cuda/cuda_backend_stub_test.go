//go:build !cuda || !cgo

package cuda_test

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/compute/kernel/cuda"
)

func TestCUDABackend_Available(t *testing.T) {
	Convey("Given a stubbed CUDABackend", t, func() {
		backend := &cuda.CUDABackend{}

		Convey("It should probe NVML for GPU count", func() {
			n, _ := backend.Available()
			So(n, ShouldBeGreaterThanOrEqualTo, 0)
		})
	})
}

func BenchmarkCUDABackend_Available(b *testing.B) {
	backend := &cuda.CUDABackend{}

	for b.Loop() {
		backend.Available()
	}
}
