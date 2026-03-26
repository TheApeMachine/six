.PHONY: build metal cuda paper pprof pprof-mem dump capnp

DUMP_EXTS := -name '*.go' -o -name '*.yml' -o -name '*.cu' -o -name '*.h' -o -name '*.metal' -o -name '*.m' -o -name '*.capnp'
# Source extensions plus only visualizer/static/index.html (no other HTML).
# Drop any .md (README.md is injected separately so it is first and only markdown).
DUMP_FIND := find . -type f \( \( $(DUMP_EXTS) \) -o -path './visualizer/static/index.html' \) \
	| grep -v '/vendor/' \
	| grep -v '^\./experiment/' \
	| grep -v '^\./paper/' \
	| grep -v '_test\.go$$' \
	| grep -v '\.capnp\.go$$' \
	| awk '!/\.md$$/'
DUMP_FILE := repo.txt
# README.md first, then the rest sorted (do not sort README into the middle).
DUMP_LIST := { printf '%s\n' './README.md'; $(DUMP_FIND) | sort; }

dump:
	@echo "<<<TREE>>>" > $(DUMP_FILE)
	@$(DUMP_LIST) >> $(DUMP_FILE)
	@echo "<<<END>>>" >> $(DUMP_FILE)
	@$(DUMP_LIST) | while read f; do \
		echo "<<<FILE $$f>>>" >> $(DUMP_FILE); \
		cat "$$f" >> $(DUMP_FILE); \
		echo "" >> $(DUMP_FILE); \
		echo "<<<END>>>" >> $(DUMP_FILE); \
	done
	@echo "Dumped $$(grep -c '<<<FILE' $(DUMP_FILE)) files to $(DUMP_FILE)"

CAPNP_STD ?= ../../capnproto/go-capnp/std

build: capnp
	go generate ./pkg/primitive/...
	
	cd pkg/compute/kernel/metal \
		&& xcrun -sdk macosx metal -std=metal3.1 -mmacosx-version-min=14.0 -c backend.metal -o backend.air \
		&& xcrun -sdk macosx metallib backend.air -o backend.metallib
		
	cd pkg/compute/kernel/cuda \
		&& go generate

metal:
	go generate ./pkg/primitive/...
	cd pkg/compute/kernel/metal \
		&& xcrun -sdk macosx metal -std=metal3.1 -mmacosx-version-min=14.0 -c backend.metal -o backend.air \
		&& xcrun -sdk macosx metallib backend.air -o backend.metallib

cuda:
	go generate ./pkg/primitive/...
	cd pkg/compute/kernel/cuda \
		&& go generate

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

