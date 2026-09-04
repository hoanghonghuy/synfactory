package release

import (
	"context"
	"errors"
	"fmt"
	"time"
)

var (
	ErrRegistryAuth      = errors.New("registry authentication failed")
	ErrRegistryPolicy    = errors.New("registry policy rejected operation")
	ErrRegistryTransient = errors.New("registry transient failure")
)

type Registry interface {
	Push(ctx context.Context, repository, tag, sourceImage string) (digest string, err error)
}

type Publisher struct {
	Registry Registry
	Attempts int
	Backoff  time.Duration
}

type PublishInput struct {
	Version   string
	SourceSHA string
	Evidence  Evidence
	Images    map[string]struct {
		Repository  string
		SourceImage string
		SBOMSHA256  string
	}
}

func (p Publisher) Publish(ctx context.Context, input PublishInput) (Manifest, error) {
	if p.Registry == nil {
		return Manifest{}, fmt.Errorf("%w: registry backend is required", ErrInvalidRelease)
	}
	if err := input.Evidence.Validate(input.SourceSHA); err != nil {
		return Manifest{}, err
	}
	attempts := p.Attempts
	if attempts <= 0 {
		attempts = 3
	}
	backoff := p.Backoff
	if backoff <= 0 {
		backoff = time.Second
	}

	manifest := Manifest{Version: input.Version, SourceSHA: input.SourceSHA}
	for _, name := range requiredImages {
		candidate, ok := input.Images[name]
		if !ok || candidate.Repository == "" || candidate.SourceImage == "" || !isHexSHA(candidate.SBOMSHA256, 64) {
			return Manifest{}, fmt.Errorf("%w: invalid publish input for %s", ErrInvalidRelease, name)
		}
		digest, err := p.pushWithRetry(ctx, attempts, backoff, candidate.Repository, input.Version, candidate.SourceImage)
		if err != nil {
			return Manifest{}, fmt.Errorf("publish %s: %w", name, err)
		}
		manifest.Images = append(manifest.Images, Image{
			Name: name, Repository: candidate.Repository, Digest: digest, SBOMSHA256: candidate.SBOMSHA256,
		})
	}
	if err := manifest.Validate(); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func (p Publisher) pushWithRetry(ctx context.Context, attempts int, backoff time.Duration, repository, tag, sourceImage string) (string, error) {
	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		digest, err := p.Registry.Push(ctx, repository, tag, sourceImage)
		if err == nil {
			if !digestPattern.MatchString(digest) {
				return "", fmt.Errorf("%w: registry returned non-sha256 digest", ErrRegistryPolicy)
			}
			return digest, nil
		}
		lastErr = err
		if errors.Is(err, ErrRegistryAuth) || errors.Is(err, ErrRegistryPolicy) || !errors.Is(err, ErrRegistryTransient) {
			return "", err
		}
		if attempt == attempts {
			break
		}
		timer := time.NewTimer(backoff * time.Duration(attempt))
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return "", ctx.Err()
		case <-timer.C:
		}
	}
	return "", fmt.Errorf("registry retries exhausted after %d attempts: %w", attempts, lastErr)
}
