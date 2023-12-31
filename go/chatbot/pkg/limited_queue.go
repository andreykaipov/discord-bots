package pkg

type LimitedQueue[T any] struct {
	slice  []T
	sticky []T
	max    int
}

func NewLimitedQueue[T any](size int) *LimitedQueue[T] {
	return &LimitedQueue[T]{
		slice:  make([]T, 0),
		sticky: make([]T, 0),
		max:    size,
	}
}

func (q *LimitedQueue[T]) Add(item T) {
	// append the new item
	q.slice = append(q.slice, item)

	// remove the oldest item if the size is exceeded
	if len(q.slice) > q.max {
		q.slice = q.slice[1:]
	}
}

// AddSticky adds an item to the queue that will never be removed
func (q *LimitedQueue[T]) AddSticky(item T) {
	q.sticky = append(q.sticky, item)
}

// The sticky items are always at the beginning of the queue
func (q *LimitedQueue[T]) AllItems() []T {
	return append(q.sticky, q.slice...)
}

func (q *LimitedQueue[T]) ClearNonSticky() {
	q.slice = make([]T, 0)
}

func (q *LimitedQueue[T]) Items() []T {
	return q.slice
}

func (q *LimitedQueue[T]) LastN(n int) T {
	if len(q.slice) == 0 {
		return q.sticky[0]
	}
	return q.slice[len(q.slice)-n]
}
