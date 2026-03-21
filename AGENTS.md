# Agents Development Guidelines

These are the rules for agents to strictly adhere to when developing code inside this project.

## Project Description

We are building an A.I. architecture that is a pretty radical departure from the norm.
It is extremely important that you reason about the architecture on its own terms and not try to make it be something it doesn't want to be.

Also, very important to keep in mind that we are reframing the A.I. tasks (experiments) as Boundary Value Problems, and Initial Value Problems. So we are not predicting the next byte, we are predicting the longest span. In that case we would call it a cantilever solve.

> !NOTE
> A common mistake is to think of data as "bytes" or "words" or "sentences". Do not do this. There are only Values.
> This is a system that should reason about Values, and nothing else.
> Also, do not try to make the tokenizer split things by sample id, the real world doesn't give you sample ids.

## Core Philosophy

**Performance is not negotiable**

If there is still performance on left on the table, no matter the complication, then we are not done yet.

**Derive values**

This is a highly dynamic system, where magic numbers, guesses, and static values are of very little use. Always find legitimate ways to derive values from the surrounding dynamics.

**No Fallbacks**

Never add a fallback, always return an error if something isn't absolutely as expected, fallbacks hide errors, and make us blind to them so we can't fix them either.

**No Tolerance Fallbacks**

If an exact match doesn't work, do not add a fuzzy fallback. If the system cannot find an exact answer, that is a bug in the system, not a reason to broaden the search. Fuzzy fallbacks mask real problems.

**No Options**

There is only one system, there is no optional second-system, no configuration beyond the basics, and it should remain that way at all times.

## Coding style

Each "thing" should be an object with methods. We don't like loose functions. A typical object usually follows a pattern like below.
Never have two objects in the same file.

> !NOTE
> Everything in examples like this is important and deliberate.
> For example, we really do not like single character variable names.
> We also want code to be spaced out correctly vertically, so put newlines between groupings.
> A block should always have a newline and be separate from the code above and below it.

```go
package packagename

/*
ObjectName is something descriptive.
It also has a reason why it was implemented.
*/
type ObjectName struct {
    err error
}

/*
opts configures ObjectName with options.
*/
type opts func(*ObjectName)

/*
NewObjectName instantiates a new ObjectName.
It also has a reason for being instantiated.
*/
func NewObjectName(opts ...opts) *ObjectName {
	obj := &ObjectName{}

	for _, opt := range opts {
		opt(obj)
	}

	return obj
}

/*
Read implements the io.Reader interface.
*/
func (objectName *ObjectName) Read(p []byte) (n int, err error) {
    return
}

/*
Write implements the io.Write interface.
*/
func (objectName *ObjectName) Write(p []byte) (n int, err error) {
    return
}

/*
Close implements the io.Reader interface.
*/
func (objectName *ObjectName) Close() (err error) {
    return
}

/*
ObjectNameWithSomething ...
*/
func ObjectNameWithSomething(something *Something) opts {
	return func(obj *ObjectName) {
		obj.something = something
	}
}

/*
ObjectNameError is a typed error for ObjectName failures.
*/
type ObjectNameError string

const (
	ErrSomething ObjectNameError = "something"
)

/*
Error implements the error interface for ObjectNameError.
*/
func (objError ObjectNameError) Error() string {
	return string(objError)
}
```

> !NOTE
> A final remark on code quality.
> Less is always more, refactoring is not optional. 
> If it can be done with less code, do it with less code.
> If you see something that is not yours that can be done with less code, refactor it.
> However, if less code means less performance, then always choose performance.
> We like clever code, readability is for amateurs.
> Keep the comments free of metaphor or needless complexity in their language.

Wrong:

```go
/*
ObjectName is an Object with a Name.
It defines the Object with a Name.
*/
```

Right:

```go
/*
SetAffine stores a tiny affine operator f(x) = ax + b (mod 257) in the shell.
This lets each value behave like a local transition rule rather than a passive
payload. Scale zero is normalized to the identity because traversal wants an
invertible default, not a black hole.
*/
```

