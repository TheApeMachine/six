package errnie

/*
Lift takes a function returning (T, error) and returns a func() Result[T].
*/
func Lift[T any](fn func() (T, error)) func() Result[T] {
	return func() Result[T] {
		return Try(fn())
	}
}

/*
Lift1 takes a function with 1 argument returning (T, error) and returns a func(A) Result[T].
*/
func Lift1[A, T any](fn func(A) (T, error)) func(A) Result[T] {
	return func(a A) Result[T] {
		return Try(fn(a))
	}
}

/*
Lift2 takes a function with 2 arguments returning (T, error) and returns a func(A, B) Result[T].
*/
func Lift2[A, B, T any](fn func(A, B) (T, error)) func(A, B) Result[T] {
	return func(a A, b B) Result[T] {
		return Try(fn(a, b))
	}
}
