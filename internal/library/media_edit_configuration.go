package library

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/moooyo/goby/internal/config"
	"github.com/moooyo/goby/internal/media"
)

type mediaEditExecutionSnapshot struct {
	Configuration config.MediaOperationsConfig
	FFmpegPath    string
	FFprobePath   string
	FFmpegSHA256  string
	FFprobeSHA256 string
}

func readMediaEditExecution(operation MediaOperation, ffmpeg, ffprobe, container string) (mediaEditExecutionSnapshot, time.Duration, int64, error) {
	var execution mediaEditExecutionSnapshot
	if len(operation.ExecutionSnapshot) == 0 || len(operation.ExecutionSnapshot) > MaxMediaOperationDocumentBytes || json.Unmarshal(operation.ExecutionSnapshot, &execution) != nil || !execution.Configuration.Enabled || execution.Configuration.Validate() != nil || execution.FFmpegPath != ffmpeg || execution.FFprobePath != ffprobe || !filepath.IsAbs(ffmpeg) || !filepath.IsAbs(ffprobe) || !mediaOperationHashValid(execution.FFmpegSHA256, false) || !mediaOperationHashValid(execution.FFprobeSHA256, false) {
		return execution, 0, 0, ErrInvalidInput
	}
	profile := "matroska-v1"
	if container == "mp4" {
		profile = "mp4-movtext-v1"
	}
	allowed := false
	for _, value := range execution.Configuration.WritableProfiles {
		if value == profile {
			allowed = true
		}
	}
	if !allowed || operation.Parameters.Profile != "" && operation.Parameters.Profile != profile {
		return execution, 0, 0, ErrForbidden
	}
	timeout := min(time.Duration(execution.Configuration.MaxRuntimeSeconds)*time.Second, media.MaxSubtitleRemovalTimeout)
	return execution, timeout, execution.Configuration.MaxScratchBytes, nil
}

func verifyMediaEditTools(ctx context.Context, execution mediaEditExecutionSnapshot) error {
	for _, tool := range []struct{ path, hash string }{{execution.FFmpegPath, execution.FFmpegSHA256}, {execution.FFprobePath, execution.FFprobeSHA256}} {
		if err := mediaEditTrustedToolPath(tool.path); err != nil {
			return err
		}
		before, err := os.Lstat(tool.path)
		if err != nil || !before.Mode().IsRegular() || before.Mode().Perm()&0111 == 0 || before.Size() <= 0 || before.Size() > 512<<20 {
			return errors.Join(ErrUnavailable, err)
		}
		file, err := os.Open(tool.path)
		if err != nil {
			return err
		}
		held, statErr := file.Stat()
		if statErr != nil || !os.SameFile(before, held) {
			_ = file.Close()
			return ErrSourceChanged
		}
		digest, hashErr := mediaEditDigest(ctx, file, held.Size())
		current, currentErr := os.Lstat(tool.path)
		closeErr := file.Close()
		if hashErr != nil || currentErr != nil || closeErr != nil || !sameMediaSourceFile(held, current) || digest != tool.hash {
			return errors.Join(ErrSourceChanged, hashErr, currentErr, closeErr)
		}
	}
	return nil
}
