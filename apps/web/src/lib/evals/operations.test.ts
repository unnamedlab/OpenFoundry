import { describe, expect, it } from 'vitest';

import type { EvaluationEvaluator, EvaluationSuiteColumn, EvaluationTargetFunction, EvaluationTestCase } from '@/lib/api/evals';
import { addLogicFileToBranch, createLogicSavedVersion, type LogicVersionDefinition } from '@/lib/logic/blocks';

import {
  AIP_EVALS_API_SURFACE,
  appendAipAuditEvents,
  auditEventsForEvaluationRun,
  compareBranchEvaluationToMain,
  createSuiteRequestFromSdk,
  filterAipAuditEvents,
  installReusableEvaluatorPackage,
  packageReusableEvaluators,
  planBranchAwareEvaluationRun,
  planEvaluationCost,
  planExperimentCost,
  runBudgetedEvaluationSuite,
  runCodeFunctionReleaseEvalCheck,
  sdkRunRegressionCheck,
} from './operations';
import { runEvaluationSuiteBuiltIns } from './builtins';

const target: EvaluationTargetFunction = {
  id: 'logic.customer-triage',
  kind: 'logic',
  functionRid: 'logic.customer-triage',
  version: 'published',
  signature: { inputs: [{ apiName: 'question', type: 'string' }], outputs: [{ apiName: 'answer', type: 'string' }] },
};

const codeTarget: EvaluationTargetFunction = {
  id: 'fn.customer-triage.ts',
  kind: 'code_function',
  functionRid: 'fn.customer-triage.ts',
  version: 'published',
  signature: target.signature,
};

const columns: EvaluationSuiteColumn[] = [
  { id: 'question', name: 'Question', apiName: 'question', type: 'string', role: 'input' },
  { id: 'expected', name: 'Expected', apiName: 'expected', type: 'string', role: 'expected_output' },
];

const testCases: EvaluationTestCase[] = [
  { id: 'case-1', name: 'Escalation', values: { question: 'What next?', expected: 'Escalate account recovery' } },
  { id: 'case-2', name: 'Recovery', values: { question: 'What now?', expected: 'Escalate account recovery' } },
];

const evaluators: EvaluationEvaluator[] = [
  { id: 'exact', kind: 'built_in', evaluator: 'exact_match', targetId: target.id, mappings: { actual: 'answer', expected: 'expected' } },
];

const suite = {
  id: 'suite-customer-triage',
  projectId: 'customer-ops',
  targetFunctions: [target, codeTarget],
  testCaseColumns: columns,
  testCases,
  evaluators,
};

function branchResource() {
  const definition: LogicVersionDefinition = { inputs: [], blocks: [], outputs: [] };
  const mainVersion = { ...createLogicSavedVersion(definition, definition, 'Casey', new Date('2026-05-13T00:00:00.000Z'), 1), status: 'published' as const };
  return addLogicFileToBranch({ branchId: 'branch-candidate', branchName: 'Candidate', logicFileId: 'logic.customer-triage', mainVersion, actor: 'Casey', now: new Date('2026-05-13T01:00:00.000Z') });
}

describe('AIP audit event stream', () => {
  it('appends immutable hash-chained audit events and filters by supported dimensions', () => {
    const stream = appendAipAuditEvents([], [
      { kind: 'logic.created', actorId: 'casey', projectId: 'customer-ops', logicFileId: 'logic.customer-triage', summary: 'Created Logic file.', atIso: '2026-05-13T10:00:00.000Z' },
      { kind: 'eval.run_completed', actorId: 'casey', projectId: 'customer-ops', suiteId: suite.id, targetId: target.id, runId: 'run-1', modelId: 'gpt-4.1-mini', summary: 'Run completed.', atIso: '2026-05-13T10:05:00.000Z' },
      { kind: 'logic.tool_resource_exposed', actorId: 'morgan', projectId: 'other', objectTypeId: 'Shipment', actionTypeId: 'create-case', logicFileId: 'logic.other', summary: 'Tool exposed resource.', atIso: '2026-05-14T10:00:00.000Z' },
    ]);

    expect(stream[0].immutable).toBe(true);
    expect(stream[1].previousHash).toBe(stream[0].hash);
    expect(Object.isFrozen(stream[0])).toBe(true);
    expect(filterAipAuditEvents(stream, { suiteId: suite.id, targetId: target.id, userId: 'casey', projectId: 'customer-ops', modelId: 'gpt-4.1-mini', runId: 'run-1', startIso: '2026-05-13T00:00:00.000Z', endIso: '2026-05-13T23:59:59.000Z' })).toHaveLength(1);
    expect(filterAipAuditEvents(stream, { objectTypeId: 'Shipment', actionTypeId: 'create-case' })[0].actorId).toBe('morgan');
  });

  it('emits run start, target, completion, and dataset write audit inputs for evaluation runs', () => {
    const run = runEvaluationSuiteBuiltIns(suite, { targetIds: [target.id], suiteProjectId: 'customer-ops', resultsDatasetRid: 'ri.dataset.results' });
    const events = auditEventsForEvaluationRun(suite, run, 'casey');

    expect(events.map((event) => event.kind)).toEqual(expect.arrayContaining(['eval.run_started', 'eval.target_changed', 'eval.run_completed', 'eval.result_dataset_written']));
    expect(events.find((event) => event.kind === 'eval.result_dataset_written')?.resourceId).toBe('ri.dataset.results');
  });
});

