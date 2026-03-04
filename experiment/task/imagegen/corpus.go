package imagegen

func visionCorpus() ([][]byte, []string) {
	var images [][]byte
	var names []string

	// checkerboard
	checker := make([]byte, 256)
	for i := range checker {
		if (i/16+i%16)%2 == 0 {
			checker[i] = 255
		} else {
			checker[i] = 0
		}
	}
	images = append(images, checker)
	names = append(names, "Checkerboard")

	// horizontal stripes
	hStripes := make([]byte, 256)
	for i := range hStripes {
		if (i/16)%2 == 0 {
			hStripes[i] = 255
		} else {
			hStripes[i] = 0
		}
	}
	images = append(images, hStripes)
	names = append(names, "Horizontal Stripes")

	// vertical stripes
	vStripes := make([]byte, 256)
	for i := range vStripes {
		if (i%16)%2 == 0 {
			vStripes[i] = 255
		} else {
			vStripes[i] = 0
		}
	}
	images = append(images, vStripes)
	names = append(names, "Vertical Stripes")

	// gradient
	gradient := make([]byte, 256)
	for i := range gradient {
		gradient[i] = byte(i)
	}
	images = append(images, gradient)
	names = append(names, "Gradient")

	// cross
	cross := make([]byte, 256)
	for i := range cross {
		x, y := i%16, i/16
		if x == y || x == 15-y {
			cross[i] = 255
		} else {
			cross[i] = 0
		}
	}
	images = append(images, cross)
	names = append(names, "Cross")

	return images, names
}
