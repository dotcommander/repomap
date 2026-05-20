package repomap

import (
	"errors"
	"fmt"
	"testing"
)

// Test_ExecExitCode_WrappedError verifies that ExecExitCode extracts the
// embedded code even when an execError is wrapped via fmt.Errorf("...: %w", err).
// Without errors.As, a direct type assertion would miss the wrapped value and
// return the default code 1.
//
// Lives in a separate file from commit_execute_secrets_test.go because that
// file is outside the writable scope for this change; this test still exercises
// the public ExecExitCode entry point used by the CLI commit/finish exit paths.
func Test_ExecExitCode_WrappedError(t *testing.T) {
	t.Parallel()
	inner := execError{code: 3, msg: "git push failed"}
	wrapped := fmt.Errorf("wrap: %w", inner)
	if code := ExecExitCode(wrapped); code != 3 {
		t.Errorf("ExecExitCode(wrapped) = %d, want 3", code)
	}
	doubleWrapped := fmt.Errorf("outer: %w", wrapped)
	if code := ExecExitCode(doubleWrapped); code != 3 {
		t.Errorf("ExecExitCode(doubleWrapped) = %d, want 3", code)
	}
	// Sanity: errors.As still recovers the original execError.
	var got execError
	if !errors.As(wrapped, &got) || got.code != 3 {
		t.Errorf("errors.As lost the embedded execError: ok=%v code=%d", errors.As(wrapped, &got), got.code)
	}
	// Plain (non-execError) defaults to exit code 1.
	if code := ExecExitCode(errors.New("plain")); code != 1 {
		t.Errorf("ExecExitCode(plain) = %d, want 1", code)
	}
	// Direct execError (unwrapped) still works.
	if code := ExecExitCode(execError{code: 4, msg: "postflight"}); code != 4 {
		t.Errorf("ExecExitCode(direct) = %d, want 4", code)
	}
}
