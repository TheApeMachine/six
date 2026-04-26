# The Six Programming Syntax

This document describes the **program text** the toolchain accepts today: the scanner (`pkg/compute/program/scanner.go`), parser (`pkg/compute/program/parser.go`), and compiler (`pkg/compute/program/compiler.go`), with regions and property names resolved from `cmd/cfg/config.yml` (loaded into `program.Layout` via `pkg/core/config.go`).

---

## 1. Values: programmable data (the ABI)

The runtime `Value` is a fixed layout of `uint64` words (see `value.region` in `config.yml`). Source programs refer to **symbolic regions** (e.g. `program`, `tokens`, `properties.surprisal`); the compiler lowers those names to absolute word indices using the active `Layout`.

```text
┌─────────────┬────────────┬────────────┬────────────┬──────────────┬──────────────┬─────────────┬──────┬──────┬─────┬──────────────┐
│   Tokens    │  Program   │  Signals   │  Context   │   Gradient   │  Properties  │   Assets    │ Prev │ Next │ ID  │   Affinity   │
│  1024 bits  │  1024 bits │  512 bits  │  512 bits  │   512 bits   │  1024 bits   │  3072 bits  │  64  │  64  │ 64  │   257 bits   │
│ words 0-15  │ words16-31 │ words32-39 │ words40-47 │  words48-55  │ words 56-71  │ words72-119 │ 120  │ 121  │ 122 │ words123-127 │
└─────────────┴────────────┴────────────┴────────────┴──────────────┴──────────────┴─────────────┴──────┴──────┴─────┴──────────────┘
```

Word spans follow `value.region.*.start` and `value.region.*.bits` in `config.yml` (word count is bits rounded up to 64-bit words).

### Properties (words 56–71)

The **order and spelling** of property slots come from `value.properties` in `config.yml`. The compiler lowercases each entry to build `Layout.Properties` (e.g. `SURPRISAL` → `surprisal`). Offsets are **indices inside the properties region** (absolute word = `properties.start` + offset).

With the default list in `config.yml`:

| Word (abs) | Offset | Symbolic name       | Config key        |
|------------|--------|---------------------|-------------------|
| 56         | 0      | **labels**          | `LABELS`          |
| 57         | 1      | **confidence**      | `CONFIDENCE`      |
| 58         | 2      | **epoch**           | `EPOCH`           |
| 59         | 3      | **ttl**             | `TTL`             |
| 60         | 4      | **temperature**     | `TEMPERATURE`     |
| 61         | 5      | **status**          | `STATUS`          |
| 62         | 6      | **noise**           | `NOISE`           |
| 63         | 7      | **program_id**      | `PROGRAM_ID`      |
| 64         | 8      | **community**       | `COMMUNITY`       |
| 65         | 9      | **target**          | `TARGET`          |
| 66         | 10     | **role**            | `ROLE`            |
| 67         | 11     | **reference**       | `REFERENCE`       |
| 68         | 12     | **surprisal**       | `SURPRISAL`       |
| 69         | 13     | **prev_surprisal**  | `PREV_SURPRISAL`  |
| 70         | 14     | **delta_surprisal** | `DELTA_SURPRISAL` |
| 71         | 15     | **continuation**    | `CONTINUATION`    |

`status` and `role` in `config.yml` define the integer enums stored in **status** and **role** words.

---

## 2. Lexical rules (scanner)

- **Whitespace** is ignored outside tokens.
- **Comments** run from `;` to end of line (`scanner.go`).
- **Identifiers** must match either a region name, word name, or enum constant, and optionally a (sub) span (e.g. `status`, `asset[0,8]`).
- **Numbers** are digit runs, optionally containing one `.` for range literals like `16..24`.
- **`<=`** is a single **feed** token. **`<` alone** is left angle; **`>`** is right angle. There is **no** `=>` token.
- **Operators** are single runes from the set `^ | & ~ = \ / -` with limited two-character lookahead (`==`, `~|`, `~&`, `~A`, `~B`, `->`, `<-`).
- **`?`** is a hard gate, when condition is not met execution stops for the the rest of the current ALU run

---

## 3. Instruction-line bracket form

Grammar (after comments stripped per line; used when §2’s feed trigger is absent):