describe('Branch-aware Evals and result isolation', () => {
  it('plans branch-scoped target versions, dependencies, and isolated datasets', () => {
    const plan = planBranchAwareEvaluationRun(suite, branchResource(), { iterations: 2 });

    expect(plan.branchName).toBe('Candidate');
    expect(plan.isolatedResultsDatasetRid).toContain('branch-candidate');
    expect(plan.isolatedRunHistoryDatasetRid).toContain('branch-candidate');
    expect(plan.runConfig.metadata?.branchName).toBe('Candidate');
    expect(plan.publishOrExportRequired).toBe(true);
    expect(plan.dependencyScope.some((dependency) => dependency.id === target.functionRid)).toBe(true);
  });

  it('compares a branch run to a main baseline before merge', () => {
    const mainRun = runEvaluationSuiteBuiltIns(suite, { targetIds: [target.id] });
    const branchRun = runEvaluationSuiteBuiltIns(suite, { targetIds: [target.id], metadata: { branchName: 'Candidate' } });
    const comparison = compareBranchEvaluationToMain(suite, branchRun, mainRun);

    expect(comparison.baseRunId).toBe(mainRun.id);
    expect(comparison.headRunId).toBe(branchRun.id);
    expect(comparison.summary.stillPassed + comparison.summary.stillFailed + comparison.summary.newlyPassed + comparison.summary.newlyFailed).toBeGreaterThanOrEqual(0);
  });
});

describe('AIP Evals API and SDK helpers', () => {
  it('declares CRUD/run/experiment/analyzer API surface and builds SDK suite requests', () => {
    expect(AIP_EVALS_API_SURFACE.suites).toContain('create');
    expect(AIP_EVALS_API_SURFACE.runs).toContain('execute');
    expect(AIP_EVALS_API_SURFACE.experiments).toContain('plan');
    expect(AIP_EVALS_API_SURFACE.analyzerJobs).toContain('create');
    expect(createSuiteRequestFromSdk({ name: 'SDK suite', project_id: 'p', folder_id: 'f' }).source_surface).toBe('api');
  });

  it('runs regression checks and compares against a baseline run', () => {
    const baselineRun = runEvaluationSuiteBuiltIns(suite, { targetIds: [target.id] });
    const regression = sdkRunRegressionCheck(suite, { targetIds: [target.id] }, { baselineRun, minPassRate: 0 });

    expect(regression.passed).toBe(true);
    expect(regression.comparison?.baseRunId).toBe(baselineRun.id);
  });
});

describe('Marketplace evaluator packages and code-function release checks', () => {
  it('packages reusable evaluators and installs them with target remapping', () => {
    const pkg = packageReusableEvaluators({ slug: 'triage-evals', name: 'Triage Evals', version: '1.0.0', evaluatorSlugs: ['rubric-grader'], targetPlaceholder: '{{target_function}}', testCaseTemplates: testCases });
    const install = installReusableEvaluatorPackage(pkg, { '{{target_function}}': target.id });

    expect(pkg.evaluatorFunctions[0].slug).toBe('rubric-grader');
    expect(install.installedEvaluators[0].target_id).toBe(target.id);
    expect(install.missingRemappings).toEqual([]);
    expect(install.setupSteps.length).toBeGreaterThan(0);
  });

  it('stores release evidence for code-authored function evaluations across mixed targets', () => {
    const check = runCodeFunctionReleaseEvalCheck({ suite, codeFunctionTargetId: codeTarget.id, packageRid: 'pkg.customer-triage', functionRid: 'fn.customer-triage.ts', version: 'v2', minPassRate: 0 });

    expect(check.comparedTargetIds).toContain(target.id);
    expect(check.releaseEvidence.functionRid).toBe('fn.customer-triage.ts');
    expect(check.releaseEvidence.version).toBe('v2');
    expect(check.publishBlockedReason).toBeUndefined();
  });
});

describe('Cost-aware evaluation and experiment planning', () => {
  it('estimates invocations, compute, cost, and enforces budget/confirmation gates', () => {
    const plan = planEvaluationCost(suite, { iterations: 3 }, { maxRuns: 100, requireConfirmationAboveUsd: 0 });
    const blocked = runBudgetedEvaluationSuite(suite, { iterations: 3 }, { requireConfirmationAboveUsd: 0 });

    expect(plan.runCount).toBe(12);
    expect(plan.targetInvocationCount).toBe(12);
    expect(plan.evaluatorInvocationCount).toBe(12);
    expect(plan.estimatedComputeSeconds).toBeGreaterThan(0);
    expect(plan.confirmationRequired).toBe(true);
    expect(blocked.blockedReason).toBe('High-cost evaluation requires confirmation.');
  });

  it('estimates experiment grid cost and budget overruns before execution', () => {
    const cost = planExperimentCost(suite, {
      dimensions: [{ id: 'model', kind: 'target_model', label: 'Model', targetId: target.id, values: ['a', 'b', 'c'] }],
      baseConfig: { iterations: 2 },
    }, { maxRuns: 5 });

    expect(cost.experimentPlan.totalCombinations).toBe(3);
    expect(cost.runCount).toBe(24);
    expect(cost.overBudget).toBe(true);
    expect(cost.warnings.some((warning) => warning.includes('budget'))).toBe(true);
  });
});
