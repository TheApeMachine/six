# Agents Development Guidelines

These are the rules for agents to strictly adhere to when developing code inside this project.

## Project Description

This is not a very typical project, and more often than not it requires less typical solutions. It is best to start by ignoring all previous notions, preconceptions, and always build from first principles, starting by developing a deep understanding what the code is actually doing.

If you come in to this project thinking that you need to fix mistakes, you must explicitely check again to make absolutely sure you are not just looking at things through a default lens that may not give you the correct picture.

## Core Philosophy

**Performance is not negotiable**

If there is still performance on left on the table, no matter the complication, then we are not done yet. If you look through this project you will soon notice that we take this to its logical extreme, with custom Cuda and Metal kernels, and Assembly SIMD optimized code. This is the standard, and not the exception.

> One specific note on performance, it is important to think about the details here, especially around the use of mutexes, channels, etc. If a lock-free path exists, we must choose to take that direction, always.

## Coding style

Each "thing" should be an object with methods. We don't like loose functions. 
A typical object usually follows a pattern like below.
Ideally we don't have two objects in the same file, but small exceptions do exist here, for example when it is just a localized helper structure.

> Everything in examples like this is important and deliberate.
> For example, we really do not like single character variable names.
> We also want code to be spaced out correctly vertically, so put newlines between groupings.
> A block should always have a newline and be separate from the code above and below it.
> The comment style is also exact, using `/**/` for top-level and `//` for inline comments, which creates a contrast that makes it very easy to visually parse things. Comments should ALWAYS explain more than the code is already telling us. Comments should not be just a more elaborate description of the name, or function, but objectively provide additional context, reasoning, or explanation. Realize that the comments are also documentation when packages end up on Godoc. Never use "section" comments to break up large files or methods. If you feel the need to do so, it means you are mixing too many concerns and you need to refactor and break up your code. 

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
func NewObjectName(ctx context.Context, opts ...opts) *ObjectName {

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

> Some additional guidelines that rely heavy on personal preferences.
> Follow the happy-path, using guards to do early returns, and keeping the happy path free from nesting.
> Avoid using `else` if at all possible. Many times reversing the logic can eliminate the `else` branch.
> We take the statement "if, is an enabler" serious, and always try to look for ways to reduce `if` statements.

> A final remark on code quality.
> Avoid over-engineering at all cost. Always ask yourself if the complexity is earned. Always.
> Less is always more, refactoring is not optional. If it can be done with less code, do it with less code.
> If you see something that is not yours that can be done with less code, refactor it.
> However, if less code means less performance, then always choose performance.
> We like clever code, readability is for amateurs.

## Testing

We always use Goconvey for testing, and tests follow a simple structure. Every file should have a test file that mirrors its structure. So each file has an accompanying `_test.go` file, with functions that mirror the code's methods, prefix by `Test`.
We follow a nested BDD approach `Given something`, `It should do something`.
Never break from this pattern, you should never have a test function that does not mirror an existing method in the code, if you feel a need to do that, it means you need to reconsider structuring/nesting the test function that actually mirrors the code where what you want to test is being called, directly or indirectly.
Always add benchmarks too, so we can measure performance.

Make sure tests and benchmarks are truly meaningful, don't test for testing's sake, make sure it truly validates the code. This also means to reduce your reliance on mocks, we actually prefer to always use the actual system for test setup, which actually makes it such that things are tested in varying scenarios that mirror reality.

## Documentation

Always keep the README.md in the root of the project up to date, and make sure to use the rich features of Github markdown to maintain high readability standards, with clear contrast between sections and information. The use of emojis is encouraged, as long as it is done in a functional and subtle manner.

---

Follow these guidelines at all times.

Now, start by reading the README.md in the root of this project, then reason through your current task step by step, sourcing additional context from the code where needed.