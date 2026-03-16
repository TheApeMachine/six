package errnie

import (
	"errors"
	"sync"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestMust(t *testing.T) {
	Convey("Given a value and a nil error", t, func() {
		value := 42
		var err error = nil

		Convey("When Must is called", func() {
			result := Must(value, err)

			Convey("Then it should return the value", func() {
				So(result, ShouldEqual, value)
			})
		})
	})

	Convey("Given a value and a non-nil error", t, func() {
		value := 42
		err := errors.New("some error")

		Convey("When Must is called", func() {
			Convey("Then it should panic", func() {
				So(func() { Must(value, err) }, ShouldPanic)
			})
		})
	})
}

func TestMustVoid(t *testing.T) {
	Convey("Given a nil error", t, func() {
		var err error = nil

		Convey("When MustVoid is called", func() {
			Convey("Then it should not panic", func() {
				So(func() { MustVoid(err) }, ShouldNotPanic)
			})
		})
	})

	Convey("Given a non-nil error", t, func() {
		err := errors.New("some error")

		Convey("When MustVoid is called", func() {
			Convey("Then it should panic", func() {
				So(func() { MustVoid(err) }, ShouldPanic)
			})
		})
	})
}

func TestSafeMust(t *testing.T) {
	Convey("Given a function that returns a value and a nil error", t, func() {
		fn := func() (int, error) {
			return 42, nil
		}

		Convey("When SafeMust is called", func() {
			result := SafeMust(fn)

			Convey("Then it should return the value", func() {
				So(result, ShouldEqual, 42)
			})
		})
	})

	Convey("Given a function that returns a value and a non-nil error", t, func() {
		fn := func() (int, error) {
			return 42, errors.New("some error")
		}

		Convey("When SafeMust is called", func() {
			Convey("Then it should panic and recover", func() {
				So(func() { SafeMust(fn) }, ShouldNotPanic)
			})
		})
	})
}

func TestSafeMustVoid(t *testing.T) {
	Convey("Given a function that returns a nil error", t, func() {
		fn := func() error {
			return nil
		}

		Convey("When SafeMustVoid is called", func() {
			Convey("Then it should not panic", func() {
				So(func() { SafeMustVoid(fn) }, ShouldNotPanic)
			})
		})
	})

	Convey("Given a function that returns a non-nil error", t, func() {
		fn := func() error {
			return errors.New("some error")
		}

		Convey("When SafeMustVoid is called", func() {
			Convey("Then it should panic and recover", func() {
				So(func() { SafeMustVoid(fn) }, ShouldNotPanic)
			})
		})
	})
}

func TestMustWithDifferentTypes(t *testing.T) {
	Convey("Given a string value and a nil error", t, func() {
		value := "test"
		var err error = nil

		Convey("When Must is called", func() {
			result := Must(value, err)

			Convey("Then it should return the string value", func() {
				So(result, ShouldEqual, value)
			})
		})
	})
}

func TestSafeMustWithComplexFunction(t *testing.T) {
	Convey("Given a function that performs complex logic and returns a nil error", t, func() {
		fn := func() (string, error) {
			// Complex logic here
			return "complex result", nil
		}

		Convey("When SafeMust is called", func() {
			result := SafeMust(fn)

			Convey("Then it should return the complex result", func() {
				So(result, ShouldEqual, "complex result")
			})
		})
	})
}

func TestSafeMustVoidWithNilFunction(t *testing.T) {
	Convey("Given a nil function", t, func() {
		var fn func() error = nil

		Convey("When SafeMustVoid is called", func() {
			Convey("Then it should panic", func() {
				So(func() { SafeMustVoid(fn) }, ShouldPanic)
			})
		})
	})
}

func TestMustWithCustomError(t *testing.T) {
	Convey("Given a value and a custom error", t, func() {
		value := 42
		err := &CustomError{"custom error"}

		Convey("When Must is called", func() {
			Convey("Then it should panic", func() {
				So(func() { Must(value, err) }, ShouldPanic)
			})
		})
	})
}

// CustomError is a custom error type for testing
type CustomError struct {
	msg string
}

func (e *CustomError) Error() string {
	return e.msg
}

func TestConcurrentMust(t *testing.T) {
	Convey("Given multiple goroutines calling Must concurrently", t, func() {
		var wg sync.WaitGroup
		value := 42
		var err error = nil
		results := make(chan int, 10)

		Convey("When Must is called concurrently", func() {
			for i := 0; i < 10; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					result := Must(value, err)
					results <- result
				}()
			}
			wg.Wait()
			close(results)

			Convey("Then it should return the correct value without panicking", func() {
				for result := range results {
					So(result, ShouldEqual, value)
				}
			})
		})
	})
}

func TestConcurrentSafeMustVoid(t *testing.T) {
	Convey("Given multiple goroutines calling SafeMustVoid concurrently", t, func() {
		var wg sync.WaitGroup
		fn := func() error {
			return nil
		}
		errorChan := make(chan error, 10)

		Convey("When SafeMustVoid is called concurrently", func() {
			for i := 0; i < 10; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					defer func() {
						if r := recover(); r != nil {
							errorChan <- errors.New("panic occurred")
						} else {
							errorChan <- nil
						}
					}()
					SafeMustVoid(fn)
				}()
			}
			wg.Wait()
			close(errorChan)

			Convey("Then it should not panic", func() {
				for err := range errorChan {
					So(err, ShouldBeNil)
				}
			})
		})
	})
}
