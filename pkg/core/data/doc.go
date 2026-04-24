/*
Package data provides two complementary “ring” shapes:

  - Ring[T] is a bounded Vyukov multi-producer multi-consumer queue (Push/Pop).
  - ListRing[T] is a circular doubly-linked list with container/ring semantics
    (Next/Prev/Move/Link/Unlink/Do): element instances, no queue discipline.

Use Ring when you need lock-free handoff between goroutines; use ListRing when
you need a fixed-size cyclic structure for iteration or in-place rewiring.
*/
package data
