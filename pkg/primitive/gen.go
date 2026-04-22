//go:build ignore
// +build ignore

/*
gen.go is the single source of truth for the Value layout AND the
program signatures across every language consumer. It loads the YAML at
cmd/cfg/config.yml once and renders the following sibling files from one
in-memory region table + a compile pass over the `programs:` block:

  - pkg/compute/kernel/shared/primitives.h    (CUDA / Metal kernels)
  - pkg/primitive/layout_generated.go         (Go runtime + array sizes)
  - visualizer/src/lib/layoutGenerated.ts     (TS visualizer: regions)
  - visualizer/src/lib/propertiesGenerated.ts (TS visualizer: property
    type offsets — mirrors PropertyType iota in pkg/primitive/properties.go;
    when properties are added there, update propertyTypes below in lockstep)
  - visualizer/src/lib/programsGenerated.ts   (TS visualizer: program
    signatures compiled from the YAML's `programs:` block, so the program
    classifier never carries a hand-maintained signature table)

If any consumer needs a layout constant or a program signature, it must
read it from one of these files. New consumers don't widen this generator;
they pick the language target that already exists. Updates land in
config.yml (or properties.go for property types) only; re-running
`go generate ./pkg/primitive/...` keeps every output in lockstep.
*/
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/viper"

	"github.com/theapemachine/six/pkg/compute/program"
)

// region is the canonical name of a Value sub-region together with the
// identifier stems each language target uses. Order matters: it is the
// emit order across all three outputs and the iteration order of the
// generated REGION_SPECS / RegionSpecs slices.
type region struct {
	yamlKey   string // value.region.<yamlKey>
	macroStem string // C macro stem: TOKENS_START_WORD etc.
	goStem    string // Go identifier stem: TokensStartWord etc.
	tsStem    string // TS lowerCamel stem used in REGION_SPECS entries
}

var regions = []region{
	{"tokens", "TOKENS", "Tokens", "tokens"},
	{"program", "PROGRAM", "Program", "program"},
	{"signals", "SIGNALS", "Signals", "signals"},
	{"context", "CONTEXT", "Context", "context"},
	{"gradient", "GRADIENT", "Gradient", "gradient"},
	{"properties", "PROPERTIES", "Properties", "properties"},
	{"asset", "ASSET", "Asset", "asset"},
	{"prev", "PREV", "Prev", "prev"},
	{"next", "NEXT", "Next", "next"},
	{"id", "ID", "ID", "id"},
	{"affinity", "AFFINITY", "Affinity", "affinity"},
}

// propertyTypes mirrors pkg/primitive/properties.go's PropertyType iota in
// declaration order. The offset of each entry is its slot inside the
// properties region (PROPERTIES_START_WORD + index). Adding a property in
// properties.go REQUIRES adding it here too — there is no automatic linkage
// because PropertyType is a plain Go iota with method receivers, not a
// schema. The TS-side test suite asserts the visualiser sees the same
// names, so a forgotten append trips a runtime assertion.
var propertyTypes = []string{
	"LABELS",
	"CONFIDENCE",
	"EPOCH",
	"TTL",
	"NOISE",
	"STATUS",
	"WINDOW",
	"DEPTH",
	"COMMUNITY",
	"TARGET",
	"ROLE",
	"REFERENCE",
	"EMIT",
	"SURPRISAL",
}

// valueRoles mirrors ValueRole in properties.go — same drift contract as
// propertyTypes above. Used by the visualiser to interpret the ROLE
// property word without re-hardcoding numeric sentinels.
var valueRoles = []string{
	"None",
	"Programmer",
	"Learner",
	"Readout",
	"Association",
	"Prompt",
}

// resolved is the per-region snapshot the renderers consume.
type resolved struct {
	region
	start        int
	bits         uint64
	words        int
	lastWordMask uint64
}

func resolveConfigPath(configFlag string) string {
	if strings.TrimSpace(configFlag) != "" {
		return filepath.Clean(configFlag)
	}
	if env := strings.TrimSpace(os.Getenv("CONFIG_PATH")); env != "" {
		return filepath.Clean(env)
	}
	_, file, _, ok := runtime.Caller(0)
	if ok {
		return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "cmd", "cfg", "config.yml"))
	}
	return filepath.Clean(filepath.Join("..", "..", "cmd", "cfg", "config.yml"))
}

