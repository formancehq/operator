package collectionutil

type Array[T any] []T

func (f Array[T]) Filter(checkFn func(t T) bool) []T {
	ret := make([]T, 0)
	for _, item := range f {
		if checkFn(item) {
			ret = append(ret, item)
		}
	}
	return ret
}

func (f Array[T]) First(checkFn func(t T) bool) *T {
	ret := f.Filter(checkFn)
	if len(ret) > 0 {
		return &ret[0]
	}
	return nil
}

func (f *Array[T]) Append(t T) {
	*f = append(*f, t)
}

func Filter[T any](items []T, checkFn func(t T) bool) []T {
	return Array[T](items).Filter(checkFn)
}

func First[T any](items []T, checkFn func(t T) bool) *T {
	return Array[T](items).First(checkFn)
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
