//go:build !amd64 && !arm64

package cpu

/*
executeKernel is the device ALU on architectures without an asm fast
path. It delegates to executeKernelGo and copies any staged indices
into the caller's buffer so the orchestrator's contract stays uniform
across architectures.
*/
func executeKernel(
	backend *Backend,
	ownerFrame *[128]uint64,
	ownerIdx uint64,
	community []*[128]uint64,
	communitySize uint64,
	dimCount uint64,
	stageBuf *[128]uint64,
	stageCount *uint64,
) {
	stagedIdx, _, _ := backend.executeKernelGo(
		ownerFrame, ownerIdx, community, communitySize, dimCount,
	)

	if stageBuf == nil || stageCount == nil {
		return
	}

	limit := uint64(len(stagedIdx))
	if limit > uint64(len(stageBuf)) {
		limit = uint64(len(stageBuf))
	}

	for idx := uint64(0); idx < limit; idx++ {
		stageBuf[idx] = stagedIdx[idx]
	}
	*stageCount = limit
}
