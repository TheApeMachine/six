.PHONY: build metal cuda paper pprof pprof-mem dump capnp

DUMP_EXTS := -name '*.go' -o -name '*.yml' -o -name '*.cu' -o -name '*.h' -o -name '*.metal' -o -name '*.m' -o -name '*.capnp'
# Source extensions plus only visualizer/static/index.html (no other HTML).
DUMP_FIND := find . -type f \( \( $(DUMP_EXTS) \) -o -path './visualizer/static/index.html' \) \
	| grep -v '/vendor/' \
	| grep -v '^\./experiment/' \
	| grep -v '^\./paper/' \
	| grep -v '_test\.go$$' \
	| grep -v '\.capnp\.go$$'
DUMP_FILE := repo.txt

dump:
	@echo "<<<TREE>>>" > $(DUMP_FILE)
	@$(DUMP_FIND) | sort >> $(DUMP_FILE)
	@echo "<<<END>>>" >> $(DUMP_FILE)
	@$(DUMP_FIND) | sort | while read f; do \
		echo "<<<FILE $$f>>>" >> $(DUMP_FILE); \
		cat "$$f" >> $(DUMP_FILE); \
		echo "" >> $(DUMP_FILE); \
		echo "<<<END>>>" >> $(DUMP_FILE); \
	done
	@echo "Dumped $$(grep -c '<<<FILE' $(DUMP_FILE)) files to $(DUMP_FILE)"

CAPNP_STD ?= ../../capnproto/go-capnp/std

capnp:
	capnp compile -I $(CAPNP_STD) -ogo pkg/store/dmt/server/server.capnp
	capnp compile -I $(CAPNP_STD) -ogo pkg/store/dmt/radix.capnp
	capnp compile -I $(CAPNP_STD) -ogo pkg/logic/substrate/graph.capnp
	capnp compile -I $(CAPNP_STD) -ogo pkg/system/process/tokenizer/universal.capnp
	capnp compile -I $(CAPNP_STD) -ogo pkg/system/vm/input/prompt.capnp
	capnp compile -I $(CAPNP_STD) -ogo pkg/logic/synthesis/bvp/cantilever.capnp
	capnp compile -I $(CAPNP_STD) -ogo pkg/logic/synthesis/macro/macro_index.capnp
	capnp compile -I $(CAPNP_STD) -ogo pkg/logic/lang/program.capnp
	capnp compile -I $(CAPNP_STD) -ogo pkg/logic/lang/primitive/value.capnp
	capnp compile -I $(CAPNP_STD) -ogo pkg/logic/synthesis/has.capnp
	capnp compile -I $(CAPNP_STD) -ogo pkg/logic/reader/head.capnp
	capnp compile -I $(CAPNP_STD) -ogo pkg/system/vm/processor/interpreter.capnp

build: capnp
	cd pkg/compute/kernel/metal \
		&& xcrun -sdk macosx metal -std=metal3.1 -mmacosx-version-min=14.0 -c resolver.metal -o resolver.air \
		&& xcrun -sdk macosx metallib resolver.air -o resolver.metallib
		
	cd pkg/compute/kernel/cuda \
		&& go generate

metal:
	cd pkg/compute/kernel/metal \
		&& xcrun -sdk macosx metal -std=metal3.1 -mmacosx-version-min=14.0 -c resolver.metal -o resolver.air \
		&& xcrun -sdk macosx metallib resolver.air -o resolver.metallib

cuda:
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

