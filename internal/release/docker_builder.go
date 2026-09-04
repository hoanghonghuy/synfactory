package release

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func VerifyCheckoutSource(ctx context.Context, expectedSourceSHA string, runner CommandRunner) error {
	if !isHexSHA(expectedSourceSHA, 40) {
		return fmt.Errorf("%w: expected checkout source must be a 40-character git SHA", ErrInvalidRelease)
	}
	if runner == nil {
		runner = ExecRunner{}
	}
	output, err := runner.CombinedOutput(ctx, "git", "rev-parse", "HEAD")
	if err != nil {
		return fmt.Errorf("%w: resolve checkout HEAD: %v", ErrInvalidRelease, err)
	}
	actual := strings.TrimSpace(string(output))
	if actual != expectedSourceSHA {
		return fmt.Errorf("%w: checkout HEAD %s does not match gated source %s", ErrInvalidRelease, actual, expectedSourceSHA)
	}
	output, err = runner.CombinedOutput(ctx, "git", "diff", "--name-only", "HEAD", "--")
	if err != nil {
		return fmt.Errorf("%w: inspect checkout modifications: %v", ErrInvalidRelease, err)
	}
	if strings.TrimSpace(string(output)) != "" {
		return fmt.Errorf("%w: tracked checkout files differ from gated source", ErrInvalidRelease)
	}
	return nil
}

func LoadAndVerifyEvidenceImages(ctx context.Context, evidence Evidence, evidenceDir string, runner CommandRunner) error {
	if err := evidence.Validate(evidence.SourceSHA); err != nil {
		return err
	}
	if strings.TrimSpace(evidenceDir) == "" {
		return fmt.Errorf("%w: evidence directory is required", ErrInvalidRelease)
	}
	if runner == nil {
		runner = ExecRunner{}
	}
	for _, name := range requiredImages {
		image := evidence.Images[name]
		archivePath := filepath.Join(evidenceDir, filepath.FromSlash(image.ArchivePath))
		digest, err := fileSHA256(archivePath)
		if err != nil {
			return fmt.Errorf("%w: read %s image archive: %v", ErrInvalidRelease, name, err)
		}
		if digest != image.ArchiveSHA256 {
			return fmt.Errorf("%w: %s image archive sha256 mismatch", ErrInvalidRelease, name)
		}
		output, err := runner.CombinedOutput(ctx, "docker", "image", "load", "-i", archivePath)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return err
			}
			return fmt.Errorf("%w: load %s image archive: %v: %s", ErrInvalidRelease, name, err, strings.TrimSpace(string(output)))
		}
		output, err = runner.CombinedOutput(ctx, "docker", "image", "inspect", "--format", "{{.Id}}", image.LocalID)
		if err != nil {
			return fmt.Errorf("%w: inspect loaded %s image: %v", ErrInvalidRelease, name, err)
		}
		loadedID := strings.TrimSpace(string(output))
		if loadedID != image.LocalID {
			return fmt.Errorf("%w: loaded %s image identity %s does not match gated evidence %s", ErrInvalidRelease, name, loadedID, image.LocalID)
		}
	}
	return nil
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = file.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
