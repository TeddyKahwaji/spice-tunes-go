package funcs

type mapperFunc[T any, R any] func(T) R

func Map[T any, R any](input []T, mapper mapperFunc[T, R]) []R {
	result := make([]R, len(input))

	for i := range len(input) {
		result[i] = mapper(input[i])
	}

	return result
}

type validatorFunc[T any] func(T) bool

func Any[T any](input []T, expression validatorFunc[T]) bool {
	for _, elem := range input {
		if expression(elem) {
			return true
		}
	}

	return false
}

func All[T any](input []T, expression validatorFunc[T]) bool {
	for _, elem := range input {
		if !expression(elem) {
			return false
		}
	}

	return true
}
