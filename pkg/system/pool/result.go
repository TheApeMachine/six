package pool

import "time"

/*
Result holds the outcome of a scheduled job.
*/
type Result struct {
	Value     any
	Error     error
	CreatedAt time.Time
	TTL       time.Duration
}

/*
NewResult creates a result with the given value.
*/
func NewResult(value any) *Result {
	return &Result{
		Value:     value,
		CreatedAt: time.Now(),
	}
}


