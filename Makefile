.PHONY: metal paper

metal:
	cd kernel/metal \
		&& xcrun -sdk macosx metal -std=metal3.1 -mmacosx-version-min=14.0 -c bitwise.metal -o bitwise.air \
		&& xcrun -sdk macosx metallib bitwise.air -o bitwise.metallib \
		&& cd ../../

paper:
	-go test ./experiment/task/...
	go run cmd/paper/main.go
	cd paper && pdflatex main.tex