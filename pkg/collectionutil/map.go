package collectionutil

func Create(args ...string) map[string]string {
	if len(args)%2 != 0 {
		panic("odd number of args pass to maputil.Create()")
	}
	ret := make(map[string]string)
	for i := 0; i < len(args); i += 2 {
		ret[args[i]] = args[i+1]
	}
	return ret
}
