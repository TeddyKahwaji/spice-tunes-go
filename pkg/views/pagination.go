package views

// Pagination Configurations can assist in creating pagination views.
type PaginationConfig[T any] struct {
	Data      []*T
	Separator int
}

func NewPaginatedConfig[T any](data []*T, separator int) *PaginationConfig[T] {
	return &PaginationConfig[T]{
		Data:      data,
		Separator: separator,
	}
}

// GetPages splits the data into pages based on the provided separator.
// Each page will contain a slice of data, where the number of elements in each
// page is determined by the separator. The function returns a 2D slice, where
// each inner slice represents a page.
func (p *PaginationConfig[T]) GetPages() [][]*T {
	pages := [][]*T{}

	for i, elem := range p.Data {
		if i%p.Separator == 0 {
			pages = append(pages, []*T{})
		}

		pages[len(pages)-1] = append(pages[len(pages)-1], elem)
	}

	return pages
}
