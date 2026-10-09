package cli

import (
	"os/exec"
	"strings"
	"testing"
)

func TestClientLayerAvoidsStoreAndService(t *testing.T) {
	out, err := exec.Command("go", "list", "-deps", "-f", "{{.ImportPath}}",
		"github.com/mkappworks-dev/cloudzilla-app/internal/cli",
		"github.com/mkappworks-dev/cloudzilla-app/cmd/cz").CombinedOutput()
	if err != nil {
		t.Fatalf("go list: %v\n%s", err, out)
	}
	for _, pkg := range strings.Fields(string(out)) {
		if strings.HasSuffix(pkg, "/internal/store") || strings.HasSuffix(pkg, "/internal/service") {
			t.Errorf("cz depends on %s", pkg)
		}
	}
}