func main() {
	configPath := flag.String("config", "", "path to config.yml")
	flag.Parse()

	cfgFile := resolveConfigPath(*configPath)
	viper.SetConfigFile(cfgFile)
	if err := viper.ReadInConfig(); err != nil {
		die("read config %q: %v", cfgFile, err)
	}

	required := []string{"value.words"}
	for _, r := range regions {
		required = append(required,
			fmt.Sprintf("value.region.%s.start", r.yamlKey),
			fmt.Sprintf("value.region.%s.bits", r.yamlKey),
		)
	}
	for _, k := range required {
		if !viper.IsSet(k) {
			die("required config key missing: %q", k)
		}
	}

	totalWords := viper.GetInt("value.words")
	totalBytes := totalWords * 8
	numRotations := 16
	if viper.IsSet("value.num_rotations") {
		numRotations = viper.GetInt("value.num_rotations")
		if numRotations <= 0 {
			die("value.num_rotations must be positive, got %d", numRotations)
		}
	}

	loaded := make([]resolved, len(regions))
	byStem := make(map[string]resolved, len(regions))
	for i, r := range regions {
		start := viper.GetInt(fmt.Sprintf("value.region.%s.start", r.yamlKey))
		bits := viper.GetUint64(fmt.Sprintf("value.region.%s.bits", r.yamlKey))
		loaded[i] = resolved{
			region:       r,
			start:        start,
			bits:         bits,
			words:        int((bits + 63) / 64),
			lastWordMask: lastWordMaskFor(bits),
		}
		byStem[r.macroStem] = loaded[i]
	}

	tokenWords := byStem["TOKENS"].words
	aWords := tokenWords / 2
	bWords := tokenWords - aWords
	// Surface width: one result lane per rotation × each A word (B rotates in lockstep per word).
	surfaceElements := numRotations * aWords

	if err := writeHeader(loaded, totalWords, numRotations, aWords, bWords, surfaceElements); err != nil {
		die("write C header: %v", err)
	}
	if err := writeGo(loaded, totalWords, totalBytes, numRotations); err != nil {
		die("write Go layout: %v", err)
	}
	if err := writeTS(loaded, totalWords, totalBytes, numRotations); err != nil {
		die("write TS layout: %v", err)
	}

	propertiesStart := byStem["PROPERTIES"].start
	if err := writePropertiesTS(propertyTypes, valueRoles, propertiesStart); err != nil {
		die("write TS properties: %v", err)
	}

	layout, err := buildProgramLayoutFromViper(loaded, byStem)
	if err != nil {
		die("build program layout: %v", err)
	}
	compiled, err := compilePrograms(layout)
	if err != nil {
		die("compile programs: %v", err)
	}
	if err := writeProgramsTS(compiled); err != nil {
		die("write TS programs: %v", err)
	}
}

func lastWordMaskFor(bits uint64) uint64 {
	rem := bits % 64
	if rem == 0 {
		return ^uint64(0)
	}
	return (uint64(1) << uint(rem)) - 1
}

func writeHeader(loaded []resolved, totalWords, numRotations, aWords, bWords, surfaceElements int) error {
	var b strings.Builder
	b.WriteString("// THIS FILE IS AUTOMATICALLY GENERATED BY go generate.\n")
	b.WriteString("// DO NOT EDIT. Source of truth: cmd/cfg/config.yml\n\n")
	b.WriteString("#ifndef SUBSTRATE_PRIMITIVES_H\n")
	b.WriteString("#define SUBSTRATE_PRIMITIVES_H\n\n")

	macro(&b, "WORDS", totalWords)
	macro(&b, "NUM_ROTATIONS", numRotations)
	macro(&b, "SURFACE_ELEMENTS", surfaceElements)
	// Tokens-specific legacy macros consumed by the universal-bitwise kernel.
	macro(&b, "TOKEN_WORDS", loaded[0].words)
	macro(&b, "A_WORDS", aWords)
	macro(&b, "B_WORDS", bWords)
	b.WriteString("\n")

	for _, r := range loaded {
		macro(&b, r.macroStem+"_START_WORD", r.start)
		macro(&b, r.macroStem+"_WORDS", r.words)
		macro(&b, r.macroStem+"_BITS", int(r.bits))
		macro64(&b, r.macroStem+"_LAST_WORD_MASK", r.lastWordMask)
		b.WriteString("\n")
	}

	b.WriteString("#endif // SUBSTRATE_PRIMITIVES_H\n")

	return writeOut(filepath.Join(rootRel("..", "compute", "kernel", "shared"), "primitives.h"), b.String())
}