```text
[ ( <region> <topology> ) <= ( <expr> ) [ ? ( <predicate> ) ] [ <= <scope> ] ]
```

- **Target:** parentheses containing exactly two tokens: region reference and topology keyword.
- **Feed:** `<=` between target and expression.
- **Expression:** parentheses around either a reduction prefix, a bare `DONE` / `A`, or a truth-table / passthrough form (see below).
- **Predicate:** optional `? ( ... )` after the expression.
- **Scope:** optional second `<=` followed by `community`, an identifier, or a parenthesized token run (e.g. `(0..n)`). The parser stores this string on the AST **but `compileInstruction` does not encode it into the instruction word**; execution scope is whatever the runtime applies when it runs the compiled program.

### Region references (`parseRef`)

- **Numeric:** `wordIndex` or half-open range `start..end` (span = `end - start` words, words `start` through `start + span - 1`).
- **Indexed region:** `name[relStart]` or `name[relStart,wordSpan]` (e.g. `signals[0,8]`).
- **Property:** `properties.<name>` where `<name>` is a key in `Layout.Properties`.
- **Indirect:** leading `*` on the above forms sets the indirect flag on the operand.
- **Bare region name:** if `Layout.Regions` contains the name (e.g. `program`), that whole region is used.

Any region or property name must exist on the `Layout` passed to `Compile` (normally built from `config.yml`).

### Topologies (`Topologies` in `compiler.go`)

| Keyword | Compiler constant | Notes                                                                      |
|---------|-------------------|----------------------------------------------------------------------------|
| `self`  | local             | Result written on the executing value (or owner flags vary by expression). |
| `next`  | ring              | Adjacent value in community order.                                         |
| `fold`  | hypercube         | Allowed only for opcodes `0`, `1`, `&`, `|`, `^`, `==` (`isFoldOpcode`).   |
| `spawn` | scatter           |                                                                            |
| `emit`  | same as `spawn`   | Alias in the topology map.                                                 |

`B` as topology is accepted and normalized to `self` with `InstrFlagTargetB` set.

### Expressions (`parseExpr` + `compileInstruction`)

