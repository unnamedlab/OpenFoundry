package resolver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/openfoundry/openfoundry-go/services/pipeline-build-service/internal/models"
)

func TestResolveBuildPipelineSimple(t *testing.T) {
	ctx := context.Background()
	specs := newMockJobSpecRepo()
	versioning := newMockDatasetRepo()
	locks := newMockLockRepo()
	specs.add(jobSpec("ri.spec.s1", []string{"raw.a"}, []string{"out.b"}))
	versioning.addBranch("raw.a", "master")

	resolved, err := ResolveBuild(ctx, ResolveBuildArgs{
		PipelineRID:       "ri.foundry.main.pipeline.simple",
		BuildBranch:       "master",
		OutputDatasetRIDs: []string{"out.b"},
	}, specs, versioning, locks)
	require.NoError(t, err)
	require.Equal(t, models.BuildResolution, resolved.State)
	require.Len(t, resolved.JobSpecs, 1)
	require.Len(t, resolved.InputViews, 1)
	require.Equal(t, "raw.a", resolved.InputViews[0].DatasetRID)
	require.Equal(t, "master", resolved.InputViews[0].Branch)
	require.Len(t, resolved.OpenedTransactions, 1)
	require.Equal(t, "out.b", resolved.OpenedTransactions[0].DatasetRID)
	require.Equal(t, [][]string{{"ri.spec.s1"}}, resolved.FanOutStages)
	require.Len(t, resolved.Jobs, 1)
	require.Equal(t, []string{resolved.OpenedTransactions[0].TransactionRID}, resolved.Jobs[0].OutputTransactionRIDs)
}

func TestResolveBuildDependencies(t *testing.T) {
	ctx := context.Background()
	specs := newMockJobSpecRepo()
	versioning := newMockDatasetRepo()
	locks := newMockLockRepo()
	specs.add(jobSpec("ri.spec.upstream", []string{"raw.a"}, []string{"mid.b"}))
	specs.add(jobSpec("ri.spec.downstream", []string{"mid.b"}, []string{"out.c"}))
	versioning.addBranch("raw.a", "master")

	resolved, err := ResolveBuild(ctx, ResolveBuildArgs{
		PipelineRID:       "ri.foundry.main.pipeline.deps",
		BuildBranch:       "master",
		OutputDatasetRIDs: []string{"out.c", "mid.b"},
	}, specs, versioning, locks)
	require.NoError(t, err)
	require.Len(t, resolved.InputViews, 1, "internal input mid.b is produced by the same build and should not be versioning-validated")
	require.Equal(t, [][]string{{"ri.spec.upstream"}, {"ri.spec.downstream"}}, resolved.FanOutStages)

	jobsBySpec := map[string]models.ResolvedJob{}
	for _, job := range resolved.Jobs {
		jobsBySpec[job.JobSpecRID] = job
	}
	require.Equal(t, []string{"ri.spec.upstream"}, jobsBySpec["ri.spec.downstream"].DependsOnJobSpecRIDs)
	require.Empty(t, jobsBySpec["ri.spec.upstream"].DependsOnJobSpecRIDs)
}

func TestAcquireLocksSuccessAndFailure(t *testing.T) {
	ctx := context.Background()
	spec := jobSpec("ri.spec.s1", nil, []string{"out.locked"})
	versioning := newMockDatasetRepo()
	locks := newMockLockRepo()
	buildID := uuid.New()

	opened, err := AcquireLocks(ctx, buildID, []models.JobSpec{spec}, "master", versioning, locks)
	require.NoError(t, err)
	require.Len(t, opened, 1)
	require.Equal(t, "out.locked", opened[0].DatasetRID)
	require.Equal(t, buildID, locks.holder("out.locked"))

	secondBuild := uuid.New()
	_, err = AcquireLocks(ctx, secondBuild, []models.JobSpec{spec}, "master", versioning, locks)
	require.Error(t, err)
	var held *LockHeldError
	require.True(t, errors.As(err, &held), "got %T: %v", err, err)
	require.Equal(t, "out.locked", held.DatasetRID)
	require.Equal(t, buildID, held.HolderBuildID)
}