func writeGo(loaded []resolved, totalWords, totalBytes, numRotations int) error {
	var b strings.Builder
	b.WriteString("// Code generated by gen.go; DO NOT EDIT.\n")
	b.WriteString("// Source of truth: cmd/cfg/config.yml\n\n")
	b.WriteString("package primitive\n\n")

	b.WriteString("// RegionSpec describes one Value sub-region in compile-time terms.\n")
	b.WriteString("// Use it when a runtime []RegionSpec walk is more convenient than\n")
	b.WriteString("// hand-listing the per-region constants below.\n")
	b.WriteString("type RegionSpec struct {\n")
	b.WriteString("\tName         string\n")
	b.WriteString("\tStartWord    int\n")
	b.WriteString("\tWords        int\n")
	b.WriteString("\tBits         uint64\n")
	b.WriteString("\tLastWordMask uint64\n")
	b.WriteString("}\n\n")

	b.WriteString("const (\n")
	fmt.Fprintf(&b, "\tWordCount       = %d // value.words\n", totalWords)
	fmt.Fprintf(&b, "\tFrameByteLength = %d // value.bytes\n", totalBytes)
	fmt.Fprintf(&b, "\tNumRotations    = %d // value.num_rotations\n", numRotations)
	b.WriteString(")\n\n")

	b.WriteString("const (\n")
	for _, r := range loaded {
		fmt.Fprintf(&b, "\t%-22s = %d\n", r.goStem+"StartWord", r.start)
		fmt.Fprintf(&b, "\t%-22s = %d\n", r.goStem+"Words", r.words)
		fmt.Fprintf(&b, "\t%-22s = %d\n", r.goStem+"Bits", r.bits)
		fmt.Fprintf(&b, "\t%-22s uint64 = 0x%016X\n", r.goStem+"LastWordMask", r.lastWordMask)
	}
	b.WriteString(")\n\n")

	b.WriteString("// RegionSpecs is the canonical word-ascending region order. Code that\n")
	b.WriteString("// iterates regions (telemetry, dumps, validators) must walk this slice\n")
	b.WriteString("// rather than hand-listing the constants above.\n")
	b.WriteString("var RegionSpecs = []RegionSpec{\n")
	for _, r := range loaded {
		fmt.Fprintf(&b,
			"\t{Name: %q, StartWord: %s, Words: %s, Bits: %s, LastWordMask: %s},\n",
			r.yamlKey,
			r.goStem+"StartWord", r.goStem+"Words", r.goStem+"Bits", r.goStem+"LastWordMask",
		)
	}
	b.WriteString("}\n")

	return writeOut(filepath.Join(rootRel("."), "layout_generated.go"), b.String())
}