- **Reduction prefix:** `popcnt`, `any_zero`, or `all_ones` before the parenthesized operand (modes `ModePopcnt`, `ModeAnyZero`, `ModeAllOnes`).
- **`saturates`:** recognized by the parser in this position but **`compileInstruction` returns an error** (“not a language intrinsic”). Do not use in instruction-line form.
- **`DONE`:** encodes immediate/status-style write using opcode `B`, immediate type, fixed slot from compiler.
- **Bare `A`:** passthrough opcode `A`.
- **Unary region / literal:** `(0)`, `(1)`, `(A)`, or a single region ref — opcode `A` or constant opcodes for `0`/`1`.
- **Binary:** `( <ref> <op> <ref-or-immediate> )` where `<op>` is a key in `var Opcodes` (`compiler.go`): `0`, `&`, `\`, `A`, `/`, `B`, `^`, `|`, `~|`, `==`, `~B`, `<-`, `~A`, `->`, `~&`, `1`, plus geometric **`compose`**, **`sandwich`**, **`reverse`** (high nibble; `IsGeometricOpcode` forces geometric mode).
- **Immediate right-hand side:** numeric `B` operand packs start/span for small immediates (`compileInstruction`).

### Predicates (`compilePredicate`)

- **Extended popcnt:** `popcnt(<region>) | <N>` → population count **≤ N** (`predicatePopcntLTE`).  
- **Feed nested gate** (§5–§6): `popcnt` with operator `<` and threshold `N` → count **< N** (`predicatePopcntLT`).
- **Word tests:** `!= 0`, `== 0`, or `> 0` on a scalar word (the `> 0` case uses extended predicate mode with `PredicateAllows` fallback `frame[predStart] > 0` when no table entry exists).
- Other combinations return a compile error (“not fully supported yet”).

---

## 4. Feed pipelines (`compileFeedSource`)

Used when the program text includes `{` or the two-byte prefix **`[` + `(`** with no space between them (`[(` — see `Compile`). That allows a compact pipe like `[(B popcnt)]` with parentheses but no braces (`compiler_test.go`). The compiler scans for each `[ ... ]` **pipe**; a pipe is marked emit-phase if it appears inside `<[ ... ]>` (see `parseFeedSites`).

### Comments

`stripFeedComments` removes `;` and `#` to end of line before splitting pipes.

### Order of compilation

- If the source **contains** the substring `<=`, pipes are compiled **from last in the file to first**; otherwise **first to last** (`compileFeedSource`).

### Operations `{ }`

Inside a pipe, one or more `{ ... }` blocks (Reverse Polish style via `parseFeedExpr`):

- **Operands:** `A(...)`, `B(...)`, bare `A` / `B` (map over community with default `signals[0,8]` target in the atom), immediates `done`, `clear`, or numeric immediates, or region refs with explicit owner where required.
- **Reducers (suffix):** `popcnt`, `any_zero`, `all_ones` only (`isFeedReducer`). **`saturates` is not a feed reducer.**
- **Operators:** keys from `Opcodes` as in §4; topology may appear as the **last** token of an operation (`self`, `next`, `fold`, `spawn`, `emit`).
- **Ambiguity:** operands that name a region must include `A(` or `B(` unless they are immediates or bare `A`/`B` — `requireExplicitFeedOwner`.

**Rotations:** inside `A(...)` / `B(...)`, a ref may be followed by `N <<` or `N >>` to rotate indices within a span (`parseFeedAtomRef` / `rotateFeedRef`).

### Feeds between pipes

Physical layout uses `<=` in the source only to select **reverse** compilation order; bonds between stages are the **incoming feed atoms** produced by earlier-compiled pipes, not a separate `=>` syntax.

### Emit `<[ ... ]>`

Emit brackets toggle emit mode for contained pipes. Emit operations set **continuation** from **id** with `ModeEmit` and spawn topology (`compileEmitSite` / `compileEmitOperationSite`).

### Feed gates

- **Standalone gate site:** nested `{ { ... } N ? }` with inner `owner(ref) popcnt` → predicate **count < N**.
- **`{ ... } { ... ? }`:** second block may encode a feed predicate parsed by `parseFeedPredicate` / `compilePredicate`.

---

## 5. Predicate vs fold (instruction-line)

For `fold`, every value must participate; the predicate only masks the **final write** (the compiler emits predicate bits accordingly; exact runtime semantics follow the substrate).

---

## 6. Layout truth

- **Property names** must exist in `value.properties` in `config.yml` (or the `Layout` you pass in tests).
- **Regions** must exist in `Layout.Regions` (`tokens`, `program`, `signals`, `context`, `gradient`, `properties`, `asset`, `prev`, `next`, `id`, `affinity` in stock config).
- **Fold:** non-associative opcodes on `fold` fail at compile time.
- **`rom.*` and other extra regions** are valid only if declared in the layout.

---

## 9. Notation summary (authoring)

```text
 ;      comment (scanner: also #)
[ ]     pipe (feed pipeline)
{ }     operation (RPN in feed form)
<=>     feed token: only <= exists (not =>)
 ?      gate / predicate marker
 !      part of != or ! when composed by scanner
 A B    frame owners in feed form
< >     emit wrapper: <[ pipes ]>
```

---

## 10. Specification (feed pipeline)

- A program is a sequence of **pipe** objects discovered in source order.
- A program may include an **emit** stage (`<[ ... ]>`).

### Pipe `[ ]`

- Staging area realized when combined with other pipes via feed compilation.
- With `<=` in the file, compilation walks pipes **last-to-first**; without it, **first-to-last**.

### Operation `{ }`

- Staging primitive inside a pipe; RPN evaluation in `parseFeedExpr`.
- Multiple `{ }` blocks at the same level compile to multiple instructions; concurrency is a runtime matter.
- Interpreted left-to-right in source; stack semantics in `parseFeedExpr`.

### Emit `< >`

- Emit stage is a sequence of pipe objects between `<[` and the matching `]>` (see `parseFeedSites`).

---

## 11. Examples aligned with `config.yml`

The `programs:` section in `cmd/cfg/config.yml` is the live reference for **feed** programs (e.g. `link`, `affinity`, `query`, `program_select`, `program_carrier`). `pkg/compute/program/compiler_test.go` exercises both surfaces (`compileFeedSource` and instruction-line compilation).
