package resolver

import (
	"fmt"
	"strings"
)

// ResolvedInput mirrors Rust branch_resolution::ResolvedInput.
type ResolvedInput struct {
	Branch        string
	FallbackIndex int
}

// ResolvedOutputKind identifies the output branch action.
type ResolvedOutputKind string

const (
	ResolvedOutputExisting   ResolvedOutputKind = "existing"
	ResolvedOutputCreateFrom ResolvedOutputKind = "create_from"
)

// ResolvedOutput mirrors Rust branch_resolution::ResolvedOutput.
type ResolvedOutput struct {
	Kind      ResolvedOutputKind
	Branch    string
	From      string
	NewBranch string
}

// NoMatchError matches Rust ResolveError::NoMatch.
type NoMatchError struct {
	BuildBranch string
	Tried       []string
	Available   []string
}

func (e *NoMatchError) Error() string {
	return fmt.Sprintf("no matching branch for build='%s'; tried [%s] against dataset branches [%s]", e.BuildBranch, strings.Join(e.Tried, ", "), strings.Join(e.Available, ", "))
}

// IncompatibleAncestryError matches Rust ResolveError::IncompatibleAncestry.
type IncompatibleAncestryError struct {
	DatasetRID  string
	BuildBranch string
	TargetChain []string
	Ancestry    []string
}

func (e *IncompatibleAncestryError) Error() string {
	return fmt.Sprintf("incompatible ancestry on dataset='%s': build='%s' chain=[%s] ancestry=[%s]", e.DatasetRID, e.BuildBranch, strings.Join(e.TargetChain, ", "), strings.Join(e.Ancestry, ", "))
}

// ResolveInputDataset implements Foundry cross-branch read resolution.
func ResolveInputDataset(buildBranch string, fallbackChain, datasetBranches []string) (ResolvedInput, error) {
	if contains(datasetBranches, buildBranch) {
		return ResolvedInput{Branch: buildBranch, FallbackIndex: 0}, nil
	}
	for i, candidate := range fallbackChain {
		if contains(datasetBranches, candidate) {
			return ResolvedInput{Branch: candidate, FallbackIndex: i + 1}, nil
		}
	}
	return ResolvedInput{}, &NoMatchError{BuildBranch: buildBranch, Tried: cloneStrings(fallbackChain), Available: cloneStrings(datasetBranches)}
}

// AssertChainAncestryCompatible enforces Rust branch_resolution ancestry rules.
func AssertChainAncestryCompatible(datasetRID, buildBranch string, fallbackChain, datasetAncestry []string) error {
	if len(datasetAncestry) == 0 {
		return nil
	}
	cursor := 0
	for _, candidate := range fallbackChain {
		if offset := indexOf(datasetAncestry[cursor:], candidate); offset >= 0 {
			cursor += offset + 1
		} else if contains(datasetAncestry, candidate) {
			return &IncompatibleAncestryError{DatasetRID: datasetRID, BuildBranch: buildBranch, TargetChain: cloneStrings(fallbackChain), Ancestry: cloneStrings(datasetAncestry)}
		}
	}
	return nil
}

// ResolveOutputDataset implements Foundry cross-branch write resolution.
func ResolveOutputDataset(buildBranch string, fallbackChain, datasetBranches []string) (ResolvedOutput, error) {
	if contains(datasetBranches, buildBranch) {
		return ResolvedOutput{Kind: ResolvedOutputExisting, Branch: buildBranch}, nil
	}
	for _, candidate := range fallbackChain {
		if contains(datasetBranches, candidate) {
			return ResolvedOutput{Kind: ResolvedOutputCreateFrom, NewBranch: buildBranch, From: candidate}, nil
		}
	}
	return ResolvedOutput{}, &NoMatchError{BuildBranch: buildBranch, Tried: cloneStrings(fallbackChain), Available: cloneStrings(datasetBranches)}
}

func contains(items []string, needle string) bool { return indexOf(items, needle) >= 0 }

func indexOf(items []string, needle string) int {
	for i, item := range items {
		if item == needle {
			return i
		}
	}
	return -1
}

func cloneStrings(items []string) []string {
	if items == nil {
		return nil
	}
	out := make([]string, len(items))
	copy(out, items)
	return out
}
