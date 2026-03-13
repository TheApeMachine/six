package vm

/*
Readiness is an optional interface a System can implement to signal
that its initial data ingestion is complete.
*/
type Readiness interface {
	Ready() bool
}
