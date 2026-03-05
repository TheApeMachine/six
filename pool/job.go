package pool

/*
Job represents a unit of work that can be executed by a Worker.
We use a simple function type for maximum flexibility across the codebase.
*/
type Job func()
