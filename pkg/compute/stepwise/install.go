package stepwise

import (
	"fmt"
)

/*
MaxEmbeddedDescriptorSlots returns how many descriptors may follow the header
word in the program band.
*/
func MaxEmbeddedDescriptorSlots() int {

	base := EmbeddedProgramBase()
	if base >= FrameWords {
		return 0
	}

	return FrameWords - base - 1
}

/*
InstallEmbedded writes PackEmbeddedHeader(len(descriptors)) at the program base,
copies descriptors to base+1…, and clears the rest of the frame tail.
*/
func InstallEmbedded(ctx *[FrameWords]uint64, descriptors []uint64) error {

	if ctx == nil {
		return fmt.Errorf("stepwise.InstallEmbedded: nil frame")
	}

	base := EmbeddedProgramBase()
	maxD := MaxEmbeddedDescriptorSlots()

	if len(descriptors) > maxD {
		return fmt.Errorf(
			"stepwise.InstallEmbedded: %d descriptors exceeds max %d",
			len(descriptors),
			maxD,
		)
	}

	if len(descriptors) > 65535 {
		return fmt.Errorf("stepwise.InstallEmbedded: descriptor count overflow")
	}

	ctx[base] = PackEmbeddedHeader(uint16(len(descriptors)))

	for i, d := range descriptors {
		ctx[base+1+i] = d
	}

	for clear := base + 1 + len(descriptors); clear < FrameWords; clear++ {
		ctx[clear] = 0
	}

	return nil
}
