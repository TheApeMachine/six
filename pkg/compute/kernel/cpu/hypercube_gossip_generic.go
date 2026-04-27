//go:build amd64 || arm64

package cpu

// executeKernel is the asm-backed device ALU. It is intentionally a free
// function so the assembly TEXT directive can use the simple `·executeKernel`
// symbol form. The receiver is passed explicitly as the first argument
// to keep the call ergonomic from the orchestrator.
//
//go:noescape
func executeKernel(
	backend *Backend,
	ownerFrame *[128]uint64,
	ownerIdx uint64,
	community []*[128]uint64,
	communitySize uint64,
	dimCount uint64,
)
