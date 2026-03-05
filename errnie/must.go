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
FlatMap applies a function to the value in a Result, returning a new Result 
with the result of the function. Monadic chaining for functions that can fail.
*/
func FlatMap[T, U any](result Result[T], fn func(T) (U, error)) Result[U] {
	if result.err != nil {
		return Fail[U](result.err)
	}

	return Try(fn(result.value))
}