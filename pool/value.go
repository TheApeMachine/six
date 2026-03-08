package pool

import "time"

/*
PoolValue is a value that can be sent to the pool.
*/
type PoolValue struct {
	Key   string
	Value any
	TTL   time.Duration
}

/*
opts is a function that configures a PoolValue.
*/
type opts func(*PoolValue)

/*
NewPoolValue creates a new PoolValue.
*/
func NewPoolValue(opts ...opts) *PoolValue {
	poolValue := &PoolValue{}

	for _, opt := range opts {
		opt(poolValue)
	}

	return poolValue
}

/*
WithKey sets the key for the PoolValue.
*/
func WithKey(key string) opts {
	return func(poolValue *PoolValue) {
		poolValue.Key = key
	}
}

/*
WithValue sets the value for the PoolValue.
*/
func WithValue(value any) opts {
	return func(poolValue *PoolValue) {
		poolValue.Value = value
	}
}