func writeTS(loaded []resolved, totalWords, totalBytes, numRotations int) error {
	var b strings.Builder
	b.WriteString("// THIS FILE IS AUTOMATICALLY GENERATED BY go generate.\n")
	b.WriteString("// DO NOT EDIT. Source of truth: cmd/cfg/config.yml\n\n")

	fmt.Fprintf(&b, "export const VALUE_WORD_COUNT = %d;\n", totalWords)
	fmt.Fprintf(&b, "export const VALUE_FRAME_BYTE_LENGTH = %d;\n", totalBytes)
	fmt.Fprintf(&b, "export const NUM_ROTATIONS = %d;\n\n", numRotations)

	for _, r := range loaded {
		fmt.Fprintf(&b, "export const %s_START_WORD = %d;\n", r.macroStem, r.start)
		fmt.Fprintf(&b, "export const %s_WORDS = %d;\n", r.macroStem, r.words)
		fmt.Fprintf(&b, "export const %s_BITS = %d;\n", r.macroStem, r.bits)
		fmt.Fprintf(&b, "export const %s_LAST_WORD_MASK = 0x%016xn;\n\n", r.macroStem, r.lastWordMask)
	}

	b.WriteString("export type ValueRegionName =\n")
	for i, r := range loaded {
		sep := ""
		if i == len(loaded)-1 {
			sep = ";"
		}
		fmt.Fprintf(&b, "\t| %q%s\n", r.yamlKey, sep)
	}
	b.WriteString("\n")

	b.WriteString("export interface RegionSpec {\n")
	b.WriteString("\tname: ValueRegionName;\n")
	b.WriteString("\tstartWord: number;\n")
	b.WriteString("\twordCount: number;\n")
	b.WriteString("\tbits: number;\n")
	b.WriteString("\tlastWordMask: bigint;\n")
	b.WriteString("}\n\n")

	b.WriteString("export const REGION_SPECS: ReadonlyArray<RegionSpec> = [\n")
	for _, r := range loaded {
		fmt.Fprintf(&b,
			"\t{ name: %q, startWord: %s_START_WORD, wordCount: %s_WORDS, bits: %s_BITS, lastWordMask: %s_LAST_WORD_MASK },\n",
			r.yamlKey, r.macroStem, r.macroStem, r.macroStem, r.macroStem,
		)
	}
	b.WriteString("] as const;\n")

	target := filepath.Join(rootRel("..", "..", "visualizer", "src", "lib"), "layoutGenerated.ts")
	return writeOut(target, b.String())
}

// rootRel resolves a path relative to the package directory of this generator
// (pkg/primitive/), so the binary can be run from any cwd via go generate.
func rootRel(parts ...string) string {
	_, genFile, _, ok := runtime.Caller(0)
	if !ok {
		genFile = "."
	}
	all := append([]string{filepath.Dir(genFile)}, parts...)
	return filepath.Clean(filepath.Join(all...))
}

func writeOut(path, body string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("mkdir %q: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		return fmt.Errorf("write %q: %w", path, err)
	}
	fmt.Printf("Generated %s\n", path)

	return nil
}

func macro(b *strings.Builder, name string, val int) {
	fmt.Fprintf(b, "#define %-40s %d\n", name, val)
}

func macro64(b *strings.Builder, name string, val uint64) {
	fmt.Fprintf(b, "#define %-40s 0x%016XULL\n", name, val)
}

func die(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "gen primitives: "+format+"\n", args...)
	os.Exit(1)
}

// writePropertiesTS emits the property type table the visualiser uses to
// turn property names into absolute word indices. Without this the TS side
// would have to hardcode "PROPERTIES_START_WORD + 8 = COMMUNITY", which
// drifts the moment properties.go grows a new entry above COMMUNITY.
func writePropertiesTS(propertyNames, roleNames []string, propertiesStartWord int) error {
	var b strings.Builder
	b.WriteString("// THIS FILE IS AUTOMATICALLY GENERATED BY go generate.\n")
	b.WriteString("// DO NOT EDIT. Sources of truth: pkg/primitive/properties.go (PropertyType\n")
	b.WriteString("// and ValueRole iotas) and cmd/cfg/config.yml (PROPERTIES_START_WORD).\n\n")

	fmt.Fprintf(&b, "export const PROPERTIES_START_WORD = %d;\n\n", propertiesStartWord)

	b.WriteString("export type PropertyName =\n")
	for i, name := range propertyNames {
		sep := ""
		if i == len(propertyNames)-1 {
			sep = ";"
		}
		fmt.Fprintf(&b, "\t| %q%s\n", name, sep)
	}
	b.WriteString("\n")

	b.WriteString("// PROPERTY_OFFSET maps a PropertyName to its slot inside the properties\n")
	b.WriteString("// region. Use PROPERTY_WORD() for the absolute frame word index.\n")
	b.WriteString("export const PROPERTY_OFFSET: Record<PropertyName, number> = {\n")
	for i, name := range propertyNames {
		fmt.Fprintf(&b, "\t%s: %d,\n", name, i)
	}
	b.WriteString("};\n\n")

	b.WriteString("export const PROPERTY_WORD = (name: PropertyName): number =>\n")
	b.WriteString("\tPROPERTIES_START_WORD + PROPERTY_OFFSET[name];\n\n")

	b.WriteString("export type ValueRoleName =\n")
	for i, name := range roleNames {
		sep := ""
		if i == len(roleNames)-1 {
			sep = ";"
		}
		fmt.Fprintf(&b, "\t| %q%s\n", name, sep)
	}
	b.WriteString("\n")

	b.WriteString("// VALUE_ROLE assigns each role its on-wire sentinel — the integer the\n")
	b.WriteString("// kernel writes into the ROLE property word. ValueRoleNone (0) means\n")
	b.WriteString("// the substrate hasn't claimed a role yet.\n")
	b.WriteString("export const VALUE_ROLE: Record<ValueRoleName, number> = {\n")
	for i, name := range roleNames {
		fmt.Fprintf(&b, "\t%s: %d,\n", name, i)
	}
	b.WriteString("};\n")

	target := filepath.Join(rootRel("..", "..", "visualizer", "src", "lib"), "propertiesGenerated.ts")
	return writeOut(target, b.String())
}

