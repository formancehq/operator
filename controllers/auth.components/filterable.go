package authcomponents

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
