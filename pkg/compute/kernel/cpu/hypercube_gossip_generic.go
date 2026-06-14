//go:build amd64 || arm64

package cpu

// executeKernel is the asm-backed device ALU. It is intentionally a free
// function so the assembly TEXT directive can use the simple `·executeKernel`
// symbol form. The receiver is passed explicitly as the first argument
// to keep the call ergonomic from the orchestrator.
//
// stageBuf / stageCount remain in the ABI so old object layouts still link.
// Strict firmware does not stage peers; active recruitment uses in-frame
// SELECTED/reference tags, and child-target emit uses the Go kernel.
//
//go:noescape
func executeKernel(
	backend *Backend,
	ownerFrame *[128]uint64,
	ownerIdx uint64,
	community []*[128]uint64,
	communitySize uint64,
	dimCount uint64,
	stageBuf *[128]uint64,
	stageCount *uint64,
)
