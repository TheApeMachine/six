# Agents Development Guidelines

These are the rules for agents to strictly adhere to when developing code inside this project.

## Project Description

We are building an A.I. architecture that is a pretty radical departure from the norm.
It is extremely important that you reason about the architecture on its own terms and not try to make it be something it doesn't want to be.

Also, very important to keep in mind that we are reframing the A.I. tasks (experiments) as Boundary Value Problems, and Initial Value Problems. So we are not predicting the next byte, we are predicting the longest span. In that case we would call it a cantilever solve.

> !NOTE
> A common mistake is to think of data as "bytes" or "words" or "sentences". Do not do this. There are only Chords.
> This is a system that should reason about Chords, and nothing else.

## Core Philosophy

**Performance is not negotiable**

If there is still performance on left on the table, no matter the complication, then we are not done yet.

**Derive values**

This is a highly dynamic system, where magic numbers, guesses, and static values are of very little use. Always find legitimate ways to derive values from the surrounding dynamics.

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
```

> !NOTE
> A final remark on code quality.
> Less is always more, refactoring is not optional. If it can be done with less code, do it with less code.
> If you see something that is not yours that can be done with less code, refactor it.
> However, if less code means less performance, then always choose performance.
> We like clever code, readability is for amateurs.

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
```

## Testing

We always use Goconvey for testing, and tests follow a simple structure. Every file should have a test file that mirrors its structure. So each file has an accompanying `_test.go` file, with functions that mirror the code's methods, prefix by `Test`.
We follow a nested BDD approach `Given something`, `It should do something`.
Always add benchmarks too, so we can measure performance.

Make sure tests and benchmarks are truly meaningful, don't test for testing's sake, make sure it truly validates the code. Also, be somewhat intelligent about your test data, and create a generator to generate some significant data. Just a toy set proves very little.

If you encounter any tests not following this pattern, rewrite them properly.