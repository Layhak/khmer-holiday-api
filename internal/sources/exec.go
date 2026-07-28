package sources

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

// getenv is a thin indirection so tests can stub the environment.
var getenv = os.Getenv

const (
	maxFetchOutput = 16 << 20
	maxFetchStderr = 4 << 10
)

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

	stderr := &cappedWriter{remaining: maxFetchStderr}
	cmd.Stderr = stderr

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("fetch command %q stdout: %w", fields[0], err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start fetch command %q: %w", fields[0], err)
	}

	out, readErr := io.ReadAll(io.LimitReader(stdout, maxFetchOutput+1))
	if int64(len(out)) > maxFetchOutput {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, fmt.Errorf("fetch command %q output exceeds %d-byte limit",
			fields[0], maxFetchOutput)
	}
	waitErr := cmd.Wait()
	if readErr != nil {
		return nil, fmt.Errorf("read fetch command %q output: %w", fields[0], readErr)
	}
	if waitErr != nil {
		return nil, fmt.Errorf("fetch command %q failed for %s: %w (stderr: %s)",
			fields[0], url, waitErr, strings.TrimSpace(stderr.String()))
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("fetch command %q returned empty output for %s", fields[0], url)
	}
	return out, nil
}

type cappedWriter struct {
	b         strings.Builder
	remaining int
}

func (w *cappedWriter) Write(p []byte) (int, error) {
	n := len(p)
	if w.remaining > 0 {
		keep := len(p)
		if keep > w.remaining {
			keep = w.remaining
		}
		_, _ = w.b.Write(p[:keep])
		w.remaining -= keep
	}
	return n, nil
}

func (w *cappedWriter) String() string { return w.b.String() }
