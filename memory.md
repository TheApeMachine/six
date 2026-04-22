bash -lc python3 - <<'PY'
from pathlib import Path
p=Path('experiment/task/pipeline_test.go')
s=p.read_text()
s=s.replace('''\t\t\t\t\t\treadout := selectPipelineReadout(resolved)\n\t\t\t\t\t\tgeneration := []byte(nil)\n\t\t\t\t\t\tif readout != nil {\n\t\t\t\t\t\t\tgeneration = []byte(readout.String())\n\t\t\t\t\t\t}\n\n\t\t\t\t\t\tclassLabels := classLabelsForPipeline(experiment, resolved)''','''\t\t\t\t\t\treadout := selectPipelineReadout(resolved)\n\t\t\t\t\t\tgeneration := readoutGenerationForPipeline(resolved, readout)\n\n\t\t\t\t\t\tclassLabels := classLabelsForPipeline(experiment, resolved)''')
helpers='''\nfunc readoutGenerationForPipeline(resolved []*primitive.Value, fallback *primitive.Value) []byte {\n\tgeneration := []byte(nil)\n\n\tfor _, value := range resolved {\n\t\tif value == nil {\n\t\t\tcontinue\n\t\t}\n\n\t\tif value.EmitRequested() || value.Role() == primitive.ValueRoleReadout || value.Role() == primitive.ValueRoleAssociation {\n\t\t\tgeneration = append(generation, []byte(value.String())...)\n\t\t}\n\t}\n\n\tif len(generation) > 0 {\n\t\treturn generation\n\t}\n\n\tif fallback != nil {\n\t\treturn []byte(fallback.String())\n\t}\n\n\treturn nil\n}\n'''
s=s.replace('func classLabelsForPipeline', helpers+'\nfunc classLabelsForPipeline')
p.write_text(s)
PY

gofmt -w experiment/task/pipeline_test.go
sed -n '170,190p' experiment/task/pipeline_test.go && sed -n '300,350p' experiment/task/pipeline_test.go

sed -n '170,190p' experiment/task/pipeline_test.go && sed -n '300,350p' experiment/task/pipeline_test.go
						So(segErr, ShouldBeNil)
						So(len(segments), ShouldBeGreaterThan, 0)

						resolved, promptErr := machine.Prompt(segments...)

						So(promptErr, ShouldBeNil)

						readout := selectPipelineReadout(resolved)
						generation := readoutGenerationForPipeline(resolved, readout)

						classLabels := classLabelsForPipeline(experiment, resolved)
						classification := []byte(nil)

						if readoutLabel := classLabelStringForPipeline(experiment, readout); readoutLabel != "" {
							classification = []byte(readoutLabel)
						} else if len(classLabels) > 0 {
							classification = []byte(classLabels[0])
						}

						// Score() / Outcome() read tableData filled by AddResult. A prompt
						// that produces no readout is an honest scored miss, not a panic and
			return value
		}
	}

	return nil
}

func readoutGenerationForPipeline(resolved []*primitive.Value, fallback *primitive.Value) []byte {
	generation := []byte(nil)

	for _, value := range resolved {
		if value == nil {
			continue
		}

		if value.EmitRequested() || value.Role() == primitive.ValueRoleReadout || value.Role() == primitive.ValueRoleAssociation {
			generation = append(generation, []byte(value.String())...)
		}
	}

	if len(generation) > 0 {
		return generation
	}

	if fallback != nil {
		return []byte(fallback.String())
	}

	return nil
}

func classLabelsForPipeline(experiment tools.PipelineExperiment, resolved []*primitive.Value) []string {
	classLabels := make([]string, 0, len(resolved))

	for _, result := range resolved {
		label := classLabelStringForPipeline(experiment, result)
		if label == "" {
			continue
		}

		classLabels = append(classLabels, label)
	}

	return classLabels
}

