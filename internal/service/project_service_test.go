package service

import (
	"testing"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

func TestProjectListView_Progress(t *testing.T) {
	cases := []struct {
		name         string
		linked, done int
		want         int
	}{
		{"no linked cards", 0, 0, 0},
		{"linked but none done", 5, 0, 0},
		{"quarter done", 8, 2, 25},
		{"all done", 8, 8, 100},
		{"integer truncation rounds down", 3, 1, 33},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			v := ProjectListView{LinkedCount: c.linked, DoneCount: c.done}
			if got := v.Progress(); got != c.want {
				t.Errorf("Progress() = %d, want %d", got, c.want)
			}
		})
	}
}

func TestProjectListView_IsClosed(t *testing.T) {
	if (ProjectListView{}).IsClosed() {
		t.Error("project with nil ClosedAt: IsClosed() = true, want false")
	}
	now := time.Now()
	closed := ProjectListView{Project: model.Project{ClosedAt: &now}}
	if !closed.IsClosed() {
		t.Error("project with set ClosedAt: IsClosed() = false, want true")
	}
}
