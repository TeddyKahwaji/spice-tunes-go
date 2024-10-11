package funcs

type mapperFunc[T any, R any] func(T) R

func Map[T any, R any](input []T, mapper mapperFunc[T, R]) []R {
	result := make([]R, len(input))

	for i := range len(input) {
		result[i] = mapper(input[i])
	}

	return result
}