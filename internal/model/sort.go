package model

// SortColumn is a column a list can be ordered by.
type SortColumn struct {
	Key string
	// DescFirst is the direction a first click on the column picks: newest first for dates, A to Z for names.
	DescFirst bool
}

// SortSpec lists a page's sortable columns; the first is the default order.
type SortSpec []SortColumn

// Resolve validates a request's sort and dir parameters, falling back to the default column and a column's first
// direction when either is missing or unknown.
func (s SortSpec) Resolve(key, dir string) (string, bool) {
	col := s[0]
	for _, c := range s {
		if c.Key == key {
			col = c
			break
		}
	}
	switch dir {
	case "asc":
		return col.Key, false
	case "desc":
		return col.Key, true
	}
	return col.Key, col.DescFirst
}

// Toggle is the direction a header link for key sorts in: the opposite of the current one when key is already
// sorted, else key's first direction.
func (s SortSpec) Toggle(current string, currentDesc bool, key string) bool {
	if current == key {
		return !currentDesc
	}
	for _, c := range s {
		if c.Key == key {
			return c.DescFirst
		}
	}
	return false
}
