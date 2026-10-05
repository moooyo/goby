package media

import (
	"context"
	"io"
	"os"
	"time"
)

func runBackgroundClipProcess(ctx context.Context, executable string, input *os.File, args []string, timeout time.Duration, stdoutLimit int64,
	stderr analysisStderrSink, parse func(io.Reader) error, gpu bool, executables ...*os.File) error {
	return runAnalysisProcessProfile(ctx, executable, input, nil, args, timeout, stdoutLimit, stderr, parse, gpu, executables...)
}

func backgroundClipProcessLimits(gpu bool, executable string) []string {
	memory := "--as=2147483648:2147483648"
	if gpu {
		// Vulkan drivers reserve device address ranges independently of their
		// resident CPU allocations. Bound private data instead of the entire
		// virtual address space; this does not claim a GPU memory ceiling.
		memory = "--data=2147483648:2147483648"
	}
	return []string{memory, "--nofile=64:64", "--fsize=0:0", "--", executable}
}

func backgroundClipGPUEnvironment() []string {
	environment := mediaProbeEnvironment()
	// Pure DRM/Vulkan processing uses software HEVC decoding and H.264
	// encoding. Do not inherit unrelated VAAPI/CUDA or server settings.
	if directory, exists := os.LookupEnv("XDG_RUNTIME_DIR"); exists {
		environment = append(environment, "XDG_RUNTIME_DIR="+directory)
	}
	// The file-size limit applies to driver writes too. Keep shader caches
	// disabled instead of granting the media process a writable cache path.
	return append(environment, "MESA_SHADER_CACHE_DISABLE=true", "__GL_SHADER_DISK_CACHE=0", "CUDA_CACHE_DISABLE=1")
}
