package components_test

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/view/components"
)

var retargetGuard = regexp.MustCompile(`getResponseHeader\('HX-Retarget'\)\)\s*return`)

// renderFormError answers a dialog form's error with 200 and HX-Retarget, which
// htmx counts as successful, so data-toast must not read that as success.
func TestConfirmDialog_DataToastSkipsFormErrors(t *testing.T) {
	var sb strings.Builder
	if err := components.ConfirmDialog().Render(context.Background(), &sb); err != nil {
		t.Fatalf("render: %v", err)
	}
	_, listener, ok := strings.Cut(sb.String(), "addEventListener('htmx:afterRequest'")
	if !ok {
		t.Fatal("no htmx:afterRequest listener")
	}
	listener, _, _ = strings.Cut(listener, "});")

	guard := retargetGuard.FindStringIndex(listener)
	toast := strings.Index(listener, "getAttribute('data-toast')")
	if toast < 0 {
		t.Fatalf("listener no longer reads data-toast:\n%s", listener)
	}
	if guard == nil || guard[0] > toast {
		t.Errorf("listener shows data-toast for a response carrying HX-Retarget:\n%s", listener)
	}
}
