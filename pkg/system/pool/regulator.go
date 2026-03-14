package pool

/*
Regulator is the interface for flow-control components (circuit breakers,
back-pressure regulators, load balancers, etc.).
*/
type Regulator interface {
	Observe(metrics *Metrics)
	Limit() bool
	Renormalize()
}
