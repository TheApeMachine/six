.PHONY: build run test bench coverage metal cuda bridge paper pprof pprof-mem dump capnp figure-deps

# The pool package (pkg/pool/lib_runtime_linkage.go) uses go:linkname to
# reach runtime scheduling symbols (e.g. runtime.dropg, runtime.casgstatus).
# Starting with Go 1.26, plain `go test` / `go build` fails at link time with:
#   link: .../pkg/pool: invalid reference to runtime.dropg
#   link: .../pkg/pool: invalid reference to runtime.casgstatus
# Pass -checklinkname=0 so the linker allows those references (see `go help build`).
LDFLAGS := -ldflags='-checklinkname=0'

DUMP_EXTS := -name '*.go' -o -name '*.yml' -o -name '*.cu' -o -name '*.h' -o -name '*.metal' -o -name '*.m' -o -name '*.s'
# Source extensions plus only visualizer/static/index.html (no other HTML).
# Drop any .md (README.md is injected separately so it is first and only markdown).
DUMP_FIND := find . -type f \( \( $(DUMP_EXTS) \) -o -path './visualizer/static/index.html' \) \
	| grep -v '/vendor/' \
	| grep -v '/node_modules/' \
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

build:
	go generate ./pkg/primitive/...

	cd pkg/compute/kernel/metal \
		&& xcrun -sdk macosx metal -std=metal3.1 -mmacosx-version-min=14.0 -I. -c backend.metal -o backend.air \
		&& xcrun -sdk macosx metallib backend.air -o backend.metallib

	cd pkg/compute/kernel/cuda \
		&& go generate

	go build $(LDFLAGS) -o six .

run: build
	./six

# Always use this target (or the same $(LDFLAGS)) for tests that transitively
# import pkg/pool — e.g. pkg/compute. Raw `go test ./...` will not apply LDFLAGS.
#
# experiment/task/pipeline_test.go is behind //go:build exp_pipeline so the
# long-running TestPipeline suite does not run here (see make paper / pprof).
test:
	go test $(LDFLAGS) ./...

# Benchmarks only (-run='^$' matches no tests). Same $(LDFLAGS) as test for pkg/pool.
bench:
	go test $(LDFLAGS) '-run=^$$' -bench=. -benchmem ./...

coverage:
	go test $(LDFLAGS) -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out

metal:
	go generate ./pkg/primitive/...
	cd pkg/compute/kernel/metal \
		&& xcrun -sdk macosx metal -std=metal3.1 -mmacosx-version-min=14.0 -I. -c backend.metal -o backend.air \
		&& xcrun -sdk macosx metallib backend.air -o backend.metallib

cuda:
	go generate ./pkg/primitive/...
	cd pkg/compute/kernel/cuda \
		&& go generate

bridge:
	go run ${LDFLAGS} main.go bridge

# Install the Python dependencies the figure renderer needs (matplotlib + numpy).
# The pipeline shells out to `python3 scripts/figures/render.py` to produce
# every chart PDF. Run this once per environment; subsequent paper builds will
# pick up the cached install. Override PYTHON=/path/to/python3 to target a
# specific interpreter (e.g. a venv).
PYTHON ?= python3
figure-deps:
	$(PYTHON) -m pip install --upgrade -r scripts/figures/requirements.txt

# For the live 3D viz: open http://127.0.0.1:6600 before this finishes — the
# pipeline starts the viz server on the first experiment and replays history
# to late-connecting browsers (see experiment/task/pipeline.go).
# Languages pulls HuggingFace parquet shards: first resolve+download per subset
# can sit silently for minutes until logs show shard cached; cache is under $TMPDIR/six_hf_*.
# Figure rendering is delegated to matplotlib via scripts/figures/render.py;
# run `make figure-deps` once before `make paper` to install matplotlib + numpy.
paper:
	go test $(LDFLAGS) -tags=exp_pipeline -v ./experiment/task/
	go run main.go paper
	cd paper && pdflatex -interaction=nonstopmode main.tex
	cd paper && pdflatex -interaction=nonstopmode main.tex

# Run a single experiment and open its CPU profile.
# Usage: make pprof EXP=Text_Classification
EXP ?= Languages
pprof:
	go test $(LDFLAGS) -tags=exp_pipeline -v -run 'TestPipeline/$(EXP)' -timeout 30m ./experiment/task/
	go tool pprof -http=:6060 paper/profiles/$(shell echo $(EXP) | tr '[:upper:]' '[:lower:]' | tr ' ' '_')_cpu.pprof

# Same for the heap snapshot.
pprof-mem:
	go test $(LDFLAGS) -tags=exp_pipeline -v -run 'TestPipeline/$(EXP)' -timeout 30m ./experiment/task/
	go tool pprof -http=:6060 paper/profiles/$(shell echo $(EXP) | tr '[:upper:]' '[:lower:]' | tr ' ' '_')_mem.pprof

