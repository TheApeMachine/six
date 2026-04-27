//go:build !amd64 && !arm64

package cpu

/*
executeKernel is the device ALU on architectures without an asm fast
path. It simply delegates to executeKernelGo so the truth-table and
predicate semantics live in one place.
*/
func executeKernel(
	backend *Backend,
	ownerFrame *[128]uint64,
	ownerIdx uint64,
	community []*[128]uint64,
	communitySize uint64,
	dimCount uint64,
) {
	backend.executeKernelGo(
		ownerFrame, ownerIdx, community, communitySize, dimCount,
	)
}