Wrong:

```go
/*
Done finalizes the current streamed program boundary.
*/
```

Right:

```go
/*
RecursiveFold dynamically folds data into a graph of AST nodes.

EXAMPLE:

	DATA:
		[Sandra] <is in the> [Garden]
		[Roy]    <is in the> [Kitchen]
		[Harold] <is in the> [Kitchen]
			<is in the> the shared component that cancels out, becomes a "label".
			<is in the>   -points to-> [Sandra, Roy, Harold]
			[Sandra]      -points to-> [Garden]
			[Garden]      -points to-> [Sandra]
		    [Roy, Harold] -points to-> [Kitchen]
		    [Kitchen]     -points to-> [Roy, Harold]
	PROMPT:
		Where is Roy?
		Where has no shared component, ignored (if it don't react, it ain't a fact)
		<is> cancels out with <{is} in the> which -points to-> [Sandra, Roy, Harold]
		[Roy] cancels out with [{Roy}] which -points to-> [Kitchen]
	ANSWER:
		<in the> [Kitchen] (left over)
*/
```

Basically, comments should provide genuine value to the engineer, and we always think of people who may need to onboard into the code-base cold.

```go
// Wrong
minSeg := seq.MinSegmentBytes
if minSeg < 2 {
    minSeg = 2
}

// Right
minSeg := max(seq.MinSegmentBytes, 2)

// Wrong
func (seq *Sequencer) isSimilar(d1, d2 *Distribution) bool {
	if d1.n == 0 || d2.n == 0 {
		return false
	}
	c1 := d1.Cost() / float64(d1.n)
	c2 := d2.Cost() / float64(d2.n)
	return math.Abs(c1-c2) < 0.2
}

// Right
func (seq *Sequencer) isSimilar(d1, d2 *Distribution) bool {
	if d1.n == 0 || d2.n == 0 {
		return false
	}

    c1 := d1.Cost() / float64(d1.n)
	c2 := d2.Cost() / float64(d2.n)
	
    return math.Abs(c1-c2) < 0.2
}

// Wrong
values, valueErr := primitive.ValueListToSlice(primitive.Value_List(ptr.List()))
if valueErr != nil {
	out = append(out, nil)
	continue
}

// Right
values, valueErr := primitive.ValueListToSlice(
	primitive.Value_List(ptr.List()),
)

if valueErr != nil {
	out = append(out, nil)
	continue
}
```

## Testing

We always use Goconvey for testing, and tests follow a simple structure. Every file should have a test file that mirrors its structure. So each file has an accompanying `_test.go` file, with functions that mirror the code's methods, prefix by `Test`.
We follow a nested BDD approach `Given something`, `It should do something`.
Always add benchmarks too, so we can measure performance.

### Failing Tests Are Good

A failing test is the most valuable thing you can produce. It is a precise pointer to what needs to be fixed. The correct response to a failing test is **always** to fix the implementation, **never** to:

- Weaken the assertion (`ShouldEqual` → `ShouldBeGreaterThan, 0`)
- Remove the assertion entirely
- Change the test data so it avoids the failure
- Rewrite the entire test file to hide that the old assertions failed

If a test fails, report the failure honestly, diagnose the root cause, and fix the code. If you cannot fix the code, leave the test failing and explain why. A red test we understand is worth more than a green test that proves nothing.

### Test Integrity Rules

These are the specific failure modes that have been observed and are now banned.

**1. Every `gc.Convey` block MUST contain at least one `gc.So` assertion.**

```go
// BANNED — computes a value but never checks it
gc.Convey("Energy should be correct", func() {
    for _, result := range results {
        manualEnergy := computeEnergy(result)
        // WHERE IS gc.So?
    }
})

// CORRECT
gc.Convey("Energy should be correct", func() {
    for _, result := range results {
        manualEnergy := computeEnergy(result)
        gc.So(result.Energy, gc.ShouldEqual, manualEnergy)
    }
})
```

