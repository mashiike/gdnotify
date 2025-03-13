package gdnotify

import (
	"iter"
	"slices"
)

func IterMap[E, F any](seq iter.Seq[E], fn func(E) F) iter.Seq[F] {
	return func(yield func(F) bool) {
		for v := range seq {
			e := fn(v)
			if !yield(e) {
				break
			}
		}
	}
}

func Map[E, F any](s []E, fn func(E) F) []F {
	return slices.Collect(IterMap(slices.Values(s), fn))
}
