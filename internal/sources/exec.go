package sources

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// getenv is a thin indirection so tests can stub the environment.
var getenv = os.Getenv

// runFetchCmd runs an external fetcher and returns its stdout.
//
// The command is split on whitespace and the URL is appended as the final
// argument; it is executed directly, never through a shell, so a URL cannot be
// interpreted as shell syntax.
func runFetchCmd(ctx context.Context, cmdline, url string) ([]byte, error) {
	fields := strings.Fields(cmdline)
	if len(fields) == 0 {
		return nil, fmt.Errorf("fetch command is empty")
	}

	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()

	args := append(fields[1:], url)
	cmd := exec.CommandContext(ctx, fields[0], args...)

	var stderr strings.Builder
	cmd.Stderr = &stderr

	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("fetch command %q failed for %s: %w (stderr: %s)",
			fields[0], url, err, strings.TrimSpace(stderr.String()))
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("fetch command %q returned empty output for %s", fields[0], url)
	}
	return out, nil
}
