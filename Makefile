.PHONY: metal cuda paper

metal:
	cd kernel/metal \
		&& xcrun -sdk macosx metal -std=metal3.1 -mmacosx-version-min=14.0 -c resolver.metal -o resolver.air \
		&& xcrun -sdk macosx metallib resolver.air -o resolver.metallib \
		&& cd ../../

cuda:
	cd kernel/cuda \
		&& go generate \
		&& cd ../../

paper:
	-go test ./experiment/task/...
	go run main.go paper
	cd paper && pdflatex main.tex