// buildProgramLayoutFromViper rebuilds the same Layout that pkg/core uses
// for runtime program compilation, but without importing pkg/core (which
// would create a generator → runtime cycle). Region offsets come from the
// already-resolved region table; opcode names come straight off viper.
func buildProgramLayoutFromViper(loaded []resolved, byStem map[string]resolved) (program.Layout, error) {
	regionsMap := make(map[string]program.RegionExtent, len(loaded))
	for _, r := range loaded {
		regionsMap[r.yamlKey] = program.RegionExtent{Start: r.start, Words: r.words}
	}

	// Opcode default table mirrors pkg/core/config.go nibbleOf fallbacks. If
	// the YAML overrides an opcode name with a different binary string, the
	// override wins via viper.GetString below.
	defaults := map[string]uint64{
		"false":    0x0,
		"and":      0x1,
		"aandnotb": 0x2,
		"a":        0x3,
		"notandb":  0x4,
		"b":        0x5,
		"xor":      0x6,
		"or":       0x7,
		"nor":      0x8,
		"xnor":     0x9,
		"notb":     0xA,
		"ifbthena": 0xB,
		"nota":     0xC,
		"ifathenb": 0xD,
		"nand":     0xE,
		"true":     0xF,
	}

	opcodes := make(map[string]uint64, len(defaults))
	for name, fallback := range defaults {
		spec := strings.TrimSpace(viper.GetString(fmt.Sprintf("value.opcodes.%s", name)))
		if spec == "" {
			opcodes[name] = fallback
			continue
		}
		v, err := strconv.ParseUint(spec, 2, 8)
		if err != nil || v > 0xF {
			opcodes[name] = fallback
			continue
		}
		opcodes[name] = v
	}

	_ = byStem // reserved for future cross-checks
	return program.Layout{Regions: regionsMap, Opcodes: opcodes}, nil
}

// compiledProgram is one row of the generated TS table.
type compiledProgram struct {
	name   string
	source string
	words  []uint64
}

