package errnie

/*
Result is a function type that operates on a receiver and returns an error.
*/
type Result[T any] func(T) error

/*
OpFn is a type alias for a function that takes a receiver and returns an error.
*/
type OpFn[T any] func(T) error

/*
Op wraps a method with an argument and returns an OpFn.
*/
func Op[T any, U any](method interface{}, value U) OpFn[T] {
	switch fn := method.(type) {
	case func(T, U) error:
		return func(receiver T) error {
			return fn(receiver, value)
		}
	case func(*T, U) error:
		return func(receiver T) error {
			return fn(&receiver, value)
		}
	default:
		panic("unsupported method type")
	}
}

/*
OpValue is a variant of Op for value receivers.
*/
func OpValue[T any, U any](method func(T, U) error, value U) OpFn[T] {
	return func(receiver T) error {
		return method(receiver, value)
	}
}

/*
OpPtr is a variant of Op for pointer receivers.
*/
func OpPtr[T any, U any](method func(*T, U) error, value U) OpFn[*T] {
	return func(receiver *T) error {
		return method(receiver, value)
	}
}
