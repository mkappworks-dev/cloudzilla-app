package pages

import "testing"

func TestHasActivity(t *testing.T) {
	t.Parallel()
	cases := []struct {
		chart []int
		want  bool
	}{
		{nil, false},
		{[]int{0, 0, 0, 0, 0}, false},
		{[]int{0, 0, 2, 0, 0}, true},
	}
	for _, c := range cases {
		if got := hasActivity(c.chart); got != c.want {
			t.Errorf("hasActivity(%v) = %v, want %v", c.chart, got, c.want)
		}
	}
}
