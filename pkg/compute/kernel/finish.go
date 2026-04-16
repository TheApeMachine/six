package kernel

/*
FinishFramePostALU runs refutation then TTL lifecycle on the frame after all
substrate ALU work has completed. Call once per Value per Execute pass.
*/
func FinishFramePostALU(frame *[128]uint64) {
	ApplyRefutationProbe(frame)
	ApplyPostExecutionLifecycle(frame)
}
