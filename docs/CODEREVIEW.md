Fix the following issues. The issues can be from different files or can overlap on same lines in one file.

- Verify each finding against the current code and only fix it if needed.

In @README.md around lines 61 - 65, Update the fenced code blocks in README.md so they include language specifiers: change the doctrine block containing "The LSM is address space, not intelligence..." to use ```text, change all ASCII diagrams (lines noted in the review) to ```text, change the bit layout diagram to ```text, and use ```c or ```text for the reduction example; locate these blocks by searching for the exact snippet "The LSM is address space, not intelligence." and the other ASCII/diagram sections mentioned and add the appropriate language token immediately after the opening triple backticks.

- Verify each finding against the current code and only fix it if needed.

In @docs/branching.puml around lines 45 - 47, The swimlane declaration "Control Flow (The Fermat Sentinel)" is empty and causes a visual artifact; either add meaningful content (e.g., a step or note) under the "Control Flow (The Fermat Sentinel)" swimlane so it isn’t empty, or remove that swimlane declaration entirely, ensuring the subsequent "Mathematical Logic (GF(257))" swimlane and the repeat while line remain correctly ordered; look for the exact swimlane label "Control Flow (The Fermat Sentinel)" and the following "Mathematical Logic (GF(257))" and adjust accordingly.

- Verify each finding against the current code and only fix it if needed.

In @docs/chord around lines 1 - 34, The file docs/chord is missing the .puml extension which breaks consistency with other PlantUML files; rename the file to docs/chord.puml and update any references to it (look for usages of "docs/chord" and the PlantUML block starting with @startuml and the class "AVX-512 Register" / ChordType) so IDE tooling, syntax highlighting, and PlantUML integrations recognize it.

- Verify each finding against the current code and only fix it if needed.

In @experiment/evaluator.go around lines 100 - 106, The loop that builds found uses strings.Contains(generated, evaluator.labels[classIdx]) which can produce false positives when one label is a substring of another; update the matching in the block that iterates classIdx over numClasses to use exact/token-boundary matching (e.g., split/generated tokenization or a regexp using regexp.QuoteMeta and word boundaries) against evaluator.labels[classIdx] instead of strings.Contains so labels are matched precisely; ensure you reference the same variables (generated, evaluator.labels, classIdx, found) and handle any escaping for special characters in labels.

- Verify each finding against the current code and only fix it if needed.

In @experiment/evaluator.go around lines 202 - 223, The ClassificationMetrics struct declares MeanScore but Metrics (the function building and returning ClassificationMetrics) never sets it; set MeanScore when constructing the returned ClassificationMetrics — e.g., populate MeanScore with the computed macroF1 (or if you prefer a different definition, compute the per-class F1s from matrix and set MeanScore to their average) so the field is not left zero-valued; update the return in Metrics to include MeanScore: macroF1 (or the computed average).

- Verify each finding against the current code and only fix it if needed.

In @experiment/task/logic/semantic_algebra.go around lines 81 - 214, Artifacts() mixes metric computation, panel construction and prose text; extract small helpers to improve readability: create a computeSemanticAlgebraMetrics(experiment *SemanticAlgebraExperiment) that returns n, score, exactRate, sampleLabels, heatData, weightedVals, meanLine (use the same symbols from Artifacts: experiment.tableData, row.Scores, weightedVals, meanLine), create buildSemanticAlgebraPanels(sampleLabels []string, scoreLabels []string, heatData [][]any, weightedVals, meanLine []float64, score float64) []tools.Panel to build the panels slice (matches the panels/PanelSeries structure), and move the multiline proseTemplate into either a package-level const semanticAlgebraProseTemplate or a function semanticAlgebraProseTemplate() string; update Artifacts() to call these helpers and return identical tools.Artifact results.

- Verify each finding against the current code and only fix it if needed.

In @experiment/task/misc/gemma_integration.go around lines 431 - 446, The populated KV cache created by transformers.NewCache and filled via translator.PopulateCache is never used because sampler.Sample([]string{cas.Question}) is called without the cache; change the generation call to use the sampler API that accepts a pre-populated cache (or configure the sampler to use the provided cache) — for example replace sampler.Sample with the appropriate method such as SampleWithCache or call a setter like sampler.SetCache(cache) before sampling, ensuring you pass the populated cache returned by NewCache/PopulateCache into the sampler/generation call and handle any returned errors (refer to translator.PopulateCache, transformers.NewCache, and sampler.Sample to locate the code).

- Verify each finding against the current code and only fix it if needed.

In @experiment/task/misc/gemma_integration.go around lines 47 - 53, This file declares multiple struct types in one file (graftCase, kvCase, giResult, GemmaIntegrationExperiment, GemmaIntegrationError); split these into separate files to follow the guideline "Never have two objects in the same file" by creating distinct files such as gemma_case_types.go (containing graftCase and kvCase), gemma_result.go (containing giResult), and gemma_experiment.go or gemma_error.go (containing GemmaIntegrationExperiment and GemmaIntegrationError), ensure each new file has the same package declaration, export names only if needed, move any related helper methods with their struct, and update any references/imports in the codebase so all usages of graftCase, kvCase, giResult, GemmaIntegrationExperiment, and GemmaIntegrationError continue to compile.

- Verify each finding against the current code and only fix it if needed.

In @experiment/task/misc/gemma_integration.go at line 518, The variable name ng is not descriptive; rename it to something like graftCountFloat (or numGraftsFloat) where it is defined (ng := float64(max(len(exp.graftResults), 1))) and update all references to that symbol in the surrounding function (e.g., any calculations that use ng) so the intent—floating-point count of graftResults with a minimum of 1—is clear; ensure you update the declaration and every usage consistently.

- Verify each finding against the current code and only fix it if needed.

In @experiment/task/misc/gemma_integration.go at line 336, The call to backends.New() ignores its error return; change the invocation to capture (backend, err) := backends.New(), check err, and return a wrapped error (e.g., return fmt.Errorf("gemma backend: %w", err)) from the surrounding function so failures creating the backend are propagated; update any subsequent uses of backend to use the newly assigned variable.

- Verify each finding against the current code and only fix it if needed.

In @experiment/task/misc/gemma_integration.go around lines 336 - 341, The backend and sampler created with backends.New and samplers.New hold GPU/XLA resources and must be finalized: call backend.Finalize() via defer immediately after creating backend, and after creating sampler defer sampler.SampleStep.Finalize() (samplers.Sampler has no Close()). Ensure both defers are placed right after successful creation to prevent resource leaks and keep existing error handling around samplers.New intact.

- Verify each finding against the current code and only fix it if needed.

In @experiment/task/phasedial/adaptive_split.go around lines 76 - 102, Remove the ~27 consecutive blank lines in adaptive_split.go where the old Finalize implementation was deleted (around the Finalize-related area) and normalize spacing so there is at most a single blank line between top-level declarations and between functions; run gofmt/organize imports after cleaning to ensure formatting is consistent.

- Verify each finding against the current code and only fix it if needed.

In @experiment/task/phasedial/adaptive_split.go around lines 133 - 173, The artifact entry for the adaptive split prose (the struct with Type: tools.ArtifactProse, FileName: "adaptive_split_section.tex", Data: tools.ProseData{...}) is misindented compared to other entries in the artifacts slice; reformat the opening brace and all fields so they align with the surrounding slice entries (match indentation of prior artifact elements), ensuring the nested Data: map and Template string maintain consistent indentation and alignment with the other artifact blocks to restore style consistency.

- Verify each finding against the current code and only fix it if needed.

In @experiment/task/phasedial/chunking_baseline.go around lines 87 - 101, Remove the 15 consecutive blank lines left after removing the Finalize function in chunking_baseline.go: collapse the excessive empty lines between the surrounding top-level declarations so there is at most one blank line separating functions/decls (e.g., around where Finalize used to be in the file), ensuring normal vertical spacing and no large empty blocks.

- Verify each finding against the current code and only fix it if needed.

In @experiment/task/phasedial/chunking_baseline.go around lines 129 - 170, The ArtifactProse literal starting with the brace for the chunking baseline artifact (the block containing Type: tools.ArtifactProse, FileName: "chunking_baseline_section.tex", Template: ..., Data: ...) is misindented; fix by aligning the opening "{" with the surrounding composite literal/array entries and indenting the inner fields (Type, FileName, Data) consistently (one level deeper) so the brace, keys (Template, Data map), and closing "}" match the project's indentation style and improve readability.

- Verify each finding against the current code and only fix it if needed.

In @experiment/task/phasedial/correlation_length.go around lines 84 - 127, The return statement constructing PhasedialSectionArtifacts is left-aligned and breaks Go formatting; re-indent the entire return expression so the "return PhasedialSectionArtifacts(...)" line and its multi-line string and map arguments are indented consistently with surrounding code (align the opening paren and all subsequent lines), locating the call by looking for PhasedialSectionArtifacts and the uses of experiment.tableData and experiment.Score(); run gofmt or adjust spacing so the block conforms to standard Go indentation and formatting.

- Verify each finding against the current code and only fix it if needed.

In @experiment/task/phasedial/group_action_equivariance.go around lines 85 - 125, The return block for PhasedialSectionArtifacts is mis-indented; reformat the return so the opening parenthesis and its multi-line string, the map[string]any{...} argument, and the closing parenthesis are consistently indented under the return statement (align the backquoted LaTeX string, the map argument referencing experiment.tableData and experiment.Score(), and the final closing ')' on its own line) to match the style used in other phasedial experiment files.

- Verify each finding against the current code and only fix it if needed.

In @experiment/task/phasedial/group_action_equivariance.go around lines 95 - 96, There is a LaTeX typo in the comment string inside group_action_equivariance.go: replace the incorrect `$lpha$` token with the correct `$\alpha$` (i.e., add the missing backslash) so the angle symbol renders properly; locate the offending text (the sentence containing "angle $lpha$ and then retrieving...") and update it to use `$\alpha$`.

- Verify each finding against the current code and only fix it if needed.

In @experiment/task/phasedial/partial_deletion.go around lines 91 - 131, The prose artifact block for tools.ArtifactProse (FileName "partial_deletion_section.tex") is mis-indented; reformat the entire artifact literal to match the indentation style of the preceding table artifact so braces and fields (Type, FileName, Data, and the nested tools.ProseData.Template and Data map) align with surrounding items; specifically adjust the opening "{" and its closing "}," and indent the Template backtick block and Data map entries to the same column as other artifact entries to restore consistent code layout.

- Verify each finding against the current code and only fix it if needed.

In @experiment/task/phasedial/permutation_invariance.go around lines 83 - 125, The Artifacts method for PermutationInvarianceExperiment has inconsistent indentation around the PhasedialSectionArtifacts(...) return; reformat the block so the return expression and all its arguments align with the project's style (indent the PhasedialSectionArtifacts call and its parameters consistently), e.g. align the opening call with the return and indent the multiline string and map literal uniformly; update the PermutationInvarianceExperiment.Artifacts() function so PhasedialSectionArtifacts, the template string, and the map[string]any{"N": len(experiment.tableData), "Score": experiment.Score()} are consistently indented to match surrounding functions.

- Verify each finding against the current code and only fix it if needed.

In @experiment/task/phasedial/permutation_invariance.go around lines 105 - 108, Typo: the phrase "strong permutation invariance invariance" inside the conditional block starting with "{{if gt .Score 0.5 -}}" and the "\paragraph{Assessment.}" line has the word "invariance" duplicated; edit that text to a single "invariance" (i.e., change "strong permutation invariance invariance" to "strong permutation invariance") so the paragraph reads correctly.

- Verify each finding against the current code and only fix it if needed.

In @experiment/task/phasedial/phase_coherence.go around lines 83 - 124, The return block for PhasedialSectionArtifacts is mis-indented; adjust the indentation of the entire return statement so the function call and its multi-line string argument align with other phasedial experiment files: locate the PhasedialSectionArtifacts(...) call (using symbols PhasedialSectionArtifacts, experiment.tableData, experiment.Score()) and reformat the return so the opening "return" and the following arguments (title, tableData, experiment.Score(), the LaTeX string, and the map literal) are each consistently indented and aligned vertically to match the project's style.

- Verify each finding against the current code and only fix it if needed.

In @experiment/task/phasedial/query_robustness.go around lines 101 - 142, The tools.ArtifactProse literal for FileName "query_robustness_section.tex" has inconsistent indentation and misaligned struct fields; reformat the block so each field (Type, FileName, Data) is on its own indented line and the nested tools.ProseData literal aligns its Template and Data fields consistently (Template string on its own, then Data: map[...] properly indented), preserving the existing keys (N: len(experiment.tableData), Score: experiment.Score()) and template content; ensure braces and commas are aligned and the entire artifact entry matches surrounding artifact entries' indentation style.

- Verify each finding against the current code and only fix it if needed.

In @experiment/task/phasedial/snap_to_surface.go around lines 83 - 126, The return block constructing PhasedialSectionArtifacts is misindented; reformat the return statement and its multi-line arguments (the call to PhasedialSectionArtifacts, the long string literal, and the map literal with "N" and "Score") so they align with Go conventions (indent the arguments on subsequent lines and close the call at the same indentation level as the return), and run gofmt (or go vet) to ensure spacing is consistent around the function name PhasedialSectionArtifacts and the experiment.Score()/len(experiment.tableData) expressions.

- Verify each finding against the current code and only fix it if needed.

In @experiment/task/phasedial/steerability.go around lines 121 - 139, Remove the excessive 19 consecutive blank lines in experiment/task/phasedial/steerability.go around the region starting near line 121; collapse them down to appropriate spacing (at most one blank line between top-level declarations and a single blank line between logical code groups), ensuring surrounding functions/methods or type declarations (e.g., the nearby function or method declarations in steerability.go) remain separated by one newline only and file formatting follows the repository vertical spacing guidelines.

- Verify each finding against the current code and only fix it if needed.

In @experiment/task/phasedial/steerability.go around lines 151 - 190, The struct literal creating a tools.ArtifactProse for "steerability_section.tex" has inconsistent indentation for its fields (Type, FileName, Data) and nested ProseData/Template/Data blocks; reformat the literal to align fields and nested values with the file's prevailing Go style (align keys at same indentation level, indent the Template string and Data map consistently), touching the tools.ArtifactProse literal, the ProseData{ Template: ..., Data: map[string]any{ "N": len(experiment.tableData), "Score": experiment.Score(), } } block to ensure consistent indentation and readability.

- Verify each finding against the current code and only fix it if needed.

In @experiment/task/phasedial/torus_navigation.go around lines 131 - 145, There are many consecutive blank lines left before the Artifacts() method in torus_navigation.go; remove the excess empty lines so there is only a single blank line separating logical groups and leave exactly one blank line immediately before the Artifacts() method declaration to restore proper vertical spacing.

- Verify each finding against the current code and only fix it if needed.

In @experiment/task/phasedial/torus_navigation.go around lines 169 - 209, The ArtifactProse composite literal is mis-indented; reformat the block that begins with Type: tools.ArtifactProse so that the struct fields (Type, FileName, Data) align with the surrounding composite literals and the nested tools.ProseData keys (Template, Data) are consistently indented, and ensure the raw Template string lines remain intact but indented to match the ProseData block; also align the Data map entries (N, Score) under the Data key so the entire torus_navigation_section.tex artifact (and references to experiment.tableData and experiment.Score()) follows the file's existing indentation style.

- Verify each finding against the current code and only fix it if needed.

In @experiment/task/phasedial/two_hop_retrieval.go at line 15, The struct field evaluator is missing a comment; add a clear comment above the evaluator field in two_hop_retrieval.go that explains what the evaluator is, what behavior or interface it represents (e.g., tools.Evaluator used to score/validate retrieved candidates or compute metrics), why it is needed, and any expectations about ownership or lifecycle (nilability, concurrency, who sets it). Reference the evaluator field name and tools.Evaluator type in the comment so readers know its role and usage.

- Verify each finding against the current code and only fix it if needed.

In @experiment/task/phasedial/two_hop_retrieval.go around lines 138 - 178, Align the struct literal fields for the prose artifact consistently with the surrounding artifact entries: re-indent the block starting with the literal containing Type, FileName, Data (tools.ProseData) so that Type, FileName, and Data are vertically aligned with other artifact entries, and indent the nested Template and nested Data map entries (including the Template string and Data keys "N" and "Score" referencing len(experiment.tableData) and experiment.Score()) one level further to reflect their nesting; ensure spacing matches the file's prevailing style for artifact literals to improve readability.

- Verify each finding against the current code and only fix it if needed.

In @experiment/task/phasedial/two_hop_retrieval.go around lines 81 - 94, There are about 14 consecutive blank lines in two_hop_retrieval.go (around lines 81–94); remove the excess empty lines so that only a single blank line separates logical blocks (e.g., between the preceding declaration and the next function/struct in this file), then run gofmt/golangci-lint to ensure formatting is consistent.

- Verify each finding against the current code and only fix it if needed.

In @experiment/task/pipeline.go around lines 20 - 28, Add a Go doc comment above the Pipeline struct explaining its purpose and why it exists: describe that Pipeline manages a single experiment run lifecycle including context cancellation (ctx, cancel), the VM execution engine (machine *vm.Machine), the experiment configuration (experiment tools.PipelineExperiment), scoring weights (scoreWgts tools.ScoreWeights), reporting (reporter Reporter) and timing (timing runTiming); ensure the comment follows Go doc conventions (starts with "Pipeline ...") and briefly notes its role in orchestrating and coordinating the experiment run.

- Verify each finding against the current code and only fix it if needed.

In @paper/include/textgen/compositional_map.html at line 7, The external ECharts import via the <script src="https://cdn.jsdelivr.net/npm/echarts@5.5.0/dist/echarts.min.js"></script> tag should be hardened: either replace it with a locally bundled copy of echarts (serve dist/echarts.min.js from the repo) or add Subresource Integrity by including the correct integrity="sha384-..." attribute and crossorigin="anonymous" on that <script> tag to pin and verify the exact artifact; update any build/static manifest to include the bundled file if choosing the local option.

- Verify each finding against the current code and only fix it if needed.

In @paper/include/textgen/prose_chaining_map.html around lines 1 - 251, This file duplicates the ECharts rendering logic from compositional_map.html; extract the shared logic that builds grids/xAxes/yAxes/series/visualMaps and calls chart.setOption into a reusable function/module (e.g., renderPanels or initECharts) and import it from both pages, keeping only the panels data and title-specific config in each HTML; move constants like PALETTES and SERIES_COLORS into the module, accept the panels array and chart container/option overrides, and replace the repeated code paths that manipulate panels, push to grids/xAxes/yAxes/series/visualMaps, and call chart.setOption with a single call to the new function.

- Verify each finding against the current code and only fix it if needed.

In @pkg/logic/grammar/parser.go around lines 84 - 110, ParserServer's Close() and Client() both access and modify the shared map clientConns without synchronization, causing a data race; add a sync.RWMutex field (e.g., mu) to the ParserServer struct and use mu.Lock()/mu.Unlock() when mutating clientConns (writes/deletes in Client() and Close()) and mu.RLock()/mu.RUnlock() for read-only accesses, ensuring all accesses to clientConns in the Client() and Close() methods (and any other places that touch clientConns) are protected by the mutex.

- Verify each finding against the current code and only fix it if needed.

In @pkg/logic/semantic/server.go around lines 89 - 94, The loop in Close() iterates and deletes entries from server.clientConns while Client() may concurrently modify that map, risking a data race; wrap the entire iteration-and-delete sequence with the same mutex used to protect clientConns (e.g., acquire server.mu.Lock()/Unlock() or the existing sync.RWMutex used by Client()) so Close() holds the write lock while closing connections and deleting keys, or alternatively swap clientConns with a new empty map under the lock and then close connections outside the lock to avoid holding locks during Close calls; ensure Client(), Close(), and any other access to clientConns use the same mutex.

- Verify each finding against the current code and only fix it if needed.

In @pkg/logic/substrate/graph.go around lines 92 - 111, The Close() calls in the cleanup block for graph.serverConn, graph.clientConns (iterating conn), graph.serverSide, and graph.clientSide currently discard errors; change this to collect each error and either log them or aggregate and return them (e.g., build a slice of errors and use errors.Join before returning) instead of using `_ =`. Locate the shutdown code that references graph.serverConn, graph.clientConns, graph.serverSide, and graph.clientSide and for each Close() call capture the returned error, append it to an errors slice (or immediately log it via the package logger used in this package), and at the end either log the joined error or return errors.Join(errs...) so callers can observe shutdown failures.

- Verify each finding against the current code and only fix it if needed.

In @pkg/logic/substrate/graph.go around lines 91 - 117, GraphServer.Close modifies shared fields (serverConn, clientConns, serverSide, clientSide, cancel) without taking the GraphServer.mu mutex, causing data races when methods like Client() run concurrently; fix by acquiring the write lock at the start of Close() (e.g., graph.mu.Lock() and defer graph.mu.Unlock()) and perform all mutations to serverConn, clientConns, serverSide, clientSide and cancel while holding that lock so state changes are atomic and race-free.

- Verify each finding against the current code and only fix it if needed.

In @pkg/logic/synthesis/bvp/cantilever.go around lines 90 - 116, The Close() logic in CantileverServer.Close (and identical implementations in EngineServer.Close, FrustrationEngineServer.Close, MacroIndexServer.Close) should be extracted into a shared helper by defining a reusable embedded struct (e.g., BaseServer) that holds serverConn, clientConns, serverSide, clientSide, and cancel and implements a CloseShared() method that performs the close/clear/cancel sequence; then embed this BaseServer in CantileverServer (and the other server types) and replace their Close() bodies with a single call to the embedded BaseServer.CloseShared(), keeping existing symbols (CantileverServer.Close, EngineServer.Close, etc.) as thin delegators.

- Verify each finding against the current code and only fix it if needed.

In @pkg/logic/synthesis/macro/macro_index.go around lines 110 - 136, Both Close() and Client() must acquire the MacroIndexServer's mutex before mutating or iterating over shared state to avoid races: wrap the body of Close() with server.Lock() and defer server.Unlock(), and likewise acquire server.Lock() at the start of Client() (not RLock) before reading/updating clientConns (and any other shared fields like serverConn, serverSide, clientSide, cancel), then defer Unlock() so the map deletes and assignments are protected; use the existing sync.RWMutex on MacroIndexServer and ensure Unlock() runs even on early returns.

- Verify each finding against the current code and only fix it if needed.

In @pkg/store/data/value_test.go around lines 1 - 73, Add benchmark functions to measure projection and affine performance by creating two benchmark funcs in value_test.go: BenchmarkSeedObservableProjection and BenchmarkApplyAffinePhase; each should replicate the test setup (use NeutralValue(), SetStatePhase/SetGuardRadius/SetLexicalTransition/SetProgram for the projection benchmark and SetAffine for the affine benchmark), call b.ResetTimer() before the loop, and run the operation inside for i := 0; i < b.N; i++ { ... } returning/discarding the result; ensure the signatures are func BenchmarkX(b *testing.B) and do not alter existing tests or imports (reuse testing and numeric) so these run with go test -bench=.

- Verify each finding against the current code and only fix it if needed.

In @pkg/store/lsm/close_test.go around lines 1 - 19, Add a benchmark that measures Close() on the same type exercised by TestSpatialIndexServerCloseIsIdempotent: create a SpatialIndexServer via NewSpatialIndexServer, initialize its client with Client("test") to simulate the active RPC path, and then call Close() inside the b.N loop to benchmark the shutdown path; ensure any setup/teardown (creating the server once or per-iteration) is handled to avoid skewed results and use b.ReportAllocs() if desired to track allocations for the Close() method.

- Verify each finding against the current code and only fix it if needed.

In @pkg/store/lsm/operator_shell_test.go around lines 1 - 75, Add benchmark functions to this test file that measure the hot paths used by the tests: create BenchmarkPredictNextPhaseFromValue that constructs numeric.NewCalculus(), a data.NeutralValue() with SetAffine/SetTrajectory/SetGuardRadius and repeatedly calls predictNextPhaseFromValue(calc, value, 19, 'x'); create BenchmarkOperatorPhaseAcceptance that builds a data.NeutralValue() with SetGuardRadius and repeatedly calls operatorPhaseAcceptance(value, 10, 12); and add a BenchmarkWavefrontSearchPrompt that builds the same SpatialIndexServer, inserts observableValue entries (using idx.insertSync and data.MustNewChord), constructs NewWavefront(...) and repeatedly calls wf.SearchPrompt([]byte("ab"), nil, nil). In each benchmark use b.ResetTimer() before the loop and iterate for i := 0; i < b.N; i++ to exercise only the measured calls.

- Verify each finding against the current code and only fix it if needed.

In @pkg/store/lsm/scanner.go around lines 87 - 104, buildCache writes to scanner.cache while holding only scanner.index.mu RLock, causing concurrent map writes; add a dedicated mutex (e.g., cacheMu sync.RWMutex) to PhaseDialScanner and use cacheMu.Lock()/Unlock() around mutations in buildCache (when assigning scanner.cache[key] = ...), and use cacheMu.RLock()/RUnlock() in readers such as EntryDial and Scan (or replace scanner.cache with sync.Map and update all cache accesses accordingly) so all concurrent reads/writes to scanner.cache are properly synchronized.

- Verify each finding against the current code and only fix it if needed.

In @pkg/store/lsm/scanner.go at line 162, The calls to morton.Unpack(key) are failing because there is no package-level morton; Unpack is a method on Morton's type (MortonCoder). Modify PhaseDialScanner to hold a MortonCoder (e.g., add a morton field to the PhaseDialScanner struct) or instantiate a local MortonCoder where needed, then replace morton.Unpack(key) with s.morton.Unpack(key) (or localMorton.Unpack(key)); ensure the MortonCoder is initialized before use (e.g., in the constructor or NewPhaseDialScanner) so Unpack calls at lines referencing morton (the calls in methods using morton.Unpack) resolve correctly.

- Verify each finding against the current code and only fix it if needed.

In @pkg/store/lsm/scanner_test.go around lines 125 - 129, The test directly reads the internal field scanner.cache to verify InvalidateCache; update the assertion to use the public API instead—call scanner.InvalidateCache() then assert scanner.EntryCount() equals 0 (or add an additional gc.So(scanner.EntryCount(), gc.ShouldEqual, 0) assertion) rather than checking len(scanner.cache), keeping the verification via the public EntryCount() method and leaving InvalidateCache and EntryCount as the referenced symbols to locate the test code.

- Verify each finding against the current code and only fix it if needed.

In @pkg/store/lsm/skip.go around lines 254 - 264, Add a Go doc comment for walkStrideUnsafe explaining its purpose and contract: that walkStrideUnsafe advances the given skipCursor by attempting to take stride steps using stepCursorUnsafe, returns the resulting cursor and a bool indicating success (false if any step fails), and that it is an unsafe/internal helper (not concurrency-safe) used within SkipIndex; ensure the comment starts with "walkStrideUnsafe" and documents the parameters (current, stride) and return values and any safety/usage notes.

- Verify each finding against the current code and only fix it if needed.

In @pkg/store/lsm/skip.go around lines 24 - 42, Add Go doc comments above each internal struct type (skipNodeKey, skipCursor, skipVisit) following Go's comment convention: start the comment with the type name and briefly describe the purpose and role of the type within the LSM skip list implementation and why it exists (e.g., what key/value they represent, cursor traversal purpose, or visit bookkeeping across segments/phases). Keep each comment one or two sentences and place it immediately above the corresponding type declaration so tools like godoc and linters will pick them up.

- Verify each finding against the current code and only fix it if needed.

In @pkg/store/lsm/skip.go around lines 266 - 290, Add a Go doc comment for startCursorUnsafe that begins with the function name and briefly describes its behavior: that it attempts to create a skipCursor starting at the given startKey and startPhase by scanning the chain via skip.idx.followChainUnsafe and, if none match, falling back to skip.entries lookup and returning skip.cursorForEntryUnsafe; also note the returned values (skipCursor, bool) semantics and any concurrency/unsafe expectations (caller must hold appropriate locks or ensure safety). Reference the related symbols in the comment (startCursorUnsafe, skipCursor, followChainUnsafe, cursorForEntryUnsafe, ToKey, entries) so readers can quickly understand intent and usage.

- Verify each finding against the current code and only fix it if needed.

In @pkg/store/lsm/skip.go around lines 136 - 174, The function buildEntryUnsafe lacks a documentation comment; add a Go doc comment above func (skip *SkipIndex) buildEntryUnsafe(key uint64, value data.Chord) (SkipEntry, bool) that briefly describes its purpose (building a SkipEntry from a raw key/value pair), explains parameters (key, value) and return values (SkipEntry and success bool), and notes that it is unsafe (e.g., does not perform bounds checks) and any side effects or invariants (uses morton.Unpack, extractStatePhase, walkStrideUnsafe and relies on skipStrides); reference the receiver SkipIndex and related types SkipEntry/SkipPhase to clarify context.

- Verify each finding against the current code and only fix it if needed.

In @pkg/store/lsm/skip.go around lines 330 - 354, Add a concise Go doc comment above the validateEntryUnsafe function describing its purpose and behavior: state that validateEntryUnsafe verifies a SkipEntry at a given SkipLevel and segment by checking level bounds, entry validity, obtaining a cursor via cursorForEntryUnsafe, walking the stride with walkStrideUnsafe, and comparing landed.key, landed.valueKey, landed.phase, and segment delta to the entry's Level fields (Target, TargetValue, Phase, SegmentDelta); mention that the function is unsafe/internal (not concurrency-safe) and returns false on any validation failure.

- Verify each finding against the current code and only fix it if needed.

In @pkg/store/lsm/skip.go around lines 176 - 211, Add Go doc comments for each helper: resolveRootEntry, resolveNodeEntry, cursorForEntryUnsafe, and findValueUnsafe. For each comment start with the function name, briefly describe its purpose (e.g., resolving entries from skip.entries or skip.nodeEntries, constructing a skipCursor without locks, or scanning the chain for a Chord value), note important behavior (unsafe suffix implies no synchronization/locking), and document the return values (what the returned SkipEntry/skipCursor/data.Chord and bool indicate). Keep comments concise and place them immediately above each function declaration.

- Verify each finding against the current code and only fix it if needed.

In @pkg/store/lsm/skip.go around lines 213 - 252, Add a Go doc comment for the method stepCursorUnsafe on type SkipIndex describing its purpose and behavior: explain that stepCursorUnsafe advances a skipCursor to the next valid position by calling advanceProgramCursor, looking up candidate keys in skip.idx.positionIndex, following chains via skip.idx.followChainUnsafe, extracting phases with extractStatePhase, predicting expected phases with predictNextPhaseFromValue, and validating acceptance via operatorPhaseAcceptance; note that it returns the next skipCursor and a bool indicating success, that it operates on internal/unsafe state (not concurrency-safe), and any important invariants or preconditions (e.g., current cursor must be valid, callers must hold appropriate locks). Include references to involved symbols (SkipIndex.stepCursorUnsafe, skipCursor, advanceProgramCursor, skip.idx.positionIndex, skip.idx.followChainUnsafe, extractStatePhase, predictNextPhaseFromValue, operatorPhaseAcceptance) in the comment.

- Verify each finding against the current code and only fix it if needed.

In @pkg/store/lsm/skip_test.go around lines 110 - 148, Add a benchmark that measures performance of the reset-aware traversal path by creating the same test setup (use NewSpatialIndexServer(), numeric.NewCalculus(), observableValue, insertSync and data.MustNewChord()) to build a SkipIndex via NewSkipIndex(idx).Build(), then run b.ResetTimer() and repeatedly call SkipIndex.SkipSearch(keyA, aPhase) (and optionally SkipIndex.Jump(keyB, SkipNext)) inside the benchmark loop; ensure the benchmark uses testing.B naming (e.g., BenchmarkSkipIndexResetTraversal) and avoids measuring setup time by performing setup before b.ResetTimer(), and validate results minimally if needed to prevent compiler optimizations (e.g., store length or a returned value to a package-level sink).

- Verify each finding against the current code and only fix it if needed.

In @pkg/store/lsm/spatial_index.go around lines 396 - 412, Add a Go doc comment block immediately above the removeUint64Key function that begins with the function name and succinctly describes its behavior: that it removes the first occurrence of target from the slice of uint64 keys, returns a slice reusing the input backing array (or the original slice if empty), and leaves the slice unchanged if target is not found; mention that it preserves the order of remaining elements. Reference: removeUint64Key(keys []uint64, target uint64) []uint64.

- Verify each finding against the current code and only fix it if needed.

In @pkg/store/lsm/spatial_index_phase_test.go around lines 130 - 153, Add a benchmark that measures insertion and retrieval of observable values alongside the existing TestSpatialIndexStoresNativeValuesButReturnsObservables; create a BenchmarkSpatialIndexObservableInsertRetrieve function that initializes the index via NewSpatialIndexServer, prepares the same observable (using data.BaseChord, Set, SetResidualCarry, SetProgram) and key (morton.Pack), calls b.ResetTimer(), then in the loop runs idx.insertSync(key, observable, data.MustNewChord()) and idx.GetEntry(key) to measure performance; keep setup outside the timed loop, use b.N for iterations, and name the benchmark exactly BenchmarkSpatialIndexObservableInsertRetrieve so it is picked up by go test -bench.

- Verify each finding against the current code and only fix it if needed.

In @pkg/store/lsm/wavefront_carry.go around lines 21 - 26, Add a Go doc comment for the carrySeedKey struct describing its purpose and fields; update the declaration of carrySeedKey to be preceded by a one-line or multi-line comment that summarizes what the struct represents (e.g., seed key for wavefront carry operations) and optionally mentions the meaning of fields phase, pos, segment, and promptIdx to satisfy the project's documentation guidelines.

- Verify each finding against the current code and only fix it if needed.

In @pkg/store/lsm/wavefront_reset_test.go around lines 3 - 9, The test file is missing the morton package import used by morton.Pack; update the import block in pkg/store/lsm/wavefront_reset_test.go to include the morton package (e.g., import "github.com/theapemachine/six/pkg/morton") so calls to morton.Pack compile; ensure the import is added alongside the existing imports (gc, numeric, data).

- Verify each finding against the current code and only fix it if needed.

In @pkg/store/lsm/wavefront_transition.go around lines 8 - 11, Add a Go-style comment block immediately above the visitMark type declaration that explains what visitMark represents and why it exists (e.g., it stores a visited key and its segment identifier for wavefront traversal/marking). Reference the struct name visitMark so reviewers can locate it, and ensure the comment follows Go doc conventions (complete sentence, starts with "visitMark").

- Verify each finding against the current code and only fix it if needed.

In @pkg/store/lsm/wavefront_transition.go around lines 13 - 23, Add a Go doc comment for the method advanceTarget on type Wavefront that succinctly describes what the method does, its parameters and return values: explain that it computes the next traversal target given a *WavefrontHead (including the nil/head.path==0 cases), and mention the meaning of the three return values (next position, target segment, and whether a target was found). Place the comment immediately above the func (wf *Wavefront) advanceTarget(head *WavefrontHead) signature and reference advanceProgramCursor, WavefrontHead, and Wavefront in the comment where useful.

- Verify each finding against the current code and only fix it if needed.

In @pkg/store/lsm/wavefront_transition.go around lines 25 - 34, Add a Go doc comment for the method predictNextPhase on type Wavefront describing its behavior and parameters: explain what predictNextPhase(head *WavefrontHead, nextSymbol byte) returns (numeric.Phase), how it handles a nil head, a non-empty head.path (delegating to predictNextPhaseFromValue using wf.calc and the last path element), and the fallback to wf.advancePromptPhase(head.phase, nextSymbol); mention any side effects or assumptions about Wavefront, WavefrontHead, predictNextPhaseFromValue, and advancePromptPhase.

- Verify each finding against the current code and only fix it if needed.

In @pkg/store/lsm/wavefront_transition.go around lines 36 - 77, Add a Go doc comment block above the resolveTransition method on type Wavefront that succinctly describes the function's purpose (resolving the next numeric.Phase for a transition), its parameters (head *WavefrontHead, nextPos uint32, nextSymbol byte, stateChord data.Chord, expected numeric.Phase), the return values (resolved phase, accumulated penalty, ok flag), and the important control flow/exit conditions (failure when extractStatePhase fails, when anchorCorrect/anchorViolates rejects, when operatorPhaseAcceptance returns not ok, and the storedPhase check when head is nil), and note how penalties are accumulated (anchor penalty via anchorCorrect and guard/route penalties via operatorPhaseAcceptance/operatorRoutePenalty); mention the related helper functions used (extractStatePhase, anchorCorrect, anchorViolates, operatorRoutePenalty, operatorPhaseAcceptance) so readers can follow the logic.

- Verify each finding against the current code and only fix it if needed.

In @pkg/store/lsm/wavefront_transition.go around lines 79 - 81, Add a Go doc comment for the helper function visitFor so it explains its purpose and parameters/return (e.g., "visitFor returns a visitMark for the given key and segment") and place it immediately above the visitFor function declaration; reference the visitFor function and the visitMark type in the comment to meet the package's commenting guidelines.

- Verify each finding against the current code and only fix it if needed.

In @pkg/system/process/tokenizer/close_test.go around lines 11 - 25, Add a BenchmarkClose(b *testing.B) in the same file that constructs the same setup used by TestUniversalServerCloseIsIdempotent (create context with cancel, workerPool via pool.New, server via newServer and call server.Client("test")), then call b.ResetTimer() and loop b.N times invoking server.Close() to measure repeated idempotent Close calls; ensure you cancel the context (defer cancel()) and ignore the error returned from server.Close() in the loop (or assert nil if you prefer) so the benchmark measures Close() performance only.

- Verify each finding against the current code and only fix it if needed.

In @pkg/system/vm/booter.go around lines 140 - 146, Booter.Close currently calls booter.cancel without nil-check and discards errors from each closer.Close; change Close to return error, first check if booter.cancel != nil before invoking it, then iterate booter.closers calling closer.Close() and collect any non-nil errors (e.g., aggregate with fmt.Errorf("...: %w", err) or github.com/hashicorp/go-multierror) and finally return the combined error (or nil if none); references: Booter.Close, booter.cancel, booter.closers, closer.Close.

- Verify each finding against the current code and only fix it if needed.

In @pkg/system/vm/input/server.go around lines 103 - 129, The Close method currently discards all errors; modify PrompterServer.Close to capture and return the first non-nil error from any Close call instead of always returning nil: create a local err variable, for each close call (server.serverConn.Close(), each conn.Close() in server.clientConns, server.serverSide.Close(), server.clientSide.Close()) check the returned error and if err == nil set err = that error (leave cancel() as-is), and finally return err; optionally also log errors before returning if you want visibility.

- Verify each finding against the current code and only fix it if needed.

In @pkg/system/vm/projection_test.go around lines 1 - 67, Convert the two table-style tests into a single GoConvey BDD-style test and add a benchmark: replace TestMachineProjectionDisabledByDefault and TestMachineProjectionStagesBootExpectedOverlay with TestMachineProjection(t *testing.T) using gc.Convey blocks ("Given a Machine", nested "When created without projection options" and "When created with ProjectionIngest/ProjectionPrompt") and assertions via gc.So for machine.projection and booter parser/engine/cantilever IsValid checks; ensure you still call NewMachine with MachineWithContext, MachineWithProjection and defer machine.Close inside each Convey scope; additionally add BenchmarkMachineProjectionBoot(b *testing.B) that measures NewMachine creation with the relevant projection options to satisfy the required benchmark.

- Verify each finding against the current code and only fix it if needed.

In @pkg/system/vm/translation.go around lines 263 - 270, prefillExec.Call's return value is used without error handling and cache.Data is mutated in place; change the call site (prefillExec.Call) to handle and propagate any error or invalid result before using outputs (validate length/types as expected), and either document that the function mutates cache.Data or modify the code to build and return a new cache object instead of assigning to cache.Data; ensure trees.FromValuesAndTree is only invoked with validated outputs and preserve/return the original cache on error to avoid silent corruption.

- Verify each finding against the current code and only fix it if needed.

In @pkg/system/vm/translation.go around lines 364 - 380, The code directly indexes cache.Map with blockName (cache.Map[blockName]) which can panic if the key is missing; change to a safe lookup (e.g., v, ok := cache.Map[blockName]) and handle the missing case before passing blockCache into transformers.Block (either create/initialize an empty blockCache, pass nil, or return an error), and also add a short inline comment above Identity(x) (or remove it if unnecessary) clarifying its purpose (e.g., "no-op used for graph node isolation") so future readers understand why Identity is retained; ensure references are adjusted in the calls to transformers.Block and tl.SubstrateBlock when blockCache may be nil/initialized.

- Verify each finding against the current code and only fix it if needed.

In @pkg/system/vm/translation.go around lines 407 - 414, Rename the single-character loop variable `b` in the nested loop over `sequences`/`seq` to a descriptive name (e.g., `byteVal`, `seqByte`, or `tokenByte`) to follow the "avoid single character variable names" guideline; update all uses inside the loop (the length check and the append to `tokens` where `int32(b)+3` is computed) to use the new name, ensuring references to `tokens` and `maxLen` remain unchanged.

- Verify each finding against the current code and only fix it if needed.

In @pkg/system/vm/translation.go around lines 39 - 52, This file defines multiple object types; split TranslationConfig and TranslationLayerError into their own files to follow the "one object per file" rule: create translation_config.go containing the TranslationConfig type (and any methods that operate only on TranslationConfig) and translation_error.go containing TranslationLayerError (and its methods), leaving TranslationLayer in translation.go; update any references/imports and ensure receiver methods remain in the files that contain their receiver types (move methods whose receiver is TranslationConfig or TranslationLayerError into the new files), and run `go build`/tests to confirm there are no unresolved references.

- Verify each finding against the current code and only fix it if needed.

In @pkg/system/vm/translation.go around lines 287 - 298, The SubstrateBlock method uses single-letter variables that violate naming guidelines; rename parameter x to a descriptive name like inputNode (or node) and replace local g := x.Graph() with graph := inputNode.Graph(), then update all uses of x and g within TranslationLayer.SubstrateBlock (e.g., calls to x.DType(), x.Graph(), and any downstream operations) to the new names so the code compiles and follows the guideline for descriptive variable names.

- Verify each finding against the current code and only fix it if needed.

In @pkg/system/vm/translation.go at line 9, Replace the dot import of "github.com/gomlx/gomlx/graph" with a named alias (e.g., mlgraph) and update all references that currently rely on the dot import to be qualified (for example change Einsum, Softmax, Add, Gather, etc. to mlgraph.Einsum, mlgraph.Softmax, mlgraph.Add, mlgraph.Gather). Make sure to update every usage in pkg/system/vm/translation.go so the symbols are resolved via the alias and remove the dot import to avoid namespace pollution.

- Verify each finding against the current code and only fix it if needed.

In @pkg/system/vm/translation.go around lines 99 - 105, The code uses errnie.SafeMust to call tokFuture.Struct() and tokResult.Edges(), which masks errors and returns zero values while the surrounding function still declares an error return; either remove the error return from the enclosing function signature if you intend silent recovery, or replace the SafeMust calls with explicit error handling: call tokFuture.Struct() and check the returned error (and similarly tokResult.Edges()), and propagate a wrapped error (e.g., "tokenizer future: %w") so callers receive real errors instead of nil; update references to tokFuture.Struct, tokResult.Edges, and errnie.SafeMust accordingly.