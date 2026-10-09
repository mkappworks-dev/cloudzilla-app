package handler

import "testing"

// UseExpandAllBudget caps how many folders Expand all opens until t ends.
func UseExpandAllBudget(t testing.TB, n int) {
	prev := expandAllBudget
	expandAllBudget = n
	t.Cleanup(func() { expandAllBudget = prev })
}

var SafeNextPath = safeNextPath

const MaxRawBlobBytes = maxRawBlobBytes
