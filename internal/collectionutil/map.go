package collectionutil

func CreateMap(args ...string) map[string]string {
	if len(args)%2 != 0 {
		panic("odd number of args pass to maputil.Create()")
	}
	ret := make(map[string]string)
	for i := 0; i < len(args); i += 2 {
		ret[args[i]] = args[i+1]
	}
	return ret
}

func Map[T1 any, T2 any](v1 []T1, transformer func(T1) T2) []T2 {
	ret := make([]T2, 0)
	for _, v := range v1 {
		ret = append(ret, transformer(v))
	}
	return ret
}
