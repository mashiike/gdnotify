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

func KeyValues[E, V any, K comparable](s []E, fn func(E) (K, V)) map[K]V {
	m := make(map[K]V)
	for _, v := range s {
		k, v := fn(v)
		m[k] = v
	}
	return m
}
