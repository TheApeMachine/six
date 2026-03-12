package errnie

/*
Result is a generic type that can hold either a value of type T or an error.
*/
type Result[T any] struct {
	value T
	err   error
}

/*
Ok returns a Result containing the given value.
*/
func Ok[T any](v T) Result[T] { return Result[T]{value: v} }

/*
ForEach runs fn(i) for i in [0, n), short-circuiting on the first error.
Replaces the per-iteration if-err idiom in counted loops.
*/
func ForEach(n int, fn func(int) error) error {
	for i := range n {
		if err := fn(i); err != nil {
			return err
		}
	}

	return nil
}

/*
Fail returns a Result containing the given error.
*/
func Fail[T any](err error) Result[T] { return Result[T]{err: err} }

/*
Try returns a Result containing the given value and error.
*/
func Try[T any](v T, err error) Result[T] {
	if err != nil {
		return Fail[T](err)
	}

	return Ok(v)
}

/*
Map applies a function to the value in a Result, returning a new Result
with the result of the function. Monadic chaining.
*/
func (result Result[T]) Map(fn func(T) T) Result[T] {
	if result.err != nil {
		return result
	}

	return Ok(fn(result.value))
}

/*
Then applies a function to the value in a Result, returning a new Result
with the result of the function. Monadic chaining for functions that can fail.
*/
func Then[T, U any](r Result[T], fn func(T) (U, error)) Result[U] {
	if r.err != nil {
		return Result[U]{err: r.err}
	}

	v, err := fn(r.value)
	return Try(v, err)
}

/*
Err returns the error held by this Result, or nil if it succeeded.
*/
func (result Result[T]) Err() error {
	return result.err
}

/*
Value returns the success value held by this Result.
*/
func (result Result[T]) Value() T {
	return result.value
}

/*
Unwrap returns both the value and error, mirroring Go's conventional (T, error).
*/
func (result Result[T]) Unwrap() (T, error) {
	return result.value, result.err
}

/*
Must returns the value or panics if the Result holds an error.
Used at the end of a chain where failure is truly unexpected.
*/
func (result Result[T]) Must() T {
	if result.err != nil {
		panic(result.err)
	}

	return result.value
}
