package service

import (
	"errors"
	"testing"

	"github.com/go-git/go-git/v5/plumbing/protocol/packp"
)

func TestPushVet_RefusesPullRefs(t *testing.T) {
	err := (&PushVet{}).Check(&packp.Command{Name: "refs/pull/1/head"})
	if !errors.Is(err, ErrPullRefPush) {
		t.Fatalf("err = %v, want ErrPullRefPush", err)
	}
}
