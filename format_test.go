package main

import (
	"os/exec"
	"strings"
	"testing"
)

// TestSourceFormatted fails if any Go file in the package is not gofmt-clean.
// Enforcing formatting from a test keeps the check portable across platforms
// and shells (Go runs gofmt directly, so it does not depend on make's shell),
// and ensures gofmt is exercised by every `go test` / `make test` run.
func TestSourceFormatted(t *testing.T) {
	out, err := exec.Command("gofmt", "-l", ".").Output()
	if err != nil {
		t.Skipf("gofmt unavailable: %v", err)
	}
	if files := strings.TrimSpace(string(out)); files != "" {
		t.Errorf("the following files are not gofmt-clean (run `make fmt`):\n%s", files)
	}
}
