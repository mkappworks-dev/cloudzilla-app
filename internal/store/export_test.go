package store

func GhostReassignedColumns() []string {
	cols := make([]string, len(ghostReassignments))
	for i, r := range ghostReassignments {
		cols[i] = r.table + "." + r.idCol
	}
	return cols
}
