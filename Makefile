.PHONY: metal

metal:
	cd gpu/metal \
		&& xcrun -sdk macosx metal -std=metal3.1 -mmacosx-version-min=14.0 -c bitwise.metal -o bitwise.air \
		&& xcrun -sdk macosx metallib bitwise.air -o default.metallib \
		&& cd ../../