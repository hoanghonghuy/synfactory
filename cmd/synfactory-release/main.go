package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	releasefactory "github.com/hoanghonghuy/synfactory/internal/release"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "publish":
		err = runPublish(os.Args[2:])
	case "promote":
		err = runPromotion("promote", os.Args[2:])
	case "rollback":
		err = runPromotion("rollback", os.Args[2:])
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "release error:", err)
		os.Exit(1)
	}
}

func runPublish(args []string) error {
	fs := flag.NewFlagSet("publish", flag.ContinueOnError)
	version := fs.String("version", "", "immutable release version")
	sourceSHA := fs.String("source-sha", "", "exact gated 40-character source SHA")
	evidencePath := fs.String("evidence", "", "path to retained release-evidence manifest.json")
	registryPrefix := fs.String("registry", "", "OCI repository prefix, for example ghcr.io/acme/synfactory")
	outputPath := fs.String("output", "", "durable release manifest path")
	attempts := fs.Int("attempts", 3, "maximum attempts for transient registry failures")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *version == "" || *sourceSHA == "" || *evidencePath == "" || *registryPrefix == "" || *outputPath == "" || *outputPath == "-" {
		return errors.New("publish requires --version, --source-sha, --evidence, --registry and a durable --output path")
	}
	raw, err := os.ReadFile(*evidencePath)
	if err != nil {
		return fmt.Errorf("read release evidence: %w", err)
	}
	evidence, err := releasefactory.ParseEvidence(raw, *sourceSHA)
	if err != nil {
		return err
	}
	input, err := releasefactory.PublishInputFromEvidence(*version, *sourceSHA, *registryPrefix, evidence)
	if err != nil {
		return err
	}
	alreadyRecorded, err := verifyExistingRelease(*outputPath, input)
	if err != nil {
		return err
	}
	if alreadyRecorded {
		fmt.Fprintln(os.Stderr, "release already recorded; build and registry publish skipped")
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	if err := releasefactory.VerifyCheckoutSource(ctx, *sourceSHA, nil); err != nil {
		return err
	}
	if err := releasefactory.BuildAndVerifyEvidenceImages(ctx, evidence, nil); err != nil {
		return err
	}
	manifest, err := (releasefactory.Publisher{
		Registry: releasefactory.DockerRegistry{},
		Attempts: *attempts,
		Backoff:  2 * time.Second,
	}).Publish(ctx, input)
	if err != nil {
		return err
	}
	return writeJSON(*outputPath, manifest)
}

func verifyExistingRelease(path string, input releasefactory.PublishInput) (bool, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read existing release manifest: %w", err)
	}
	var existing releasefactory.Manifest
	if err := json.Unmarshal(raw, &existing); err != nil {
		return false, fmt.Errorf("decode existing release manifest: %w", err)
	}
	if err := existing.Validate(); err != nil {
		return false, err
	}
	if existing.Version != input.Version || existing.SourceSHA != input.SourceSHA || existing.EvidenceSHA256 != input.Evidence.ManifestSHA256 || existing.WebLockSHA256 != input.Evidence.WebLockSHA256 {
		return false, fmt.Errorf("%w: output path is already bound to another release identity", releasefactory.ErrIdentityConflict)
	}
	for scanner, version := range input.Evidence.Scanners {
		if existing.Scanners[scanner] != version {
			return false, fmt.Errorf("%w: recorded release scanner provenance differs for %s", releasefactory.ErrIdentityConflict, scanner)
		}
	}
	for _, image := range existing.Images {
		candidate, ok := input.Images[image.Name]
		sbom := input.Evidence.SBOMs[image.Name]
		if !ok || candidate.Repository != image.Repository || candidate.SBOMSHA256 != image.SBOMSHA256 || sbom.Path != image.SBOMPath {
			return false, fmt.Errorf("%w: recorded release differs from selected evidence for %s", releasefactory.ErrIdentityConflict, image.Name)
		}
	}
	return true, nil
}

func runPromotion(action string, args []string) error {
	fs := flag.NewFlagSet(action, flag.ContinueOnError)
	manifestPath := fs.String("manifest", "", "path to an existing known-good immutable release manifest")
	environment := fs.String("environment", "", "target environment")
	outputPath := fs.String("output", "-", "promotion document path, or - for stdout")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *manifestPath == "" || *environment == "" {
		return fmt.Errorf("%s requires --manifest and --environment", action)
	}
	raw, err := os.ReadFile(*manifestPath)
	if err != nil {
		return fmt.Errorf("read release manifest: %w", err)
	}
	var manifest releasefactory.Manifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return fmt.Errorf("decode release manifest: %w", err)
	}
	promotion, err := releasefactory.NewPromotion(*environment, manifest)
	if err != nil {
		return err
	}
	if err := releasefactory.ValidatePromotion(promotion); err != nil {
		return err
	}
	return writeJSON(*outputPath, promotion)
}

func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if path == "-" {
		_, err = os.Stdout.Write(data)
		return err
	}
	dir := filepath.Dir(path)
	if dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(temporary, path); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	return nil
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: synfactory-release <publish|promote|rollback> [flags]")
}
