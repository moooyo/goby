//go:build !linux

package media

import "context"

// No unsandboxed fallback is available on platforms without the Linux process
// and cgroup resource owner. No tool or media command is started here.
func openDiagnosticExecution(context.Context, diagnosticProcessOptions) (diagnosticExecutionBackend, error) {
	return nil, ErrDiagnosticResources
}
