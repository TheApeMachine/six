package data

/*
ListRing is one element of a circular list. There is no distinguished head;
any *ListRing[T] refers to the whole ring. Nil *ListRing[T] means an empty
ring. The zero value for a non-nil ListRing is a one-element ring whose Value
is the zero value of T.

Behavior matches stdlib container/ring, except Value is typed as T instead of
any, and Do takes func(T).

Empty rings are represented as nil *ListRing pointers (same as container/ring).
*/
type ListRing[T any] struct {
	next, prev *ListRing[T]
	Value      T
}

/*
init makes r a circular list of length 1.
*/
func (ring *ListRing[T]) init() *ListRing[T] {
	ring.next = ring
	ring.prev = ring

	return ring
}

/*
Next returns the next ring element. ring must not be nil.
*/
func (ring *ListRing[T]) Next() *ListRing[T] {
	if ring.next == nil {
		return ring.init()
	}

	return ring.next
}

/*
Prev returns the previous ring element. ring must not be nil.
*/
func (ring *ListRing[T]) Prev() *ListRing[T] {
	if ring.next == nil {
		return ring.init()
	}

	return ring.prev
}

/*
Move moves n % Len() elements backward (n < 0) or forward (n >= 0) in the
ring and returns that element. ring must not be nil.
*/
func (ring *ListRing[T]) Move(step int) *ListRing[T] {
	if ring.next == nil {
		return ring.init()
	}

	switch {
	case step < 0:
		for ; step < 0; step++ {
			ring = ring.prev
		}
	case step > 0:
		for ; step > 0; step-- {
			ring = ring.next
		}
	}

	return ring
}

/*
NewListRing creates a ring of elementCount elements, each with Value set to the
zero value of T. Returns nil if elementCount <= 0.
*/
func NewListRing[T any](elementCount int) *ListRing[T] {
	if elementCount <= 0 {
		return nil
	}

	head := new(ListRing[T])
	tail := head

	for index := 1; index < elementCount; index++ {
		tail.next = &ListRing[T]{prev: tail}
		tail = tail.next
	}

	tail.next = head
	head.prev = tail

	return head
}

/*
Link connects ring with other such that ring.Next() becomes other and returns
the previous ring.Next(). ring must not be nil.

If ring and other point to the same ring, linking removes the elements between
them and returns a reference to the removed subring. If they point to
different rings, other is inserted after ring.
*/
func (ring *ListRing[T]) Link(other *ListRing[T]) *ListRing[T] {
	next := ring.Next()

	if other != nil {
		tail := other.Prev()
		ring.next = other
		other.prev = ring
		next.prev = tail
		tail.next = next
	}

	return next
}

/*
Unlink removes count % Len() elements starting at ring.Next(). If
count % Len() == 0, ring is unchanged. Returns the removed subring, or nil if
count <= 0. ring must not be nil.
*/
func (ring *ListRing[T]) Unlink(count int) *ListRing[T] {
	if count <= 0 {
		return nil
	}

	return ring.Link(ring.Move(count + 1))
}

/*
Len is the number of elements in the ring. Time is O(n). ring must not be nil.
*/
func (ring *ListRing[T]) Len() int {
	if ring == nil {
		return 0
	}

	length := 1

	for walk := ring.Next(); walk != ring; walk = walk.next {
		length++
	}

	return length
}

/*
Do calls visitor on each element in forward order, starting at ring. Behavior
is undefined if visitor mutates the ring structure. ring may be nil (no-op).
*/
func (ring *ListRing[T]) Do(visitor func(T)) {
	if ring == nil {
		return
	}

	visitor(ring.Value)

	for walk := ring.Next(); walk != ring; walk = walk.next {
		visitor(walk.Value)
	}
}