func TestResolveBuildFanOut(t *testing.T) {
	ctx := context.Background()
	specs := newMockJobSpecRepo()
	versioning := newMockDatasetRepo()
	locks := newMockLockRepo()
	for _, letter := range []string{"a", "b", "c"} {
		input := "raw." + letter
		output := "out." + letter
		specs.add(jobSpec("ri.spec."+letter, []string{input}, []string{output}))
		versioning.addBranch(input, "master")
	}

	resolved, err := ResolveBuild(ctx, ResolveBuildArgs{
		PipelineRID:       "ri.foundry.main.pipeline.parallel",
		BuildBranch:       "master",
		OutputDatasetRIDs: []string{"out.c", "out.a", "out.b"},
	}, specs, versioning, locks)
	require.NoError(t, err)
	require.Equal(t, [][]string{{"ri.spec.a", "ri.spec.b", "ri.spec.c"}}, resolved.FanOutStages)
	require.Len(t, resolved.OpenedTransactions, 3)
	require.Equal(t, []string{"out.a", "out.b", "out.c"}, []string{resolved.OpenedTransactions[0].DatasetRID, resolved.OpenedTransactions[1].DatasetRID, resolved.OpenedTransactions[2].DatasetRID})
}

func TestResolveBuildInvalidGraphDetectsCycle(t *testing.T) {
	ctx := context.Background()
	specs := newMockJobSpecRepo()
	versioning := newMockDatasetRepo()
	locks := newMockLockRepo()
	specs.add(jobSpec("ri.spec.s1", []string{"a"}, []string{"b"}))
	specs.add(jobSpec("ri.spec.s2", []string{"b"}, []string{"a"}))
	versioning.addBranch("a", "master")
	versioning.addBranch("b", "master")

	_, err := ResolveBuild(ctx, ResolveBuildArgs{
		PipelineRID:       "ri.foundry.main.pipeline.cyclic",
		BuildBranch:       "master",
		OutputDatasetRIDs: []string{"a", "b"},
	}, specs, versioning, locks)
	require.Error(t, err)
	var cycle *CycleDetectedError
	require.True(t, errors.As(err, &cycle), "got %T: %v", err, err)
	require.NotEmpty(t, cycle.CyclePath)
}

func TestResolveBuildMissingJobSpecListsTriedBranches(t *testing.T) {
	ctx := context.Background()
	_, err := ResolveBuild(ctx, ResolveBuildArgs{
		PipelineRID:       "ri.foundry.main.pipeline.x",
		BuildBranch:       "feature",
		JobSpecFallback:   []string{"develop", "master"},
		OutputDatasetRIDs: []string{"ri.foundry.main.dataset.unknown"},
	}, newMockJobSpecRepo(), newMockDatasetRepo(), newMockLockRepo())
	require.Error(t, err)
	var missing *MissingJobSpecError
	require.True(t, errors.As(err, &missing), "got %T: %v", err, err)
	require.Equal(t, "ri.foundry.main.dataset.unknown", missing.DatasetRID)
	require.Equal(t, []string{"feature", "develop", "master"}, missing.Tried)
}

func TestBranchAncestryCompatibility(t *testing.T) {
	require.NoError(t, AssertChainAncestryCompatible("ri.foundry.main.dataset.1", "feature", []string{"develop", "master"}, []string{"feature", "develop", "master"}))
	err := AssertChainAncestryCompatible("ri.foundry.main.dataset.2", "feature", []string{"master", "develop"}, []string{"feature", "develop", "master"})
	require.Error(t, err)
	var incompatible *IncompatibleAncestryError
	require.True(t, errors.As(err, &incompatible))
	require.Equal(t, []string{"master", "develop"}, incompatible.TargetChain)
	require.NoError(t, AssertChainAncestryCompatible("ri.foundry.main.dataset.3", "feature", []string{"develop", "master"}, nil))
}

