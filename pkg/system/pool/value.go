package pool

import "time"

/*
PoolValue is a value that can be sent to the pool.
*/
type PoolValue[T any] struct {
	Key   string
	Value T
	TTL   time.Duration
}

/*
opts is a function that configures a PoolValue.
*/
type opts[T any] func(*PoolValue[T])

/*
NewPoolValue creates a new PoolValue.
*/
func NewPoolValue[T any](opts ...opts[T]) *PoolValue[T] {
	poolValue := &PoolValue[T]{}

	for _, opt := range opts {
		opt(poolValue)
	}

	return poolValue
}

/*
WithKey sets the key for the PoolValue.
*/
func WithKey[T any](key string) opts[T] {
	return func(poolValue *PoolValue[T]) {
		poolValue.Key = key
	}
}

/*
WithValue sets the value for the PoolValue.
*/
func WithValue[T any](value T) opts[T] {
	return func(poolValue *PoolValue[T]) {
		poolValue.Value = value
	}
}

/*
WithPoolValueTTL sets the TTL for the PoolValue.
*/
func WithPoolValueTTL[T any](ttl time.Duration) opts[T] {
	return func(poolValue *PoolValue[T]) {
		poolValue.TTL = ttl
	}
}


