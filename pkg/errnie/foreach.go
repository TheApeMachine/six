package errnie

/*
ForEach runs fn(i) for i in [0, n), short-circuiting on the first error.
Replaces the per-iteration if-err idiom in counted loops.
*/
func ForEach(n int, fn func(int) error) error {
	for index := 0; index < n; index++ {
		if err := fn(index); err != nil {
			return err
		}
	}

	return nil
}