**2. Never construct test data that makes your assertion a tautology.**

If you manually set exactly 1 bit, then asserting `ActiveCount == 1` proves nothing — you are testing your own test setup, not the system. Use the real constructors (`BaseValue`, `BuildValue`, or the actual tokenizer path) so the test data has the same shape as production data.

```go
// BANNED — you set 1 bit, then check for 1 bit
stateValue := data.MustNewValue()
stateValue.Set(int(state))
// later...
gc.So(value.ActiveCount(), gc.ShouldEqual, 1) // tautology

// CORRECT — use real BaseValue, verify real properties
value := data.BaseValue(b)
value.Set(int(state))
// later...
gc.So(value.Has(int(expectedState)), gc.ShouldBeTrue)
```

**3. Never reimplement the system's logic in the test.**

If the test and the code use the same formula, you are testing that `A == A`. Tests must exercise the real system and verify observable outputs. If you need a ground truth, compute it once and hardcode the expected value, or use a completely independent method.

```go
// BANNED — same formula as production code
phase := wf.PromptToPhase(prompt)
manual := calc.SumBytes(prompt) // this IS PromptToPhase
gc.So(phase, gc.ShouldEqual, manual) // A == A

// CORRECT — verify the output has a real observable property
results := wf.Search(promptValue, nil, nil)
lastValue := results[0].Path[len(results[0].Path)-1]
gc.So(lastValue.Has(int(expectedState)), gc.ShouldBeTrue)
```

**4. Never use `gc.Printf` as a substitute for `gc.So`.**

Printing a value is not testing it. If a value matters, assert it. If it doesn't matter, don't print it.

```go
// BANNED — prints but never fails
gc.Printf("validation: %d/%d", validated, total)

// CORRECT — fails if validation is broken
gc.So(validated, gc.ShouldEqual, total)
```

**5. Test the real system, not a mock of it.**

Do not write helper functions that reimplement insertion, querying, or state management. Use the actual objects (`TokenizerServer`, `SpatialIndexServer`, `Wavefront`) through their real interfaces. If the real interface is hard to test, that is a design problem to fix, not a reason to build a parallel test universe.

> !NOTE
> Experiments are set up as Goconvey tests as well, and you MUST follow the standard 
> `pipeline.go` and `pipeline_test.go` harness, and use the full `vm.Machine` to 
> excercise the real architecture, no exceptions!

## Experiments

There are strict, non-negotiable rules for running experiments.

1. Always use the `pipeline.go` and `pipeline_test.go` harness
2. Do not change the harness in any way, shape, or form without discussion
3. Always use the full `vm.Machine` to excercise the real architecture
4. Use real data, not toy data.
5. DO NOT CHEAT, USE ORACLES, FAKE RESULTS, OR ANYTHING ELSE! DO NOT CUT CORNERS!

Experiments are about emperical results, and we want to report both the good, and the bad.

If we get good results, we need to push it to the limit, so we know where the breaking points are, and either fix it, or report it. No matter what want to report the breaking points, wherever those may be.

If we get bad results, we need to understand why, and fix it.

### When Tests or Experiments Fail

This is the procedure. No exceptions.

1. **Report the failure exactly as it happened.** Include the expected value, the actual value, and which assertion failed.
2. **Diagnose the root cause.** Is it a bug in the implementation? A wrong assumption in the test data? A missing feature?
3. **Fix the implementation, not the test.** If the test is correct but the code is wrong, fix the code. If the test is genuinely wrong (wrong expected value, wrong setup), explain why the test was wrong before changing it.
4. **Never silently weaken an assertion.** If you change `ShouldEqual` to `ShouldBeGreaterThan, 0`, you must explicitly justify why the exact value is unknowable.
5. **Never rewrite the entire test file to avoid a failure.** Surgical fixes only. If you need to rewrite, explain what was wrong with the original structure first.

