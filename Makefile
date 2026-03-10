.PHONY: metal cuda paper pprof pprof-mem

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
	go test -v ./experiment/task/
	go run main.go paper
	cd paper && pdflatex -interaction=nonstopmode main.tex
	cd paper && pdflatex -interaction=nonstopmode main.tex

# Run a single experiment and open its CPU profile.
# Usage: make pprof EXP=Text_Classification
EXP ?= Languages
pprof:
	go test -v -run 'TestPipeline/$(EXP)' -timeout 30m ./experiment/task/
	go tool pprof -http=:6060 paper/profiles/$(shell echo $(EXP) | tr '[:upper:]' '[:lower:]' | tr ' ' '_')_cpu.pprof

# Same for the heap snapshot.
pprof-mem:
	go test -v -run 'TestPipeline/$(EXP)' -timeout 30m ./experiment/task/
	go tool pprof -http=:6060 paper/profiles/$(shell echo $(EXP) | tr '[:upper:]' '[:lower:]' | tr ' ' '_')_mem.pprof