func jobSpec(rid string, inputs, outputs []string) models.JobSpec {
	inputSpecs := make([]models.InputSpec, 0, len(inputs))
	for _, input := range inputs {
		inputSpecs = append(inputSpecs, models.InputSpec{DatasetRID: input, FallbackChain: []string{"master"}})
	}
	return models.JobSpec{
		RID:               rid,
		PipelineRID:       "ri.foundry.main.pipeline.test",
		BranchName:        "master",
		Inputs:            inputSpecs,
		OutputDatasetRIDs: append([]string(nil), outputs...),
		LogicKind:         "TRANSFORM",
		LogicPayload:      json.RawMessage(`null`),
		ContentHash:       "hash-" + rid,
	}
}

type mockJobSpecRepo struct {
	mu    sync.Mutex
	specs map[string]models.JobSpec
}

func newMockJobSpecRepo() *mockJobSpecRepo {
	return &mockJobSpecRepo{specs: map[string]models.JobSpec{}}
}

func (m *mockJobSpecRepo) add(spec models.JobSpec) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, output := range spec.OutputDatasetRIDs {
		m.specs[output] = spec
	}
}

func (m *mockJobSpecRepo) Lookup(_ context.Context, _, outputDatasetRID, _ string, _ []string) (*models.JobSpec, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	spec, ok := m.specs[outputDatasetRID]
	if !ok {
		return nil, nil
	}
	return &spec, nil
}

type mockDatasetRepo struct {
	mu       sync.Mutex
	branches map[string][]models.BranchSnapshot
	schemas  map[string]json.RawMessage
	counter  int
}

func newMockDatasetRepo() *mockDatasetRepo {
	return &mockDatasetRepo{branches: map[string][]models.BranchSnapshot{}, schemas: map[string]json.RawMessage{}}
}

func (m *mockDatasetRepo) addBranch(datasetRID, branch string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.branches[datasetRID] = append(m.branches[datasetRID], models.BranchSnapshot{Name: branch})
}

func (m *mockDatasetRepo) ListBranches(_ context.Context, datasetRID string) ([]models.BranchSnapshot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]models.BranchSnapshot(nil), m.branches[datasetRID]...), nil
}

func (m *mockDatasetRepo) OpenTransaction(_ context.Context, datasetRID, _ string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.counter++
	return "ri.foundry.main.transaction." + datasetRID + "." + uuid.NewSHA1(uuid.NameSpaceOID, []byte(fmt.Sprintf("%s:%d", datasetRID, m.counter))).String(), nil
}

func (m *mockDatasetRepo) ViewSchema(_ context.Context, datasetRID, branch string) (json.RawMessage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if schema, ok := m.schemas[datasetRID+":"+branch]; ok {
		return schema, nil
	}
	return json.RawMessage(`{"fields":[]}`), nil
}

type mockLockRepo struct {
	mu    sync.Mutex
	locks map[string]uuid.UUID
}

func newMockLockRepo() *mockLockRepo { return &mockLockRepo{locks: map[string]uuid.UUID{}} }

func (m *mockLockRepo) TryAcquire(_ context.Context, outputDatasetRID string, buildID uuid.UUID, _ string) (uuid.UUID, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if holder, ok := m.locks[outputDatasetRID]; ok {
		return holder, false, nil
	}
	m.locks[outputDatasetRID] = buildID
	return uuid.Nil, true, nil
}

func (m *mockLockRepo) HasUpstreamInProgress(_ context.Context, _ []string, _ uuid.UUID) (bool, error) {
	return false, nil
}

func (m *mockLockRepo) holder(outputDatasetRID string) uuid.UUID {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.locks[outputDatasetRID]
}
