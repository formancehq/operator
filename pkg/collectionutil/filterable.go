package collectionutil

type Filterable[T any] []T

func (f Filterable[T]) Filter(checkFn func(t T) bool) []T {
	ret := make([]T, 0)
	for _, item := range f {
		if checkFn(item) {
			ret = append(ret, item)
		}
	}
	return ret
}

func (f Filterable[T]) First(checkFn func(t T) bool) *T {
	ret := f.Filter(checkFn)
	if len(ret) > 0 {
		return &ret[0]
	}
	return nil
}

func Filter[T any](items []T, checkFn func(t T) bool) []T {
	return Filterable[T](items).Filter(checkFn)
}

func First[T any](items []T, checkFn func(t T) bool) *T {
	return Filterable[T](items).First(checkFn)
}

func NotIn[T comparable](items ...T) func(t T) bool {
	return func(t T) bool {
		for _, item := range items {
			if item == t {
				return false
			}
		}
		return true
	}
}

func Equal[T comparable](value T) func(t T) bool {
	return func(t T) bool {
		return t == value
	}
}
