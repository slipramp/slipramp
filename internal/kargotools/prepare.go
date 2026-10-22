// Package kargotools prepares the read-only Git and Argo CD artifacts used to
// review a Kargo Promotion.
package kargotools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

type Runner struct {
	runCommand func(context.Context, string, []string, []string) ([]byte, error)
}

func NewRunner() *Runner {
	return &Runner{runCommand: runCommand}
}

func (r *Runner) Prepare(ctx context.Context, config Config) error {
	config.setDefaults()
	if err := config.validate(); err != nil {
		return err
	}

	targets, err := loadArgoTargets(config.ArgoTargetsPath)
	if err != nil {
		return err
	}
	target, found := targets[config.Environment]
	if !found {
		return fmt.Errorf("argo CD target is not configured for environment %q", config.Environment)
	}
	if err := target.validate(config.Environment); err != nil {
		return err
	}

	type result struct {
		kind  string
		value []byte
		err   error
	}

	workContext, cancel := context.WithCancel(ctx)
	defer cancel()
	results := make(chan result, 2)
	go func() {
		value, err := r.gitDiff(workContext, config)
		results <- result{kind: "git", value: value, err: err}
	}()
	go func() {
		value, err := r.argoDiff(workContext, config, target)
		results <- result{kind: "argo", value: value, err: err}
	}()

	var gitDiff []byte
	var argoOutput []byte
	var preparationErrors []error
	for range 2 {
		result := <-results
		if result.err != nil {
			preparationErrors = append(preparationErrors, result.err)
			cancel()
			continue
		}
		switch result.kind {
		case "git":
			gitDiff = result.value
		case "argo":
			argoOutput = result.value
		}
	}
	if err := errors.Join(preparationErrors...); err != nil {
		return err
	}
	gitDiff = normalizeTextArtifact(gitDiff)
	if len(gitDiff) > maximumDiffBytes {
		return fmt.Errorf("normalized Git diff exceeds %d bytes", maximumDiffBytes)
	}

	argoDiff, err := ParseArgoDiff(argoOutput)
	if err != nil {
		return fmt.Errorf("parse Argo CD diff: %w", err)
	}
	encodedArgoDiff, err := json.Marshal(argoDiff)
	if err != nil {
		return fmt.Errorf("encode Argo CD diff: %w", err)
	}
	encodedArgoDiff = append(encodedArgoDiff, '\n')
	if len(encodedArgoDiff) > maximumDiffBytes {
		return fmt.Errorf("encoded Argo CD diff exceeds %d bytes", maximumDiffBytes)
	}

	if err := writeArtifacts(
		config.GitDiffPath,
		gitDiff,
		config.ArgoDiffPath,
		encodedArgoDiff,
	); err != nil {
		return err
	}

	return nil
}
