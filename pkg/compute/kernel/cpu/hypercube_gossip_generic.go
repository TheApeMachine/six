//go:build amd64 || arm64

package cpu

// executeKernel is the asm-backed device ALU. It is intentionally a free
// function so the assembly TEXT directive can use the simple `·executeKernel`
// symbol form. The receiver is passed explicitly as the first argument
// to keep the call ergonomic from the orchestrator.
//
// stageBuf / stageCount give the kernel a place to record the indices
// of peers it stages. Asm writes peer indices into stageBuf[0..*stageCount)
// when stage(B) instructions fire; the orchestrator consumes those indices
// alongside spawned children (staging lanes are compute.Backend.Submit keyed).
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