## Paper

The experiment tests generate a research paper, but there are also static sections that need to be occasionally updated.
When updating the paper, realize who the audience is, and what the academic expectations are.
Do not write irrelevant historical references the reader has no frame of reference for.
We describe the system as it is at any given moment, and we do not leave old terminology or claims around in the paper, when those are no longer relevant.
The paper should be a high-quality academic breakdown of the system, with all the math, rigerous experimental results, which should show both the success and the failures, proper citations and references.
When it comes to experiments, we need to show good, rigerous science, so we push the system to the breaking point, and report it.
Further more, we take a position of absolute honesty, clear distinction between claims and speculation, and we do not hide any aspect of the system, or any aspect of the experiment.
And never use "hype" or "marketing" language. We are not selling anything, we are reporting science.

## FINAL NOTE

Please treat this project with respect. It is important to me and reflects many months of work. Never fake results, never fake data, and never fake any aspect of the system. Always double-check your work and report any issues you find.

## Learned User Preferences

- Verify reported findings against the current code before changing anything; when asked to fix something narrowly (lint, compile error, single call site, or a batch of reported issues), restrict edits to the confirmed issues and avoid broad rewrites or re-architecture unless invited.
- Stay on the task the user is doing now; if they narrow scope, redirect focus, or stop an approach, follow that instead of expanding or reviving sidelined work.
- Do not discard or replace substantial user-written structure to “save” the session; treat RPC and capability flow, especially Cap’n Proto local/remote semantics, as real constraints rather than generic data piping or unnecessary complexity.
- Services that need remote peers get a cluster router (or equivalent capability routing), not ad-hoc direct client wiring that bypasses the intended RPC path.
- For streaming transport code, keep fixes small and avoid replacing ring-buffer-oriented concurrency with extra mutexes, runtime/scheduler manipulation, or heavy casting when the existing stream design already carries the thread-safety goals; `FlipFlop` is intentionally sequential, and async bidirectional flow belongs in `transport.Stream`.
- Uncovered code is assumed broken until proven otherwise; test coverage and benchmarks gate trust in any package before optimization or feature work proceeds.

## Learned Workspace Facts

- End-state direction: pipeline parts composed with `io.ReadWriteCloser` (including in-place mutating middleware that still implements that interface) and stdlib glue (`io.Copy`, `MultiWriter`, `TeeReader`) where practical; `pool.Task` is `io.ReadWriteCloser`, workers drive jobs via `io.Copy`; `readPoolTask` is the adapter for scheduling background `func` work in dmt and kernel.
- Cluster RPC registration and bootstrap should live in a layer that does not import-cycle with packages that only consume the router; the router closes or tears down what it owns.
- Cap’n Proto services are expected to support both local and remote callers through the same capability shape; distributed services should reuse a single client/server `rpc.Conn` pair per transport instead of creating a new `rpc.Conn` on every `Client()` call.
- For Cap’n Proto streaming calls, keep `-> stream` semantics where appropriate and keep `ReleaseFunc` handling outside `errnie.Guard` closures so deferred releases always run.
- `transport.Stream` on a bounded ring blocks unless writers and readers overlap for payloads larger than the buffer; finish the producer with `CloseWrite` so reads can drain to `io.EOF`.
- Benchmarks of hot loops should hoist `make(chan …)` and per-iteration goroutines when the goal is zero allocs per iteration; one long-lived reader driven by a kick channel is the usual pattern.
- Prefer Cap’n Proto for on-the-wire work; avoid JSON on hot or internal paths.
- Bitwise ops on `Value` are structural signals (glue, splits); avoid mutating canonical chunk bytes in place—Heal is merge-oriented; RecursiveFold should split on structure from the data, not blind midpoints.
- Pool dispatcher uses bounded-wait + retry when handing jobs to workers; scaler can cancel workers mid-dispatch, causing deadlocks under coverage instrumentation or high load.