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
func Ok[T any](value T) Result[T] { return Result[T]{value: value} }




/*
Fail returns a Result containing the given error.
*/
func Fail[T any](err error) Result[T] { return Result[T]{err: err} }

/*
Try returns a Result containing the given value and error.
*/
func Try[T any](value T, err error) Result[T] {
	if err != nil {
		return Fail[T](err)
	}

	return Ok(value)
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
func Then[T, U any](res Result[T], fn func(T) (U, error)) Result[U] {
	if res.err != nil {
		return Result[U]{err: res.err}
	}

	value, err := fn(res.value)
	return Try(value, err)
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