// compilePrograms walks viper's `programs:` map (sorted for stable output)
// and lowers each DSL block through the same compiler the runtime uses.
func compilePrograms(lay program.Layout) ([]compiledProgram, error) {
	raw, ok := viper.Get("programs").(map[string]any)
	if !ok || raw == nil {
		return nil, nil
	}

	keys := make([]string, 0, len(raw))
	for k := range raw {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	out := make([]compiledProgram, 0, len(keys))
	for _, key := range keys {
		source, ok := raw[key].(string)
		if !ok || source == "" {
			continue
		}
		c, err := program.Compile(source, lay)
		if err != nil {
			return nil, fmt.Errorf("program %q: %w", key, err)
		}
		out = append(out, compiledProgram{name: key, source: source, words: c.Words})
	}
	return out, nil
}

// writeProgramsTS emits the program signature table the visualiser's
// classifier matches against. Each program's instruction stream is
// pre-decoded so the TS side just compares operand tuples — no DSL parser
// in the browser, no separate signature table that drifts.
func writeProgramsTS(programs []compiledProgram) error {
	var b strings.Builder
	b.WriteString("// THIS FILE IS AUTOMATICALLY GENERATED BY go generate.\n")
	b.WriteString("// DO NOT EDIT. Source of truth: cmd/cfg/config.yml `programs:` block,\n")
	b.WriteString("// lowered by pkg/compute/program.Compile against the runtime layout.\n\n")

	// Re-export the packed-instruction bit layout so callers that decode a
	// raw program region don't carry their own copy of the constants.
	fmt.Fprintf(&b, "export const INSTR_DST_SPAN_SHIFT  = %d;\n", program.InstrDstSpanShift)
	fmt.Fprintf(&b, "export const INSTR_DST_START_SHIFT = %d;\n", program.InstrDstStartShift)
	fmt.Fprintf(&b, "export const INSTR_B_SPAN_SHIFT    = %d;\n", program.InstrBSpanShift)
	fmt.Fprintf(&b, "export const INSTR_B_START_SHIFT   = %d;\n", program.InstrBStartShift)
	fmt.Fprintf(&b, "export const INSTR_A_SPAN_SHIFT    = %d;\n", program.InstrASpanShift)
	fmt.Fprintf(&b, "export const INSTR_A_START_SHIFT   = %d;\n", program.InstrAStartShift)
	fmt.Fprintf(&b, "export const INSTR_OPCODE_SHIFT    = %d;\n", program.InstrOpcodeShift)
	fmt.Fprintf(&b, "export const INSTR_MODE_SHIFT      = %d;\n", program.InstrModeShift)
	fmt.Fprintf(&b, "export const INSTR_FIELD_MASK   = 0x%xn;\n", program.InstrFieldMask)
	fmt.Fprintf(&b, "export const INSTR_OPCODE_MASK  = 0x%xn;\n", program.InstrOpcodeMask)
	fmt.Fprintf(&b, "export const INSTR_MODE_MASK    = 0x%xn;\n\n", program.InstrModeMask)

	b.WriteString("export interface DecodedInstruction {\n")
	b.WriteString("\taStart: number;\n")
	b.WriteString("\taSpan: number;\n")
	b.WriteString("\tbStart: number;\n")
	b.WriteString("\tbSpan: number;\n")
	b.WriteString("\tdstStart: number;\n")
	b.WriteString("\tdstSpan: number;\n")
	b.WriteString("\topcode: number;\n")
	b.WriteString("\tmode: number;\n")
	b.WriteString("}\n\n")

	b.WriteString("export interface ProgramSignature {\n")
	b.WriteString("\tname: string;\n")
	b.WriteString("\tinstructions: ReadonlyArray<DecodedInstruction>;\n")
	b.WriteString("}\n\n")

	b.WriteString("// PROGRAM_SIGNATURES holds every named program the runtime knows about,\n")
	b.WriteString("// pre-lowered to its packed instruction stream. The visualiser classifier\n")
	b.WriteString("// matches a Value's program region against this table; an exact match\n")
	b.WriteString("// (same opcode + operand tuples in the same order) names the program.\n")
	b.WriteString("export const PROGRAM_SIGNATURES: ReadonlyArray<ProgramSignature> = [\n")
	for _, p := range programs {
		fmt.Fprintf(&b, "\t{\n\t\tname: %q,\n\t\tinstructions: [\n", p.name)
		for _, w := range p.words {
			aStart, aSpan, bStart, bSpan, dstStart, dstSpan, opcode, mode := program.DecodeInstruction(w)
			fmt.Fprintf(&b,
				"\t\t\t{ aStart: %d, aSpan: %d, bStart: %d, bSpan: %d, dstStart: %d, dstSpan: %d, opcode: 0x%x, mode: %d },\n",
				aStart, aSpan, bStart, bSpan, dstStart, dstSpan, opcode, mode,
			)
		}
		b.WriteString("\t\t],\n\t},\n")
	}
	b.WriteString("] as const;\n")

	target := filepath.Join(rootRel("..", "..", "visualizer", "src", "lib"), "programsGenerated.ts")
	return writeOut(target, b.String())
}
