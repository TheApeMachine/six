package algebra

import "math"

/*
Möbius Inversion: Reconstructing Components From Composites
For square-free numbers (which all Values are), the Möbius function is:

	μ(n) = (-1)^k

where k is the number of active primes. This is the parity of the popcount.
Even number of primes → μ = +1, odd → μ = -1.
The Möbius inversion formula:

	g(n) = Σ_{d|n} f(d), then f(n) = Σ_{d|n} μ(n/d) · g(d).

In plain terms: if you have an aggregate g that sums contributions from all
divisors of n, the Möbius function recovers the individual contribution
f at n itself. This inverts accumulation.
Applied to the Value lattice: OR (= LCM) merges Values. The primes of the
individual contributors are mixed into one composite. Standard information
theory says this is irreversible — OR destroys information about which
inputs produced the output. On the prime-indexed lattice, this is wrong.
OR does not destroy information if the lattice structure is preserved.
The divisors of the composite Value are all the Values whose primes are
subsets of the composite's primes. The Möbius function, applied over these
divisors, reconstructs which specific combinations contributed to the composite.
This works because the lattice of square-free numbers is a distributive lattice
where inclusion-exclusion is exact. Every subset of primes corresponds to a
unique divisor. The Möbius function alternates signs over subset sizes, canceling
the over-counting that OR introduces. The reconstruction is not a heuristic or
approximation — it is algebraically exact. This means the system can compose
Values freely (via OR, AND, XOR, motor composition) and later decompose the results
back into their constituents. Folding is reversible. Accumulation is undoable.
The state-space supports both synthesis and analysis as exact inverses.

Or, in other words: The Shannon mic-drop.
*/
func Mobius(n int) int {
	return int(math.Pow(-1, float64(n)))
}