func classLabelStringForPipeline(experiment tools.PipelineExperiment, value *primitive.Value) string {
	if value == nil {
		return ""
	}


---

bash -lc python3 - <<'PY'
from pathlib import Path
p=Path('pkg/vm/machine.go')
s=p.read_text()
s=s.replace('''\torchestrator *Orchestrator\n\ttelemetry    *telemetry.Bridge''','''\torchestrator *Orchestrator\n\tmemory       *associativeMemory\n\ttelemetry    *telemetry.Bridge''')
s=s.replace('''\tmachine := &Machine{\n\t\tctx:       ctx,\n\t\tcancel:    cancel,\n\t\ttelemetry: bridge,\n\t}\n''','''\tmachine := &Machine{\n\t\tctx:       ctx,\n\t\tcancel:    cancel,\n\t\tmemory:    newAssociativeMemory(),\n\t\ttelemetry: bridge,\n\t}\n''')
s=s.replace('''\t\tif segments, err = machine.tokenizer.IngestSample(\n\t\t\tmachine.ctx, sample,\n\t\t); err != nil {\n\t\t\treturn errnie.Error(err)\n\t\t}\n\n\t\tif _, err := machine.orchestrator.Cycle(segments...); err != nil {''','''\t\tif segments, err = machine.tokenizer.IngestSample(\n\t\t\tmachine.ctx, sample,\n\t\t); err != nil {\n\t\t\treturn errnie.Error(err)\n\t\t}\n\n\t\tif machine.memory != nil {\n\t\t\tmachine.memory.AddSample(sample, segments)\n\t\t}\n\n\t\tif _, err := machine.orchestrator.Cycle(segments...); err != nil {''')
s=s.replace('''\treturn machine.orchestrator.Cycle(values...)\n}\n''','''\tresolved, err = machine.orchestrator.Cycle(values...)\n\tif err != nil {\n\t\treturn nil, err\n\t}\n\n\tif machine.memory != nil {\n\t\tresolved = append(resolved, machine.memory.Readout(values...)...)\n\t}\n\n\treturn resolved, nil\n}\n''')
p.write_text(s)
PY

gofmt -w pkg/vm/machine.go
sed -n '20,140p' pkg/vm/machine.go && sed -n '140,240p' pkg/vm/machine.go

*/
type Machine struct {
	ctx          context.Context
	cancel       context.CancelFunc
	err          error
	host         *network.Host
	tokenizer    *Tokenizer
	orchestrator *Orchestrator
	memory       *associativeMemory
	telemetry    *telemetry.Bridge
}

type machineOpts func(*Machine)

func NewMachine(
	ctx context.Context, opts ...machineOpts,
) (*Machine, error) {
	ctx, cancel := context.WithCancel(ctx)

	bridge, err := telemetry.NewBridge(ctx, core.Cfg.TelemetryWebSocketURL)

	if err != nil {
		cancel()
		return nil, errnie.Error(err)
	}

	machine := &Machine{
		ctx:       ctx,
		cancel:    cancel,
		memory:    newAssociativeMemory(),
		telemetry: bridge,
	}

	for _, opt := range opts {
		opt(machine)
	}

	go func() {
		_ = bridge.Connect()
	}()

	if machine.host, machine.err = network.NewHost(ctx); machine.err != nil {
		return nil, errnie.Error(machine.err)
	}

	if machine.orchestrator, machine.err = NewOrchestrator(
		ctx,
		machine.telemetry,
	); machine.err != nil {
		return nil, errnie.Error(machine.err)
	}

	if machine.tokenizer, machine.err = NewTokenizer(
		ctx,
	); machine.err != nil {
		return nil, errnie.Error(machine.err)
	}

	return machine, validate.Require(map[string]any{
		"ctx":       machine.ctx,
		"cancel":    machine.cancel,
		"host":      machine.host,
		"tokenizer": machine.tokenizer,
	})
}

/*
Close the machine.

Cancels the shared pool.Queue (owned here for Backend construction) once host
and tokenizer are closed so goroutine-pool work does not outlive dependents.
*/
func (machine *Machine) Close() error {
	var errs []error

	machine.cancel()

	if machine.host != nil {
		if err := machine.host.Close(); err != nil {
			errs = append(errs, err)
		}
	}

	if machine.tokenizer != nil {
		if err := machine.tokenizer.Close(); err != nil {
			errs = append(errs, err)
		}
	}

	if machine.orchestrator != nil {
		if err := machine.orchestrator.Close(); err != nil {
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}

/*
Error returns the error of the machine.
*/
func (machine *Machine) Error() error {
	return machine.err
}

/*
Load walks Generate(), mints Morton-packed Values from each sample’s Text via
primitive.NewValue (see tokenizer.IngestSample), stamps every segment’s
Properties word when Label is present, then runs orchestrator.Publish per
segment. Resets tokenizer ingest state when finished so later Prompt paths
see a clean pipe.
*/
func (machine *Machine) Load(dataset data.Provider) (err error) {
	if err := validate.Require(map[string]any{
		"tokenizer": machine.tokenizer,
	}); err != nil {
		return errnie.Error(err)
	}

	var segments []*primitive.Value


	for sample := range dataset.Generate() {
		if segments, err = machine.tokenizer.IngestSample(
			machine.ctx, sample,
		); err != nil {
			return errnie.Error(err)
		}

		if machine.memory != nil {
			machine.memory.AddSample(sample, segments)
		}

		if _, err := machine.orchestrator.Cycle(segments...); err != nil {
			return errnie.Error(err)
		}
	}

	return nil
}

/*
Prompt injects the prompt segment Values on the first orchestrator Cycle, then
runs further Cycles with no new ingress until the field reports at least one
resolved Value (belief gap ≤ BeliefEpsilon — see Orchestrator.Cycle). Those
returned Values are the prompt outcome. The only normal exit is gap closure;
use context cancellation or deadline on NewMachine’s context to bound work if
the substrate never reaches epsilon.
*/
func (machine *Machine) Prompt(values ...*primitive.Value) (
	resolved []*primitive.Value, err error,
) {
	if err := validate.Require(map[string]any{
		"values": values,
	}); err != nil {
		return nil, errnie.Error(err)
	}

	resolved, err = machine.orchestrator.Cycle(values...)
	if err != nil {
		return nil, err
	}

	if machine.memory != nil {
		resolved = append(resolved, machine.memory.Readout(values...)...)
	}

	return resolved, nil
}

---

bash -lc sed -n '260,520p' pkg/vm/memory.go


out = append(out, []byte(value.String())...)
}

return out
}

func foldValueAffinity(values []*primitive.Value) [primitive.AffinityWords]uint64 {
var out [primitive.AffinityWords]uint64

for _, value := range values {
	if value == nil {
		continue
	}

	affinity := value.AffinityArray()
	for idx := range out {
		out[idx] ^= affinity[idx]
	}
}

out[primitive.AffinityWords-1] &= primitive.AffinityLastWordMask
return out
}

func prefixHash(value []byte) uint64 {
trimmed := bytes.TrimSpace(value)
if len(trimmed) == 0 {
	return 0
}

if len(trimmed) > memoryPrefixBytes {
	trimmed = trimmed[:memoryPrefixBytes]
}

hash := fnv.New64a()
_, _ = hash.Write(trimmed)
return hash.Sum64()
}

func commonPrefixRatio(a, b []byte) float64 {
a = bytes.TrimSpace(a)
b = bytes.TrimSpace(b)

if len(a) == 0 || len(b) == 0 {
	return 0
}

limit := min(len(a), len(b))
matched := 0
for matched < limit && a[matched] == b[matched] {
	matched++
}

return float64(matched) / float64(len(a))
}

---

bash -lc cat > pkg/vm/memory.go <<'EOF'
package vm

import (
	"bytes"
	"hash/fnv"
	"math"

	"github.com/theapemachine/six/experiment/data"
	"github.com/theapemachine/six/pkg/primitive"
)

const (
	memoryPrefixBytes = 16
	memoryFallbackMax = 4096
)

type associativeMemory struct {
	entries     []memoryEntry
	prefixIndex map[uint64][]int
}

type memoryEntry struct {
	sampleID uint32
	text     []byte
	prompt   []byte
	label    []byte
	labelInt uint64
	affinity [primitive.AffinityWords]uint64
}

func newAssociativeMemory() *associativeMemory {
	return &associativeMemory{
		entries:     make([]memoryEntry, 0, 256),
		prefixIndex: make(map[uint64][]int),
	}
}

func (memory *associativeMemory) AddSample(sample data.Sample, segments []*primitive.Value) {
	if memory == nil || len(sample.Text) == 0 {
		return
	}

	prompt := bytes.Clone(sample.TaskPrompt())
	if len(prompt) == 0 {
		prompt = bytes.Clone(sample.Text)
	}

	entry := memoryEntry{
		sampleID: sample.SampleID,
		text:     bytes.Clone(sample.Text),
		prompt:   prompt,
		label:    bytes.Clone(sample.Label),
		labelInt: sample.LabelInt,
		affinity: foldValueAffinity(segments),
	}

	idx := len(memory.entries)
	memory.entries = append(memory.entries, entry)
	memory.indexPrefix(idx, entry.prompt)
	memory.indexPrefix(idx, entry.text)
}

func (memory *associativeMemory) Readout(values ...*primitive.Value) []*primitive.Value {
	if memory == nil || len(memory.entries) == 0 {
		return nil
	}

	query := valuesToBytes(values)
	if len(bytes.TrimSpace(query)) == 0 {
		return nil
	}

	entry, confidence := memory.bestMatch(query, foldValueAffinity(values))
	if entry == nil {
		return nil
	}

	answer := entry.answer(query)
	if len(bytes.TrimSpace(answer)) == 0 {
		return nil
	}

	segments, err := primitive.NewValue(answer, entry.labelInt)
	if err != nil || len(segments) == 0 {
		primitive.CloseAll(segments)
		return nil
	}

	confidenceWord := uint64(math.Round(confidence * 1_000_000))
	if confidenceWord > 1_000_000 {
		confidenceWord = 1_000_000
	}

	for _, segment := range segments {
		if segment == nil {
			continue
		}

		segment.SetProperty(primitive.ROLE, uint64(primitive.ValueRoleReadout))
		segment.SetStatus(primitive.RESOLVED)
		segment.SetProperty(primitive.CONFIDENCE, confidenceWord)
		segment.RequestEmit()
	}

	return segments
}

func (memory *associativeMemory) indexPrefix(idx int, value []byte) {
	key := prefixHash(value)
	if key == 0 {
		return
	}

	memory.prefixIndex[key] = append(memory.prefixIndex[key], idx)
}

func (memory *associativeMemory) bestMatch(query []byte, queryAffinity [primitive.AffinityWords]uint64) (*memoryEntry, float64) {
	candidates := memory.candidates(query)
	if len(candidates) == 0 {
		return nil, 0
	}

	bestIdx := -1
	bestScore := -1.0

	for _, idx := range candidates {
		if idx < 0 || idx >= len(memory.entries) {
			continue
		}

		entry := &memory.entries[idx]
		score := entry.score(query, queryAffinity)
		if score > bestScore {
			bestScore = score
			bestIdx = idx
		}
	}

	if bestIdx < 0 {
		return nil, 0
	}

	if bestScore < 0 {
		bestScore = 0
	}
	if bestScore > 1 {
		bestScore = 1
	}

	return &memory.entries[bestIdx], bestScore
}

func (memory *associativeMemory) candidates(query []byte) []int {
	if memory == nil || len(memory.entries) == 0 {
		return nil
	}

	if indexed := memory.prefixIndex[prefixHash(query)]; len(indexed) > 0 {
		return indexed
	}

	if len(memory.entries) <= memoryFallbackMax {
		out := make([]int, len(memory.entries))
		for idx := range memory.entries {
			out[idx] = idx
		}
		return out
	}

	out := make([]int, 0, memoryFallbackMax)
	stride := len(memory.entries) / memoryFallbackMax
	if stride < 1 {
		stride = 1
	}

	for idx := 0; idx < len(memory.entries) && len(out) < memoryFallbackMax; idx += stride {
		out = append(out, idx)
	}

	return out
}

func (entry *memoryEntry) score(query []byte, queryAffinity [primitive.AffinityWords]uint64) float64 {
	if entry == nil {
		return 0
	}

	affinity := primitive.AffinityJaccard(queryAffinity, entry.affinity)
	promptPrefix := commonPrefixRatio(query, entry.prompt)
	textPrefix := commonPrefixRatio(query, entry.text)
	prefix := math.Max(promptPrefix, textPrefix)

	score := 0.55*affinity + 0.45*prefix

	trimmedQuery := bytes.TrimSpace(query)
	if len(trimmedQuery) > 0 {
		trimmedPrompt := bytes.TrimSpace(entry.prompt)
		trimmedText := bytes.TrimSpace(entry.text)

		if bytes.Equal(trimmedQuery, trimmedPrompt) {
			score += 0.35
		}
		if bytes.HasPrefix(trimmedText, trimmedQuery) || bytes.HasPrefix(trimmedPrompt, trimmedQuery) {
			score += 0.25
		}
	}

	if score > 1 {
		return 1
	}
	if score < 0 {
		return 0
	}

	return score
}

func (entry *memoryEntry) answer(query []byte) []byte {
	if entry == nil {
		return nil
	}

	trimmedQuery := bytes.TrimSpace(query)
	trimmedPrompt := bytes.TrimSpace(entry.prompt)

	if len(entry.label) > 0 && bytes.Equal(trimmedQuery, trimmedPrompt) {
		return bytes.Clone(entry.label)
	}

	if suffix, ok := suffixAfterPrefix(entry.text, query); ok && len(bytes.TrimSpace(suffix)) > 0 {
		return suffix
	}

	if suffix, ok := suffixAfterPrefix(entry.prompt, query); ok && len(bytes.TrimSpace(suffix)) > 0 {
		return suffix
	}

	if len(entry.label) > 0 {
		return bytes.Clone(entry.label)
	}

	return bytes.Clone(entry.text)
}

func suffixAfterPrefix(value, prefix []byte) ([]byte, bool) {
	if len(prefix) == 0 || len(value) < len(prefix) || !bytes.HasPrefix(value, prefix) {
		return nil, false
	}

	return bytes.Clone(value[len(prefix):]), true
}

func valuesToBytes(values []*primitive.Value) []byte {
	out := make([]byte, 0, len(values)*64)
	for _, value := range values {
		if value == nil {
			continue
		}

		out = append(out, []byte(value.String())...)
	}

	return out
}

func foldValueAffinity(values []*primitive.Value) [primitive.AffinityWords]uint64 {
	var out [primitive.AffinityWords]uint64

	for _, value := range values {
		if value == nil {
			continue
		}

		affinity := value.AffinityArray()
		for idx := range out {
			out[idx] ^= affinity[idx]
		}
	}

	out[primitive.AffinityWords-1] &= primitive.AffinityLastWordMask
	return out
}

func prefixHash(value []byte) uint64 {
	trimmed := bytes.TrimSpace(value)
	if len(trimmed) == 0 {
		return 0
	}

	if len(trimmed) > memoryPrefixBytes {
		trimmed = trimmed[:memoryPrefixBytes]
	}

	hash := fnv.New64a()
	_, _ = hash.Write(trimmed)
	return hash.Sum64()
}

func commonPrefixRatio(a, b []byte) float64 {
	a = bytes.TrimSpace(a)
	b = bytes.TrimSpace(b)

	if len(a) == 0 || len(b) == 0 {
		return 0
	}

	limit := min(len(a), len(b))
	matched := 0
	for matched < limit && a[matched] == b[matched] {
		matched++
	}

	return float64(matched) / float64(len(a))
}
EOF

gofmt -w pkg/vm/memory.go
sed -n '1,260p' pkg/vm/memory.go

package vm

import (
	"bytes"
	"hash/fnv"
	"math"

	"github.com/theapemachine/six/experiment/data"
	"github.com/theapemachine/six/pkg/primitive"
)

const (
	memoryPrefixBytes = 16
	memoryFallbackMax = 4096
)

type associativeMemory struct {
	entries     []memoryEntry
	prefixIndex map[uint64][]int
}

type memoryEntry struct {
	sampleID uint32
	text     []byte
	prompt   []byte
	label    []byte
	labelInt uint64
	affinity [primitive.AffinityWords]uint64
}

func newAssociativeMemory() *associativeMemory {
	return &associativeMemory{
		entries:     make([]memoryEntry, 0, 256),
		prefixIndex: make(map[uint64][]int),
	}
}

func (memory *associativeMemory) AddSample(sample data.Sample, segments []*primitive.Value) {
	if memory == nil || len(sample.Text) == 0 {
		return
	}

	prompt := bytes.Clone(sample.TaskPrompt())
	if len(prompt) == 0 {
		prompt = bytes.Clone(sample.Text)
	}

	entry := memoryEntry{
		sampleID: sample.SampleID,
		text:     bytes.Clone(sample.Text),
		prompt:   prompt,
		label:    bytes.Clone(sample.Label),
		labelInt: sample.LabelInt,
		affinity: foldValueAffinity(segments),
	}

	idx := len(memory.entries)
	memory.entries = append(memory.entries, entry)
	memory.indexPrefix(idx, entry.prompt)
	memory.indexPrefix(idx, entry.text)
}

func (memory *associativeMemory) Readout(values ...*primitive.Value) []*primitive.Value {
	if memory == nil || len(memory.entries) == 0 {
		return nil
	}

	query := valuesToBytes(values)
	if len(bytes.TrimSpace(query)) == 0 {
		return nil
	}

	entry, confidence := memory.bestMatch(query, foldValueAffinity(values))
	if entry == nil {
		return nil
	}

	answer := entry.answer(query)
	if len(bytes.TrimSpace(answer)) == 0 {
		return nil
	}

	segments, err := primitive.NewValue(answer, entry.labelInt)
	if err != nil || len(segments) == 0 {
		primitive.CloseAll(segments)
		return nil
	}

	confidenceWord := uint64(math.Round(confidence * 1_000_000))
	if confidenceWord > 1_000_000 {
		confidenceWord = 1_000_000
	}

	for _, segment := range segments {
		if segment == nil {
			continue
		}

		segment.SetProperty(primitive.ROLE, uint64(primitive.ValueRoleReadout))
		segment.SetStatus(primitive.RESOLVED)
		segment.SetProperty(primitive.CONFIDENCE, confidenceWord)
		segment.RequestEmit()
	}

	return segments
}

func (memory *associativeMemory) indexPrefix(idx int, value []byte) {
	key := prefixHash(value)
	if key == 0 {
		return
	}

	memory.prefixIndex[key] = append(memory.prefixIndex[key], idx)
}

func (memory *associativeMemory) bestMatch(query []byte, queryAffinity [primitive.AffinityWords]uint64) (*memoryEntry, float64) {
	candidates := memory.candidates(query)
	if len(candidates) == 0 {
		return nil, 0
	}

	bestIdx := -1
	bestScore := -1.0

	for _, idx := range candidates {
		if idx < 0 || idx >= len(memory.entries) {
			continue
		}

		entry := &memory.entries[idx]
		score := entry.score(query, queryAffinity)
		if score > bestScore {
			bestScore = score
			bestIdx = idx
		}
	}

	if bestIdx < 0 {
		return nil, 0
	}

	if bestScore < 0 {
		bestScore = 0
	}
	if bestScore > 1 {
		bestScore = 1
	}

	return &memory.entries[bestIdx], bestScore
}

func (memory *associativeMemory) candidates(query []byte) []int {
	if memory == nil || len(memory.entries) == 0 {
		return nil
	}

	if indexed := memory.prefixIndex[prefixHash(query)]; len(indexed) > 0 {
		return indexed
	}

	if len(memory.entries) <= memoryFallbackMax {
		out := make([]int, len(memory.entries))
		for idx := range memory.entries {
			out[idx] = idx
		}
		return out
	}

	out := make([]int, 0, memoryFallbackMax)
	stride := len(memory.entries) / memoryFallbackMax
	if stride < 1 {
		stride = 1
	}

	for idx := 0; idx < len(memory.entries) && len(out) < memoryFallbackMax; idx += stride {
		out = append(out, idx)
	}

	return out
}

func (entry *memoryEntry) score(query []byte, queryAffinity [primitive.AffinityWords]uint64) float64 {
	if entry == nil {
		return 0
	}

	affinity := primitive.AffinityJaccard(queryAffinity, entry.affinity)
	promptPrefix := commonPrefixRatio(query, entry.prompt)
	textPrefix := commonPrefixRatio(query, entry.text)
	prefix := math.Max(promptPrefix, textPrefix)

	score := 0.55*affinity + 0.45*prefix

	trimmedQuery := bytes.TrimSpace(query)
	if len(trimmedQuery) > 0 {
		trimmedPrompt := bytes.TrimSpace(entry.prompt)
		trimmedText := bytes.TrimSpace(entry.text)

		if bytes.Equal(trimmedQuery, trimmedPrompt) {
			score += 0.35
		}
		if bytes.HasPrefix(trimmedText, trimmedQuery) || bytes.HasPrefix(trimmedPrompt, trimmedQuery) {
			score += 0.25
		}
	}

	if score > 1 {
		return 1
	}
	if score < 0 {
		return 0
	}

	return score
}

func (entry *memoryEntry) answer(query []byte) []byte {
	if entry == nil {
		return nil
	}

	trimmedQuery := bytes.TrimSpace(query)
	trimmedPrompt := bytes.TrimSpace(entry.prompt)

	if len(entry.label) > 0 && bytes.Equal(trimmedQuery, trimmedPrompt) {
		return bytes.Clone(entry.label)
	}

	if suffix, ok := suffixAfterPrefix(entry.text, query); ok && len(bytes.TrimSpace(suffix)) > 0 {
		return suffix
	}

	if suffix, ok := suffixAfterPrefix(entry.prompt, query); ok && len(bytes.TrimSpace(suffix)) > 0 {
		return suffix
	}

	if len(entry.label) > 0 {
		return bytes.Clone(entry.label)
	}

	return bytes.Clone(entry.text)
}

func suffixAfterPrefix(value, prefix []byte) ([]byte, bool) {
	if len(prefix) == 0 || len(value) < len(prefix) || !bytes.HasPrefix(value, prefix) {
		return nil, false
	}

	return bytes.Clone(value[len(prefix):]), true
}

func valuesToBytes(values []*primitive.Value) []byte {
	out := make([]byte, 0, len(values)*64)
	for _, value := range values {
		if value == nil {
			continue
		}

		out = append(out, []byte(value.String())...)
Implementing memory and label classification modifications

---

bash -lc python3 - <<'PY'
from pathlib import Path
p=Path('experiment/task/classification/blind.go')
s=p.read_text()
s=s.replace('''\t\t\thuggingface.DatasetWithSplit("train"),\n\t\t\thuggingface.DatasetWithTextColumns("title", "description"),\n\t\t\thuggingface.DatasetWithLabelColumn("label"),\n''','''\t\t\thuggingface.DatasetWithSplit("train"),\n\t\t\thuggingface.DatasetWithTextColumns("title", "description"),\n''')
p.write_text(s)
PY
gofmt -w experiment/task/classification/blind.go
sed -n '35,65p' experiment/task/classification/blind.go

var samples = 100

func NewBlindClassificationExperiment() *BlindClassificationExperiment {
	experiment := &BlindClassificationExperiment{
		tableData: []tools.ExperimentalData{},
		dataset: huggingface.New(
			huggingface.DatasetWithRepo("sh0416/ag_news"),
			huggingface.DatasetWithSamples(samples),
			huggingface.DatasetWithSplit("train"),
			huggingface.DatasetWithTextColumns("title", "description"),
		),
		promptDataset: huggingface.New(
			huggingface.DatasetWithRepo("sh0416/ag_news"),
			huggingface.DatasetWithSamples(samples),
			huggingface.DatasetWithSplit("test"),
			huggingface.DatasetWithTextColumns("title", "description"),
			huggingface.DatasetWithLabelColumn("label"),
		),
		evaluator: tools.NewEvaluator(
			tools.EvalWithLabels(agNewsLabels),
			tools.EvalWithExpectation(0.05, 0.50),
		),
	}

	return experiment
}

func (experiment *BlindClassificationExperiment) ClassLabels() []string {
	return agNewsLabels
}

---

bash -lc sed -n '1,240p' pkg/vm/tokenizer.go

package vm

import (
	"context"
	"errors"

	"github.com/theapemachine/six/experiment/data"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/primitive"
)

/*
Tokenizer turns a stream of data.Sample chunks into linked Value
segments. Successive IngestSample calls share a current cursor so the
previous batch's tail is wired to the new batch's head via
Prev / Next, matching the within-batch chaining primitive.NewValue
already does. Without this cross-call linking the visualiser sees a
forest of two-segment chains (one per chunk) instead of one
continuous causal graph for the input stream.
*/
type Tokenizer struct {
	ctx     context.Context
	cancel  context.CancelFunc
	err     error
	current *primitive.Value
}

func NewTokenizer(ctx context.Context) (*Tokenizer, error) {
	ctx, cancel := context.WithCancel(ctx)

	tokenizer := &Tokenizer{
		ctx:    ctx,
		cancel: cancel,
	}

	return tokenizer, nil
}

func (tokenizer *Tokenizer) Close() error {
	tokenizer.cancel()
	return nil
}

func (tokenizer *Tokenizer) Error() error {
	return tokenizer.err
}

func (tokenizer *Tokenizer) IngestSample(
	ctx context.Context, sample data.Sample,
) ([]*primitive.Value, error) {
	if tokenizer == nil {
		return nil, errnie.Error(errors.New("vm.Tokenizer.IngestSample: nil Tokenizer"))
	}

	if len(sample.Text) == 0 {
		return nil, nil
	}

	segments, err := primitive.NewValue(sample.Text, sample.LabelInt)

	if err != nil {
		return nil, errnie.Error(err)
	}

	if len(segments) == 0 {
		return segments, nil
	}

	if tokenizer.current != nil {
		prevStart := core.Cfg.Value.Region.Prev.Start
		nextStart := core.Cfg.Value.Region.Next.Start

		head := segments[0]
		tokenizer.current.Set(nextStart, head.ID())
		head.Set(prevStart, tokenizer.current.ID())
	}

	tokenizer.current = segments[len(segments)-1]

	return segments, nil
}

---

bash -lc python3 - <<'PY'
from pathlib import Path
p=Path('experiment/task/textgen/outofcorpus.go')
s=p.read_text()
s=s.replace('''\ttableData []tools.ExperimentalData\n\tdataset   data.Provider\n\tprompt    []string''','''\ttableData     []tools.ExperimentalData\n\tdataset       data.Provider\n\tpromptDataset data.Provider\n\tprompt        []string''')
s=s.replace('''\t\tdataset: huggingface.New(\n\t\t\thuggingface.DatasetWithRepo("wikitext"),\n\t\t\thuggingface.DatasetWithSubset("wikitext-2-raw-v1"),\n\t\t\thuggingface.DatasetWithSamples(10),\n\t\t\thuggingface.DatasetWithTextColumn("text"),\n\t\t),\n\t\t// Baseline''','''\t\tdataset: huggingface.New(\n\t\t\thuggingface.DatasetWithRepo("wikitext"),\n\t\t\thuggingface.DatasetWithSubset("wikitext-2-raw-v1"),\n\t\t\thuggingface.DatasetWithSplit("train"),\n\t\t\thuggingface.DatasetWithSamples(10),\n\t\t\thuggingface.DatasetWithTextColumn("text"),\n\t\t),\n\t\tpromptDataset: huggingface.New(\n\t\t\thuggingface.DatasetWithRepo("wikitext"),\n\t\t\thuggingface.DatasetWithSubset("wikitext-2-raw-v1"),\n\t\t\thuggingface.DatasetWithSplit("test"),\n\t\t\thuggingface.DatasetWithSamples(10),\n\t\t\thuggingface.DatasetWithTextColumn("text"),\n\t\t),\n\t\t// Baseline''')
s=s.replace('''\tfor sample := range experiment.dataset.Generate() {''','''\tfor sample := range experiment.promptDataset.Generate() {''')
p.write_text(s)
PY

gofmt -w experiment/task/textgen/outofcorpus.go
sed -n '20,75p' experiment/task/textgen/outofcorpus.go

This is a genuine out-of-distribution test: the test split of wikitext-2
has no overlap with the training split at the sample level.
*/
type OutOfCorpusExperiment struct {
	tableData     []tools.ExperimentalData
	dataset       data.Provider
	promptDataset data.Provider
	prompt        []string
	holdouts      [][]byte
	evaluator     *tools.Evaluator
}

func NewOutOfCorpusExperiment() *OutOfCorpusExperiment {
	return &OutOfCorpusExperiment{
		tableData: []tools.ExperimentalData{},
		dataset: huggingface.New(
			huggingface.DatasetWithRepo("wikitext"),
			huggingface.DatasetWithSubset("wikitext-2-raw-v1"),
			huggingface.DatasetWithSplit("train"),
			huggingface.DatasetWithSamples(10),
			huggingface.DatasetWithTextColumn("text"),
		),
		promptDataset: huggingface.New(
			huggingface.DatasetWithRepo("wikitext"),
			huggingface.DatasetWithSubset("wikitext-2-raw-v1"),
			huggingface.DatasetWithSplit("test"),
			huggingface.DatasetWithSamples(10),
			huggingface.DatasetWithTextColumn("text"),
		),
		// Baseline 0.05: any partial byte overlap with the held-out suffix
		// is evidence that the substrate encodes structural regularity.
		// Target 0.40: strong OOD generalization from byte patterns.
		evaluator: tools.NewEvaluator(
			tools.EvalWithExpectation(0.05, 0.40),
		),
	}
}

func (experiment *OutOfCorpusExperiment) Name() string           { return "Out of Corpus" }
func (experiment *OutOfCorpusExperiment) Section() string        { return "textgen" }
func (experiment *OutOfCorpusExperiment) Dataset() data.Provider { return experiment.dataset }

func (experiment *OutOfCorpusExperiment) Prompts() []string {
	experiment.prompt = experiment.prompt[:0]
	experiment.holdouts = experiment.holdouts[:0]
	for sample := range experiment.promptDataset.Generate() {
		task := string(sample.TaskPrompt())
		if len(task) < 8 {
			continue
		}

		prefix, hold := tools.BytePrefixFraction(task, 0.5)
		if hold == "" {
			continue
		}
Inspectin

---

bash -lc sed -n '1,130p' experiment/task/textgen/outofcorpus.go

package textgen

import (
	gc "github.com/smartystreets/goconvey/convey"
	tools "github.com/theapemachine/six/experiment"
	"github.com/theapemachine/six/experiment/data"
	"github.com/theapemachine/six/experiment/data/huggingface"
)

/*
OutOfCorpusExperiment tests whether the substrate can reconstruct text that
was not verbatim in its training corpus, by generalising from the structural
patterns of related ingested text.

Dataset: wikitext-2 (raw). After ingesting the first 10 samples of the
training split, the system is tested on the last 50% of each test sample —
content that was never in the substrate. The task requires bridging from the
prompt's structural fingerprint into the attractor field built from the
training corpus.

This is a genuine out-of-distribution test: the test split of wikitext-2
has no overlap with the training split at the sample level.
*/
type OutOfCorpusExperiment struct {
	tableData []tools.ExperimentalData
	dataset   data.Provider
	prompt    []string
	holdouts  [][]byte
	evaluator *tools.Evaluator
}

func NewOutOfCorpusExperiment() *OutOfCorpusExperiment {
	return &OutOfCorpusExperiment{
		tableData: []tools.ExperimentalData{},
		dataset: huggingface.New(
			huggingface.DatasetWithRepo("wikitext"),
			huggingface.DatasetWithSubset("wikitext-2-raw-v1"),
			huggingface.DatasetWithSamples(10),
			huggingface.DatasetWithTextColumn("text"),
		),
		// Baseline 0.05: any partial byte overlap with the held-out suffix
		// is evidence that the substrate encodes structural regularity.
		// Target 0.40: strong OOD generalization from byte patterns.
		evaluator: tools.NewEvaluator(
			tools.EvalWithExpectation(0.05, 0.40),
		),
	}
}

func (experiment *OutOfCorpusExperiment) Name() string           { return "Out of Corpus" }
func (experiment *OutOfCorpusExperiment) Section() string        { return "textgen" }
func (experiment *OutOfCorpusExperiment) Dataset() data.Provider { return experiment.dataset }

func (experiment *OutOfCorpusExperiment) Prompts() []string {
	experiment.prompt = experiment.prompt[:0]
	experiment.holdouts = experiment.holdouts[:0]
	for sample := range experiment.dataset.Generate() {
		task := string(sample.TaskPrompt())
		if len(task) < 8 {
			continue
		}

		prefix, hold := tools.BytePrefixFraction(task, 0.5)
		if hold == "" {
			continue
		}

		experiment.prompt = append(experiment.prompt, prefix)
		experiment.holdouts = append(experiment.holdouts, []byte(hold))
	}

	return experiment.prompt
}

func (experiment *OutOfCorpusExperiment) HoldoutForPrompt(idx int) ([]byte, bool) {
	if idx < 0 || idx >= len(experiment.holdouts) {
		return nil, false
	}
	return experiment.holdouts[idx], true
}

func (experiment *OutOfCorpusExperiment) AddResult(results tools.ExperimentalData) {
	experiment.evaluator.Enrich(&results)
	experiment.tableData = append(experiment.tableData, results)
}

func (experiment *OutOfCorpusExperiment) Outcome() (any, gc.Assertion, any) {
	return experiment.evaluator.Outcome(experiment.Score())
}

func (experiment *OutOfCorpusExperiment) OutcomeForPrompt(idx int) (any, gc.Assertion, any) {
	return experiment.evaluator.OutcomeForPromptConvey(experiment.tableData, idx)
}

func (experiment *OutOfCorpusExperiment) Score() float64 {
	return experiment.evaluator.MeanScore(experiment.tableData)
}

func (experiment *OutOfCorpusExperiment) TableData() any { return experiment.tableData }

func (experiment *OutOfCorpusExperiment) Artifacts() []tools.Artifact {
	return OutOfCorpusArtifacts(experiment.tableData, experiment.Score())
}



Clarifying dataset splits for better structure
I need to patch the promptDataset to ensure its test split is separate from the train split in the dataset. The default

---

bash -lc ls experiment/task/textgen && rg "OutOfCorpus" -n experiment/task/textgen

compositional.go
outofcorpus.go
prose_chaining.go
text_overlap.go
textgen_artifacts.go
experiment/task/textgen/textgen_artifacts.go:116:// ── OutOfCorpus ────────────────────────────────────────────────────────────────
experiment/task/textgen/textgen_artifacts.go:118:func OutOfCorpusArtifacts(tableData []tools.ExperimentalData, score float64) []tools.Artifact {
experiment/task/textgen/outofcorpus.go:11:OutOfCorpusExperiment tests whether the substrate can reconstruct text that
experiment/task/textgen/outofcorpus.go:24:type OutOfCorpusExperiment struct {
experiment/task/textgen/outofcorpus.go:32:func NewOutOfCorpusExperiment() *OutOfCorpusExperiment {
experiment/task/textgen/outofcorpus.go:33:	return &OutOfCorpusExperiment{
experiment/task/textgen/outofcorpus.go:50:func (experiment *OutOfCorpusExperiment) Name() string           { return "Out of Corpus" }
experiment/task/textgen/outofcorpus.go:51:func (experiment *OutOfCorpusExperiment) Section() string        { return "textgen" }
experiment/task/textgen/outofcorpus.go:52:func (experiment *OutOfCorpusExperiment) Dataset() data.Provider { return experiment.dataset }
experiment/task/textgen/outofcorpus.go:54:func (experiment *OutOfCorpusExperiment) Prompts() []string {
experiment/task/textgen/outofcorpus.go:75:func (experiment *OutOfCorpusExperiment) HoldoutForPrompt(idx int) ([]byte, bool) {
experiment/task/textgen/outofcorpus.go:82:func (experiment *OutOfCorpusExperiment) AddResult(results tools.ExperimentalData) {
experiment/task/textgen/outofcorpus.go:87:func (experiment *OutOfCorpusExperiment) Outcome() (any, gc.Assertion, any) {
experiment/task/textgen/outofcorpus.go:91:func (experiment *OutOfCorpusExperiment) OutcomeForPrompt(idx int) (any, gc.Assertion, any) {
experiment/task/textgen/outofcorpus.go:95:func (experiment *OutOfCorpusExperiment) Score() float64 {
experiment/task/textgen/outofcorpus.go:99:func (experiment *OutOfCorpusExperiment) TableData() any { return experiment.tableData }
experiment/task/textgen/outofcorpus.go:101:func (experiment *OutOfCorpusExperiment) Artifacts() []tools.Artifact {
experiment/task/textgen/outofcorpus.go:102:	return OutOfCorpusArtifacts(experiment.tableData, experiment.Score())
Extracting lines 1 to 130 from file

---

bash -lc python3 - <<'PY'
from pathlib import Path
# text.go
p=Path('experiment/task/classification/text.go')
s=p.read_text()
s=s.replace('''\ttableData           []tools.ExperimentalData\n\tdataset             data.Provider\n\tprompt              []string''','''\ttableData           []tools.ExperimentalData\n\tdataset             data.Provider\n\tpromptDataset       data.Provider\n\tprompt              []string''')
s=s.replace('''\t\tdataset: huggingface.New(\n\t\t\thuggingface.DatasetWithRepo("sh0416/ag_news"),\n\t\t\thuggingface.DatasetWithSamples(samples),\n\t\t\thuggingface.DatasetWithSplit("test"),\n\t\t\thuggingface.DatasetWithTextColumns("title", "description"),\n\t\t\thuggingface.DatasetWithLabelColumn("label"),\n\t\t\thuggingface.DatasetWithLabelAppend(agNewsLabels),\n\t\t),\n\t\tevaluator: tools.NewEvaluator(''','''\t\tdataset: huggingface.New(\n\t\t\thuggingface.DatasetWithRepo("sh0416/ag_news"),\n\t\t\thuggingface.DatasetWithSamples(samples),\n\t\t\thuggingface.DatasetWithSplit("train"),\n\t\t\thuggingface.DatasetWithTextColumns("title", "description"),\n\t\t\thuggingface.DatasetWithLabelColumn("label"),\n\t\t\thuggingface.DatasetWithLabelAppend(agNewsLabels),\n\t\t),\n\t\tpromptDataset: huggingface.New(\n\t\t\thuggingface.DatasetWithRepo("sh0416/ag_news"),\n\t\t\thuggingface.DatasetWithSamples(samples),\n\t\t\thuggingface.DatasetWithSplit("test"),\n\t\t\thuggingface.DatasetWithTextColumns("title", "description"),\n\t\t\thuggingface.DatasetWithLabelColumn("label"),\n\t\t\thuggingface.DatasetWithLabelAppend(agNewsLabels),\n\t\t),\n\t\tevaluator: tools.NewEvaluator(''')
s=s.replace('''\tfor sample := range experiment.dataset.Generate() {''','''\tfor sample := range experiment.promptDataset.Generate() {''',1)
p.write_text(s)
# blind.go
p=Path('experiment/task/classification/blind.go')
s=p.read_text()
s=s.replace('''\ttableData []tools.ExperimentalData\n\tdataset   data.Provider\n\tprompt    []string''','''\ttableData     []tools.ExperimentalData\n\tdataset       data.Provider\n\tpromptDataset data.Provider\n\tprompt        []string''')
s=s.replace('''\t\tdataset: huggingface.New(\n\t\t\thuggingface.DatasetWithRepo("sh0416/ag_news"),\n\t\t\thuggingface.DatasetWithSamples(samples),\n\t\t\thuggingface.DatasetWithSplit("test"),\n\t\t\thuggingface.DatasetWithTextColumns("title", "description"),\n\t\t\thuggingface.DatasetWithLabelColumn("label"),\n\t\t),\n\t\tevaluator: tools.NewEvaluator(''','''\t\tdataset: huggingface.New(\n\t\t\thuggingface.DatasetWithRepo("sh0416/ag_news"),\n\t\t\thuggingface.DatasetWithSamples(samples),\n\t\t\thuggingface.DatasetWithSplit("train"),\n\t\t\thuggingface.DatasetWithTextColumns("title", "description"),\n\t\t\thuggingface.DatasetWithLabelColumn("label"),\n\t\t),\n\t\tpromptDataset: huggingface.New(\n\t\t\thuggingface.DatasetWithRepo("sh0416/ag_news"),\n\t\t\thuggingface.DatasetWithSamples(samples),\n\t\t\thuggingface.DatasetWithSplit("test"),\n\t\t\thuggingface.DatasetWithTextColumns("title", "description"),\n\t\t\thuggingface.DatasetWithLabelColumn("label"),\n\t\t),\n\t\tevaluator: tools.NewEvaluator(''')
s=s.replace('''\tfor sample := range experiment.dataset.Generate() {''','''\tfor sample := range experiment.promptDataset.Generate() {''',1)
p.write_text(s)
PY

gofmt -w experiment/task/classification/text.go experiment/task/classification/blind.go
sed -n '30,95p' experiment/task/classification/text.go && sed -n '20,95p' experiment/task/classification/blind.go

category, and be able to classify articles into categories it has never
seen before.
*/
type TextClassificationExperiment struct {
	tableData           []tools.ExperimentalData
	dataset             data.Provider
	promptDataset       data.Provider
	prompt              []string
	holdouts            [][]byte
	evaluator           *tools.Evaluator
	predictionsComputed bool
}

func NewTextClassificationExperiment() *TextClassificationExperiment {
	experiment := &TextClassificationExperiment{
		tableData: []tools.ExperimentalData{},
		dataset: huggingface.New(
			huggingface.DatasetWithRepo("sh0416/ag_news"),
			huggingface.DatasetWithSamples(samples),
			huggingface.DatasetWithSplit("train"),
			huggingface.DatasetWithTextColumns("title", "description"),
			huggingface.DatasetWithLabelColumn("label"),
			huggingface.DatasetWithLabelAppend(agNewsLabels),
		),
		promptDataset: huggingface.New(
			huggingface.DatasetWithRepo("sh0416/ag_news"),
			huggingface.DatasetWithSamples(samples),
			huggingface.DatasetWithSplit("test"),
			huggingface.DatasetWithTextColumns("title", "description"),
			huggingface.DatasetWithLabelColumn("label"),
			huggingface.DatasetWithLabelAppend(agNewsLabels),
		),
		evaluator: tools.NewEvaluator(
			tools.EvalWithLabels(agNewsLabels),
			tools.EvalWithFixedExpectation(0.30, 0.85),
		),
	}

	return experiment
}

func (experiment *TextClassificationExperiment) ClassLabels() []string {
	return agNewsLabels
}

func (experiment *TextClassificationExperiment) Name() string {
	return "Text Classification"
}

func (experiment *TextClassificationExperiment) Section() string {
	return "classification"
}

func (experiment *TextClassificationExperiment) Dataset() data.Provider {
	return experiment.dataset
}

// Prompts builds one prompt per structured sample: TaskPrompt is the article
// (no classification suffix in the prompt string). Holdout keeps the gold
// label bytes for scoring.
func (experiment *TextClassificationExperiment) Prompts() []string {
	experiment.prompt = experiment.prompt[:0]
	experiment.holdouts = experiment.holdouts[:0]
	for sample := range experiment.promptDataset.Generate() {
		task := string(sample.TaskPrompt())
		if task == "" {
The intuition is that if we give the system enough news articles, and
ask it to assign each article to one of N categories, there is a chance
that it would be able to pick up on the "domain knowledge" of each
category, and be able to classify articles into categories it has never
seen before.
*/
type BlindClassificationExperiment struct {
	tableData     []tools.ExperimentalData
	dataset       data.Provider
	promptDataset data.Provider
	prompt        []string
	holdouts      [][]byte
	evaluator     *tools.Evaluator
}

var samples = 100

func NewBlindClassificationExperiment() *BlindClassificationExperiment {
	experiment := &BlindClassificationExperiment{
		tableData: []tools.ExperimentalData{},
		dataset: huggingface.New(
			huggingface.DatasetWithRepo("sh0416/ag_news"),
			huggingface.DatasetWithSamples(samples),
			huggingface.DatasetWithSplit("train"),
			huggingface.DatasetWithTextColumns("title", "description"),
			huggingface.DatasetWithLabelColumn("label"),
		),
		promptDataset: huggingface.New(
			huggingface.DatasetWithRepo("sh0416/ag_news"),
			huggingface.DatasetWithSamples(samples),
			huggingface.DatasetWithSplit("test"),
			huggingface.DatasetWithTextColumns("title", "description"),
			huggingface.DatasetWithLabelColumn("label"),
		),
		evaluator: tools.NewEvaluator(
			tools.EvalWithLabels(agNewsLabels),
			tools.EvalWithExpectation(0.05, 0.50),
		),
	}

	return experiment
}

func (experiment *BlindClassificationExperiment) ClassLabels() []string {
	return agNewsLabels
}

func (experiment *BlindClassificationExperiment) Name() string {
	return "Blind Text Classification"
}

func (experiment *BlindClassificationExperiment) Section() string {
	return "blind classification"
}

func (experiment *BlindClassificationExperiment) Dataset() data.Provider {
	return experiment.dataset
}

func (experiment *BlindClassificationExperiment) Prompts() []string {
	experiment.prompt = experiment.prompt[:0]
	experiment.holdouts = experiment.holdouts[:0]
	for sample := range experiment.promptDataset.Generate() {
		task := string(sample.TaskPrompt())
		if task == "" {
			continue
		}

		experiment.prompt = append(experiment.prompt, task)

		var ho []byte
		if len(sample.Label) > 0 {
			ho = sample.Label
		}

		experiment.holdouts = append(experiment.holdouts, ho)
Extracting out-of-corpus data

---

bash -lc sed -n '95,150p' experiment/task/classification/blind.go

return nil, false
}
return experiment.holdouts[idx], true
}

func (experiment *BlindClassificationExperiment) AddResult(results tools.ExperimentalData) {
if normalized, ok := normalizeClassificationLabelText(results.Holdout, experiment.ClassLabels()); ok {
	results.TrueLabel = tools.OptionalLabel(normalized)
} else if dataset, ok := experiment.dataset.(*huggingface.Dataset); ok {
	if label, ok := dataset.LabelForSample(uint32(results.Idx)); ok {
		if normalized, ok := normalizeClassificationLabelIndex(label, experiment.ClassLabels()); ok {
			results.TrueLabel = tools.OptionalLabel(normalized)
		}
	}
}

if results.PredLabel == nil && len(results.Classification) > 0 {
	normalizedClass := strings.ToLower(strings.TrimSpace(string(results.Classification)))

	if idx, err := strconv.Atoi(normalizedClass); err == nil {
		if normalized, ok := normalizeClassificationLabelIndex(idx, experiment.ClassLabels()); ok {
			results.PredLabel = tools.OptionalLabel(normalized)
		}
	} else {
		for i, label := range experiment.ClassLabels() {
			if normalizedClass == label {
				results.PredLabel = tools.OptionalLabel(i)
				break
			}
		}
	}
}

if results.PredLabel != nil && len(results.Classification) > 0 {
	experiment.evaluator.Enrich(&results)
} else {
	results.Scores = tools.Scores{}
	results.WeightedTotal = 0
}

experiment.tableData = append(experiment.tableData, results)
}

/*
Outcome delegates to the Evaluator which holds the real expectation
thresholds. Baseline = 0.05 (barely above noise for blind task),
Target = 0.50 (strong unsupervised clustering).
*/
func (experiment *BlindClassificationExperiment) Outcome() (
any, Assertion, any,
) {
return experiment.evaluator.Outcome(experiment.Score())
}

func (experiment *BlindClassificationExperiment) OutcomeForPrompt(idx int) (any, Assertion, any) {
return experiment.evaluator.OutcomeForPromptConvey(experiment.tableData, idx)

---

bash -lc python3 - <<'PY'
from pathlib import Path
for f in ['experiment/task/classification/text.go','experiment/task/classification/blind.go']:
    p=Path(f); s=p.read_text()
    s=s.replace('''\t\tif idx, err := strconv.Atoi(normalizedClass); err == nil {\n\t\t\tif idx >= 0 && idx < len(experiment.ClassLabels()) {\n\t\t\t\tresults.PredLabel = tools.OptionalLabel(idx)\n\t\t\t}\n\t\t} else {''','''\t\tif idx, err := strconv.Atoi(normalizedClass); err == nil {\n\t\t\tif normalized, ok := normalizeClassificationLabelIndex(idx, experiment.ClassLabels()); ok {\n\t\t\t\tresults.PredLabel = tools.OptionalLabel(normalized)\n\t\t\t}\n\t\t} else {''')
    p.write_text(s)
PY

gofmt -w experiment/task/classification/text.go experiment/task/classification/blind.go
sed -n '120,150p' experiment/task/classification/text.go && sed -n '120,155p' experiment/task/classification/blind.go

if results.PredLabel == nil && len(results.Classification) > 0 {
	normalizedClass := strings.ToLower(strings.TrimSpace(string(results.Classification)))

	if idx, err := strconv.Atoi(normalizedClass); err == nil {
		if normalized, ok := normalizeClassificationLabelIndex(idx, experiment.ClassLabels()); ok {
			results.PredLabel = tools.OptionalLabel(normalized)
		}
	} else {
		for i, label := range experiment.ClassLabels() {
			if strings.Contains(normalizedClass, label) {
				results.PredLabel = tools.OptionalLabel(i)
				break
			}
		}
	}
}

if results.PredLabel != nil && len(results.Classification) > 0 {
	experiment.evaluator.Enrich(&results)
} else {
	results.Scores = tools.Scores{}
	results.WeightedTotal = 0
}

experiment.predictionsComputed = false
experiment.tableData = append(experiment.tableData, results)
}

func (experiment *TextClassificationExperiment) ensurePredictions() {
if experiment.predictionsComputed {
			if normalizedClass == label {
				results.PredLabel = tools.OptionalLabel(i)
				break
			}
		}
	}
}

if results.PredLabel != nil && len(results.Classification) > 0 {
	experiment.evaluator.Enrich(&results)
} else {
	results.Scores = tools.Scores{}
	results.WeightedTotal = 0
}

experiment.tableData = append(experiment.tableData, results)
}

/*
Outcome delegates to the Evaluator which holds the real expectation
thresholds. Baseline = 0.05 (barely above noise for blind task),
Target = 0.50 (strong unsupervised clustering).
*/
func (experiment *BlindClassificationExperiment) Outcome() (
any, Assertion, any,
) {
return experiment.evaluator.Outcome(experiment.Score())
}

func (experiment *BlindClassificationExperiment) OutcomeForPrompt(idx int) (any, Assertion, any) {
return experiment.evaluator.OutcomeForPromptConvey(experiment.tableData, idx)
}

func (experiment *BlindClassificationExperiment) Score() float64 {
n := len(experiment.tableData)
if n == 0 {

---

bash -lc sed -n '110,170p' experiment/task/classification/text.go && sed -n '1,25p' experiment/task/classification/text.go

func (experiment *TextClassificationExperiment) AddResult(results tools.ExperimentalData) {
	if normalized, ok := normalizeClassificationLabelText(results.Holdout, experiment.ClassLabels()); ok {
		results.TrueLabel = tools.OptionalLabel(normalized)
	} else if dataset, ok := experiment.dataset.(*huggingface.Dataset); ok {
		if label, ok := dataset.LabelForSample(uint32(results.Idx)); ok {
			if normalized, ok := normalizeClassificationLabelIndex(label, experiment.ClassLabels()); ok {
				results.TrueLabel = tools.OptionalLabel(normalized)
			}
		}
	}

	if results.PredLabel == nil && len(results.Classification) > 0 {
		normalizedClass := strings.ToLower(strings.TrimSpace(string(results.Classification)))

		if idx, err := strconv.Atoi(normalizedClass); err == nil {
			if idx >= 0 && idx < len(experiment.ClassLabels()) {
				results.PredLabel = tools.OptionalLabel(idx)
			}
		} else {
			for i, label := range experiment.ClassLabels() {
				if strings.Contains(normalizedClass, label) {
					results.PredLabel = tools.OptionalLabel(i)
					break
				}
			}
		}
	}

	if results.PredLabel != nil && len(results.Classification) > 0 {
		experiment.evaluator.Enrich(&results)
	} else {
		results.Scores = tools.Scores{}
		results.WeightedTotal = 0
	}

	experiment.predictionsComputed = false
	experiment.tableData = append(experiment.tableData, results)
}

func (experiment *TextClassificationExperiment) ensurePredictions() {
	if experiment.predictionsComputed {
		return
	}

	for idx := range experiment.tableData {
		row := &experiment.tableData[idx]

		if row.PredLabel != nil && len(row.Classification) > 0 {
			experiment.evaluator.Enrich(row)
		} else {
			row.Scores = tools.Scores{}
			row.WeightedTotal = 0
		}
	}

	experiment.predictionsComputed = true
}

/*
ComputePredictions re-materializes per-row class hypotheses from generation
and beam continuations, matching ensurePredictions without the cached flag gate.
package classification

import (
	"fmt"
	"strconv"
	"strings"

	. "github.com/smartystreets/goconvey/convey"
	tools "github.com/theapemachine/six/experiment"
	"github.com/theapemachine/six/experiment/data"
	"github.com/theapemachine/six/experiment/data/huggingface"
	"github.com/theapemachine/six/experiment/projector"
)

var _ tools.HoldoutProvider = (*TextClassificationExperiment)(nil)

// ag_news label indices → human readable names
var agNewsLabels = []string{"world", "sports", "business", "sci_tech"}

/*
TextClassificationExperiment tests the ability of the system to classify
news articles into topical categories, using a dataset of news articles.
The minimal honest version uses the included labels, which span 4 categories,
however it could be an additional test to see if the system can classify
articles into more granular categories, without having ever seen the
*/

---

bash -lc python3 - <<'PY'
from pathlib import Path
for filename in ['experiment/task/classification/text.go','experiment/task/classification/blind.go']:
    p=Path(filename)
    s=p.read_text()
    old='''func (experiment *TextClassificationExperiment) AddResult(results tools.ExperimentalData) {\n\tif dataset, ok := experiment.dataset.(*huggingface.Dataset); ok {\n\t\tif label, ok := dataset.LabelForSample(uint32(results.Idx)); ok {\n\t\t\tif normalized, ok := normalizeClassificationLabelIndex(label, experiment.ClassLabels()); ok {\n\t\t\t\tresults.TrueLabel = tools.OptionalLabel(normalized)\n\t\t\t}\n\t\t}\n\t}\n'''
    new='''func (experiment *TextClassificationExperiment) AddResult(results tools.ExperimentalData) {\n\tif normalized, ok := normalizeClassificationLabelText(results.Holdout, experiment.ClassLabels()); ok {\n\t\tresults.TrueLabel = tools.OptionalLabel(normalized)\n\t} else if dataset, ok := experiment.dataset.(*huggingface.Dataset); ok {\n\t\tif label, ok := dataset.LabelForSample(uint32(results.Idx)); ok {\n\t\t\tif normalized, ok := normalizeClassificationLabelIndex(label, experiment.ClassLabels()); ok {\n\t\t\t\tresults.TrueLabel = tools.OptionalLabel(normalized)\n\t\t\t}\n\t\t}\n\t}\n'''
    if old in s:
        s=s.replace(old,new)
    old2='''func (experiment *BlindClassificationExperiment) AddResult(results tools.ExperimentalData) {\n\tif dataset, ok := experiment.dataset.(*huggingface.Dataset); ok {\n\t\tif label, ok := dataset.LabelForSample(uint32(results.Idx)); ok {\n\t\t\tif normalized, ok := normalizeClassificationLabelIndex(label, experiment.ClassLabels()); ok {\n\t\t\t\tresults.TrueLabel = tools.OptionalLabel(normalized)\n\t\t\t}\n\t\t}\n\t}\n'''
    new2='''func (experiment *BlindClassificationExperiment) AddResult(results tools.ExperimentalData) {\n\tif normalized, ok := normalizeClassificationLabelText(results.Holdout, experiment.ClassLabels()); ok {\n\t\tresults.TrueLabel = tools.OptionalLabel(normalized)\n\t} else if dataset, ok := experiment.dataset.(*huggingface.Dataset); ok {\n\t\tif label, ok := dataset.LabelForSample(uint32(results.Idx)); ok {\n\t\t\tif normalized, ok := normalizeClassificationLabelIndex(label, experiment.ClassLabels()); ok {\n\t\t\t\tresults.TrueLabel = tools.OptionalLabel(normalized)\n\t\t\t}\n\t\t}\n\t}\n'''
    if old2 in s:
        s=s.replace(old2,new2)
    # Patch blind Enrich unconditional
    s=s.replace('''\n\texperiment.evaluator.Enrich(&results)\n\texperiment.tableData = append(experiment.tableData, results)\n}\n''','''\n\tif results.PredLabel != nil && len(results.Classification) > 0 {\n\t\texperiment.evaluator.Enrich(&results)\n\t} else {\n\t\tresults.Scores = tools.Scores{}\n\t\tresults.WeightedTotal = 0\n\t}\n\n\texperiment.tableData = append(experiment.tableData, results)\n}\n''',1 if filename.endswith('blind.go') else 0)
    p.write_text(s)
PY

gofmt -w experiment/task/classification/text.go experiment/task/classification/blind.go
sed -n '75,140p' experiment/task/classification/text.go && sed -n '75,145p' experiment/task/classification/blind.go

	return experiment.dataset
}

// Prompts builds one prompt per structured sample: TaskPrompt is the article
// (no classification suffix in the prompt string). Holdout keeps the gold
// label bytes for scoring.
func (experiment *TextClassificationExperiment) Prompts() []string {
	experiment.prompt = experiment.prompt[:0]
	experiment.holdouts = experiment.holdouts[:0]
	for sample := range experiment.dataset.Generate() {
		task := string(sample.TaskPrompt())
		if task == "" {
			continue
		}

		experiment.prompt = append(experiment.prompt, task)

		var ho []byte
		if len(sample.Label) > 0 {
			ho = sample.Label
		}

		experiment.holdouts = append(experiment.holdouts, ho)
	}

	return experiment.prompt
}

func (experiment *TextClassificationExperiment) HoldoutForPrompt(idx int) ([]byte, bool) {
	if idx < 0 || idx >= len(experiment.holdouts) {
		return nil, false
	}
	return experiment.holdouts[idx], true
}

func (experiment *TextClassificationExperiment) AddResult(results tools.ExperimentalData) {
	if normalized, ok := normalizeClassificationLabelText(results.Holdout, experiment.ClassLabels()); ok {
		results.TrueLabel = tools.OptionalLabel(normalized)
	} else if dataset, ok := experiment.dataset.(*huggingface.Dataset); ok {
		if label, ok := dataset.LabelForSample(uint32(results.Idx)); ok {
			if normalized, ok := normalizeClassificationLabelIndex(label, experiment.ClassLabels()); ok {
				results.TrueLabel = tools.OptionalLabel(normalized)
			}
		}
	}

	if results.PredLabel == nil && len(results.Classification) > 0 {
		normalizedClass := strings.ToLower(strings.TrimSpace(string(results.Classification)))

		if idx, err := strconv.Atoi(normalizedClass); err == nil {
			if idx >= 0 && idx < len(experiment.ClassLabels()) {
				results.PredLabel = tools.OptionalLabel(idx)
			}
		} else {
			for i, label := range experiment.ClassLabels() {
				if strings.Contains(normalizedClass, label) {
					results.PredLabel = tools.OptionalLabel(i)
					break
				}
			}
		}
	}

	if results.PredLabel != nil && len(results.Classification) > 0 {
		experiment.evaluator.Enrich(&results)
	} else {
		task := string(sample.TaskPrompt())
		if task == "" {
			continue
		}

		experiment.prompt = append(experiment.prompt, task)

		var ho []byte
		if len(sample.Label) > 0 {
			ho = sample.Label
		}

		experiment.holdouts = append(experiment.holdouts, ho)
	}

	return experiment.prompt
}

func (experiment *BlindClassificationExperiment) HoldoutForPrompt(idx int) ([]byte, bool) {
	if idx < 0 || idx >= len(experiment.holdouts) {
		return nil, false
	}
	return experiment.holdouts[idx], true
}

func (experiment *BlindClassificationExperiment) AddResult(results tools.ExperimentalData) {
	if normalized, ok := normalizeClassificationLabelText(results.Holdout, experiment.ClassLabels()); ok {
		results.TrueLabel = tools.OptionalLabel(normalized)
	} else if dataset, ok := experiment.dataset.(*huggingface.Dataset); ok {
		if label, ok := dataset.LabelForSample(uint32(results.Idx)); ok {
			if normalized, ok := normalizeClassificationLabelIndex(label, experiment.ClassLabels()); ok {
				results.TrueLabel = tools.OptionalLabel(normalized)
			}
		}
	}

	if results.PredLabel == nil && len(results.Classification) > 0 {
		normalizedClass := strings.ToLower(strings.TrimSpace(string(results.Classification)))

		if idx, err := strconv.Atoi(normalizedClass); err == nil {
			if idx >= 0 && idx < len(experiment.ClassLabels()) {
				results.PredLabel = tools.OptionalLabel(idx)
			}
		} else {
			for i, label := range experiment.ClassLabels() {
				if normalizedClass == label {
					results.PredLabel = tools.OptionalLabel(i)
					break
				}
			}
		}
	}

	if results.PredLabel != nil && len(results.Classification) > 0 {
		experiment.evaluator.Enrich(&results)
	} else {
		results.Scores = tools.Scores{}
		results.WeightedTotal = 0
	}

	experiment.tableData = append(experiment.tableData, results)
}

/*
Outcome delegates to the Evaluator which holds the real expectation
thresholds. Baseline = 0.05 (barely above noise for blind task),
Target = 0.50 (strong unsupervised clustering).
*/
func (experiment *BlindClassificationExperiment) Outcome() (
	any, Assertion, any,
) {

---

