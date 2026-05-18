import type {
  CreateEvaluationSuiteRequest,
  EvaluationEvaluator,
  EvaluationSuite,
  EvaluationSuiteColumn,
  EvaluationTargetFunction,
  EvaluationTestCase,
} from '@/lib/api/evals';
import {
  buildEvaluationExperimentPlan,
  compareEvaluationRuns,
  estimateEvaluationRunComputeUsage,
  runEvaluationExperiment,
  runEvaluationSuiteBuiltIns,
  type BuiltInEvaluationRunConfig,
  type BuiltInEvaluationRunResult,
  type EvaluationExperimentConfig,
  type EvaluationExperimentPlan,
  type EvaluationExperimentResults,
  type EvaluationResultsAnalyzerConfig,
} from '@/lib/evals/builtins';
import {
  buildMarketplaceEvaluationFunction,
  marketplaceEvaluatorProductBySlug,
  marketplaceEvaluatorSetupPlan,
  type MarketplaceEvaluatorProduct,
  type MarketplaceEvaluatorSlug,
} from '@/lib/evals/marketplaceEvaluators';
import type { LogicBranchAdapterResource } from '@/lib/logic/blocks';

export type AipAuditEventKind =
  | 'logic.created'
  | 'logic.edited'
  | 'logic.published'
  | 'logic.deleted'
  | 'logic.execution_mode_changed'
  | 'logic.run_invoked'
  | 'logic.action_used'
  | 'logic.automation_used'
  | 'logic.tool_resource_exposed'
  | 'eval.suite_created'
  | 'eval.suite_updated'
  | 'eval.suite_deleted'
  | 'eval.target_changed'
  | 'eval.test_case_changed'
  | 'eval.evaluator_changed'
  | 'eval.run_started'
  | 'eval.run_completed'
  | 'eval.experiment_started'
  | 'eval.result_dataset_written';

export interface AipAuditEventInput {
  kind: AipAuditEventKind;
  actorId: string;
  actorName?: string;
  projectId?: string;
  logicFileId?: string;
  suiteId?: string;
  targetId?: string;
  runId?: string;
  modelId?: string;
  objectTypeId?: string;
  actionTypeId?: string;
  resourceId?: string;
  branchId?: string;
  branchName?: string;
  summary: string;
  details?: Record<string, unknown>;
  atIso?: string;
}

export interface AipAuditEvent extends AipAuditEventInput {
  id: string;
  atIso: string;
  immutable: true;
  previousHash?: string;
  hash: string;
}

export interface AipAuditEventFilter {
  logicFileId?: string;
  suiteId?: string;
  targetId?: string;
  userId?: string;
  projectId?: string;
  modelId?: string;
  objectTypeId?: string;
  actionTypeId?: string;
  runId?: string;
  branchId?: string;
  kind?: AipAuditEventKind | AipAuditEventKind[];
  startIso?: string;
  endIso?: string;
}

export interface EvaluationSuiteLike {
  id?: string;
  projectId?: string;
  project_id?: string;
  folderId?: string;
  folder_id?: string;
  name?: string;
  targetFunctions: EvaluationTargetFunction[];
  testCaseColumns?: EvaluationSuiteColumn[];
  testCases: EvaluationTestCase[];
  evaluators: EvaluationEvaluator[];
  resultsDatasetRid?: string;
}

function stableStringify(value: unknown): string {
  if (Array.isArray(value)) return `[${value.map(stableStringify).join(',')}]`;
  if (value && typeof value === 'object') {
    return `{${Object.entries(value as Record<string, unknown>).sort(([a], [b]) => a.localeCompare(b)).map(([key, entry]) => `${JSON.stringify(key)}:${stableStringify(entry)}`).join(',')}}`;
  }
  return JSON.stringify(value);
}

function hashString(value: string): string {
  let hash = 2166136261;
  for (let index = 0; index < value.length; index += 1) {
    hash ^= value.charCodeAt(index);
    hash = Math.imul(hash, 16777619);
  }
  return (hash >>> 0).toString(16).padStart(8, '0');
}

export function appendAipAuditEvent(stream: readonly AipAuditEvent[], input: AipAuditEventInput): readonly AipAuditEvent[] {
  const previousHash = stream[stream.length - 1]?.hash;
  const atIso = input.atIso ?? new Date().toISOString();
  const base = { ...input, atIso, previousHash };
  const hash = hashString(stableStringify(base));
  const event: AipAuditEvent = Object.freeze({
    ...base,
    id: `audit-${atIso.replace(/[^0-9]/g, '')}-${hash}`,
    immutable: true,
    hash,
  });
  return Object.freeze([...stream, event]);
}

export function appendAipAuditEvents(stream: readonly AipAuditEvent[], inputs: AipAuditEventInput[]): readonly AipAuditEvent[] {
  return inputs.reduce((next, input) => appendAipAuditEvent(next, input), stream);
}

export function filterAipAuditEvents(events: readonly AipAuditEvent[], filter: AipAuditEventFilter): AipAuditEvent[] {
  const kindSet = filter.kind ? new Set(Array.isArray(filter.kind) ? filter.kind : [filter.kind]) : undefined;
  const start = filter.startIso ? Date.parse(filter.startIso) : Number.NEGATIVE_INFINITY;
  const end = filter.endIso ? Date.parse(filter.endIso) : Number.POSITIVE_INFINITY;
  return events.filter((event) => {
    const at = Date.parse(event.atIso);
    return (!filter.logicFileId || event.logicFileId === filter.logicFileId)
      && (!filter.suiteId || event.suiteId === filter.suiteId)
      && (!filter.targetId || event.targetId === filter.targetId)
      && (!filter.userId || event.actorId === filter.userId)
      && (!filter.projectId || event.projectId === filter.projectId)
      && (!filter.modelId || event.modelId === filter.modelId)
      && (!filter.objectTypeId || event.objectTypeId === filter.objectTypeId)
      && (!filter.actionTypeId || event.actionTypeId === filter.actionTypeId)
      && (!filter.runId || event.runId === filter.runId)
      && (!filter.branchId || event.branchId === filter.branchId)
      && (!kindSet || kindSet.has(event.kind))
      && Number.isFinite(at) && at >= start && at <= end;
  });
}

export function auditEventsForEvaluationRun(suite: EvaluationSuiteLike, run: BuiltInEvaluationRunResult, actorId: string, projectId?: string): AipAuditEventInput[] {
  const suiteProjectId = projectId ?? suite.projectId ?? suite.project_id;
  const targetEvents = run.config.targetIds.map((targetId) => ({
    kind: 'eval.target_changed' as const,
    actorId,
    projectId: suiteProjectId,
    suiteId: suite.id,
    targetId,
    runId: run.id,
    modelId: run.config.targetModels[targetId],
    summary: `Evaluation run selected target ${targetId}.`,
    atIso: run.startedAtIso,
  }));
  const datasetEvent = run.resultsDatasetWrite ? [{
    kind: 'eval.result_dataset_written' as const,
    actorId,
    projectId: suiteProjectId,
    suiteId: suite.id,
    runId: run.id,
    resourceId: run.resultsDatasetWrite.config.datasetRid,
    summary: `Evaluation results wrote ${run.resultsDatasetWrite.rows.length} row(s).`,
    atIso: run.completedAtIso,
  }] : [];
  return [
    { kind: 'eval.run_started', actorId, projectId: suiteProjectId, suiteId: suite.id, runId: run.id, summary: `Evaluation run ${run.id} started.`, atIso: run.startedAtIso },
    ...targetEvents,
    { kind: 'eval.run_completed', actorId, projectId: suiteProjectId, suiteId: suite.id, runId: run.id, summary: `Evaluation run ${run.id} completed with pass rate ${Math.round(run.passRate * 100)}%.`, atIso: run.completedAtIso, details: { passRate: run.passRate, totalCount: run.totalCount } },
    ...datasetEvent,
  ];
}

function branchSafeId(value: string): string {
  return value.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-|-$/g, '') || 'branch';
}

export interface BranchAwareEvaluationRunPlan {
  branchId: string;
  branchName: string;
  branchFunctionRid?: string;
  isolatedResultsDatasetRid: string;
  isolatedRunHistoryDatasetRid: string;
  targetVersions: Record<string, string>;
  dependencyScope: Array<{ kind: 'ontology' | 'function' | 'action' | 'logic'; id: string; branchScoped: boolean }>;
  runConfig: BuiltInEvaluationRunConfig;
  publishOrExportRequired: boolean;
}

export function planBranchAwareEvaluationRun(suite: EvaluationSuiteLike, branch: LogicBranchAdapterResource, config: BuiltInEvaluationRunConfig = {}): BranchAwareEvaluationRunPlan {
  const branchKey = branchSafeId(branch.branchId);
  const branchFunctionRid = branch.publication?.functionRid;
  const targetVersions = Object.fromEntries(suite.targetFunctions.map((target) => [
    target.id,
    target.kind === 'logic' && branchFunctionRid ? branch.publication?.versionId ?? branch.branchVersion.id : config.targetVersions?.[target.id] ?? target.version,
  ]));
  const dependencyIds = new Set<string>();
  suite.targetFunctions.forEach((target) => dependencyIds.add(String(target.functionRid ?? target.function_rid ?? target.id)));
  suite.evaluators.forEach((evaluator) => dependencyIds.add(String(evaluator.functionRid ?? evaluator.function_rid ?? evaluator.evaluator)));
  return {
    branchId: branch.branchId,
    branchName: branch.branchName,
    branchFunctionRid,
    isolatedResultsDatasetRid: `ri.openfoundry.dataset.aip-evals.${branchKey}.${suite.id ?? 'suite'}.results`,
    isolatedRunHistoryDatasetRid: `ri.openfoundry.dataset.logic-run-history.${branchKey}.${branchSafeId(branch.logicFileId)}`,
    targetVersions,
    dependencyScope: Array.from(dependencyIds).sort().map((id) => ({
      kind: id.startsWith('logic.') ? 'logic' : id.startsWith('fn.') ? 'function' : id.includes('action') ? 'action' : 'ontology',
      id,
      branchScoped: id === branchFunctionRid || id === branch.logicFileId || id.endsWith(`@${branchKey}`),
    })),
    runConfig: {
      ...config,
      targetVersions,
      resultsDatasetRid: `ri.openfoundry.dataset.aip-evals.${branchKey}.${suite.id ?? 'suite'}.results`,
      metadata: { ...(config.metadata ?? {}), branchName: branch.branchName, customMetadata: { ...(config.metadata?.customMetadata ?? {}), isolatedBranchRun: true } },
    },
    publishOrExportRequired: true,
  };
}

export function runBranchAwareEvaluationSuite(suite: EvaluationSuiteLike, branch: LogicBranchAdapterResource, config: BuiltInEvaluationRunConfig = {}) {
  const plan = planBranchAwareEvaluationRun(suite, branch, config);
  const run = runEvaluationSuiteBuiltIns(suite, plan.runConfig);
  return { plan, run };
}

export function compareBranchEvaluationToMain(suite: EvaluationSuiteLike, branchRun: BuiltInEvaluationRunResult, mainRun: BuiltInEvaluationRunResult) {
  return compareEvaluationRuns(suite, mainRun, branchRun);
}

export interface EvalsSdkRegressionResult {
  run: BuiltInEvaluationRunResult;
  passed: boolean;
  passRate: number;
  minPassRate: number;
  comparison?: ReturnType<typeof compareEvaluationRuns>;
}

export const AIP_EVALS_API_SURFACE = Object.freeze({
  suites: ['list', 'create', 'get', 'update', 'move', 'duplicate', 'archive', 'restore'],
  targets: ['add', 'remove', 'select-version', 'map-inputs'],
  testCases: ['create', 'update', 'delete', 'object-set-backfill'],
  evaluators: ['built-in', 'custom-function', 'marketplace-function', 'objective-config'],
  runs: ['configure', 'execute', 'retrieve-results', 'write-results-dataset'],
  experiments: ['plan', 'execute', 'compare'],
  analyzerJobs: ['create', 'retrieve'],
});

export function createSuiteRequestFromSdk(input: Omit<CreateEvaluationSuiteRequest, 'source_surface'> & { source_surface?: CreateEvaluationSuiteRequest['source_surface'] }): CreateEvaluationSuiteRequest {
  return { ...input, source_surface: input.source_surface ?? 'api' };
}

export function sdkRunRegressionCheck(
  suite: EvaluationSuiteLike,
  config: BuiltInEvaluationRunConfig = {},
  options: { minPassRate?: number; baselineRun?: BuiltInEvaluationRunResult } = {},
): EvalsSdkRegressionResult {
  const run = runEvaluationSuiteBuiltIns(suite, { ...config, source: config.source ?? 'api' });
  const minPassRate = options.minPassRate ?? 1;
  return {
    run,
    passed: run.passRate >= minPassRate,
    passRate: run.passRate,
    minPassRate,
    comparison: options.baselineRun ? compareEvaluationRuns(suite, options.baselineRun, run) : undefined,
  };
}

export function sdkCompareMetrics(suite: EvaluationSuiteLike, baseRun: BuiltInEvaluationRunResult, headRun: BuiltInEvaluationRunResult) {
  return compareEvaluationRuns(suite, baseRun, headRun);
}

export interface ReusableEvaluatorPackage {
  slug: string;
  name: string;
  version: string;
  evaluatorFunctions: MarketplaceEvaluatorProduct[];
  testCaseTemplates: EvaluationTestCase[];
  logicFileTemplates: Array<{ logicFileId: string; name: string; targetPlaceholder: string }>;
  exampleSuites: Array<Pick<EvaluationSuite, 'id' | 'name' | 'target_functions' | 'test_case_columns' | 'test_cases' | 'evaluators'>>;
  dependencyPlaceholders: Record<string, string>;
}

export interface ReusableEvaluatorInstallPlan {
  packageSlug: string;
  installedEvaluators: EvaluationEvaluator[];
  setupSteps: string[];
  remappedPlaceholders: Record<string, string>;
  missingRemappings: string[];
}

export function packageReusableEvaluators(input: {
  slug: string;
  name: string;
  version: string;
  evaluatorSlugs: MarketplaceEvaluatorSlug[];
  targetPlaceholder: string;
  testCaseTemplates?: EvaluationTestCase[];
  logicFileTemplates?: Array<{ logicFileId: string; name: string; targetPlaceholder: string }>;
  exampleSuite?: Pick<EvaluationSuite, 'id' | 'name' | 'target_functions' | 'test_case_columns' | 'test_cases' | 'evaluators'>;
}): ReusableEvaluatorPackage {
  const evaluatorFunctions = input.evaluatorSlugs.map((slug) => marketplaceEvaluatorProductBySlug(slug)).filter((product): product is MarketplaceEvaluatorProduct => Boolean(product));
  return {
    slug: input.slug,
    name: input.name,
    version: input.version,
    evaluatorFunctions,
    testCaseTemplates: input.testCaseTemplates ?? [],
    logicFileTemplates: input.logicFileTemplates ?? [],
    exampleSuites: input.exampleSuite ? [input.exampleSuite] : [],
    dependencyPlaceholders: Object.fromEntries(evaluatorFunctions.map((product) => [product.functionRid, input.targetPlaceholder])),
  };
}

export function installReusableEvaluatorPackage(pkg: ReusableEvaluatorPackage, remappings: Record<string, string>): ReusableEvaluatorInstallPlan {
  const missingRemappings = Object.values(pkg.dependencyPlaceholders).filter((placeholder, index, values) => values.indexOf(placeholder) === index && !remappings[placeholder]);
  const targetId = remappings[pkg.logicFileTemplates[0]?.targetPlaceholder ?? Object.values(pkg.dependencyPlaceholders)[0] ?? 'target'] ?? remappings.target ?? 'target';
  const installedEvaluators = pkg.evaluatorFunctions.map((product) => buildMarketplaceEvaluationFunction(product, {
    [targetId]: { actual: 'actual', expected: 'expected' },
  }));
  return {
    packageSlug: pkg.slug,
    installedEvaluators,
    setupSteps: pkg.evaluatorFunctions.flatMap((product) => marketplaceEvaluatorSetupPlan(product).steps),
    remappedPlaceholders: remappings,
    missingRemappings,
  };
}

export interface CodeFunctionReleaseEvalCheck {
  suiteId?: string;
  codeFunctionTargetId: string;
  comparedTargetIds: string[];
  run: BuiltInEvaluationRunResult;
  comparison?: ReturnType<typeof compareEvaluationRuns>;
  releaseEvidence: {
    id: string;
    packageRid: string;
    functionRid: string;
    version: string;
    resultDatasetRid?: string;
    passed: boolean;
    passRate: number;
    createdAtIso: string;
  };
  publishBlockedReason?: string;
}

export function runCodeFunctionReleaseEvalCheck(input: {
  suite: EvaluationSuiteLike;
  codeFunctionTargetId: string;
  packageRid: string;
  functionRid: string;
  version: string;
  minPassRate?: number;
  baselineRun?: BuiltInEvaluationRunResult;
  config?: BuiltInEvaluationRunConfig;
}): CodeFunctionReleaseEvalCheck {
  const comparedTargetIds = input.suite.targetFunctions.filter((target) => target.id !== input.codeFunctionTargetId).map((target) => target.id);
  const run = runEvaluationSuiteBuiltIns(input.suite, {
    ...(input.config ?? {}),
    targetIds: [input.codeFunctionTargetId, ...comparedTargetIds],
    targetVersions: { ...(input.config?.targetVersions ?? {}), [input.codeFunctionTargetId]: input.version },
    source: 'code_function_published',
  });
  const minPassRate = input.minPassRate ?? 1;
  const passed = run.passRate >= minPassRate;
  return {
    suiteId: input.suite.id,
    codeFunctionTargetId: input.codeFunctionTargetId,
    comparedTargetIds,
    run,
    comparison: input.baselineRun ? compareEvaluationRuns(input.suite, input.baselineRun, run) : undefined,
    releaseEvidence: {
      id: `release-evidence-${input.packageRid}-${input.version}`.replace(/[^A-Za-z0-9_.-]+/g, '-'),
      packageRid: input.packageRid,
      functionRid: input.functionRid,
      version: input.version,
      resultDatasetRid: run.resultsDatasetWrite?.config.datasetRid,
      passed,
      passRate: run.passRate,
      createdAtIso: run.completedAtIso,
    },
    publishBlockedReason: passed ? undefined : `Pass rate ${Math.round(run.passRate * 100)}% is below required ${Math.round(minPassRate * 100)}%.`,
  };
}

export interface EvaluationCostBudgetPolicy {
  projectId?: string;
  userId?: string;
  maxRuns?: number;
  maxComputeSeconds?: number;
  maxEstimatedCostUsd?: number;
  requireConfirmationAboveUsd?: number;
}

export interface EvaluationCostPlan {
  runCount: number;
  targetInvocationCount: number;
  evaluatorInvocationCount: number;
  blockExecutionCount: number;
  llmToolUsageCount: number;
  estimatedComputeSeconds: number;
  estimatedCostUsd: number;
  overBudget: boolean;
  confirmationRequired: boolean;
  warnings: string[];
}

export function planEvaluationCost(
  suite: EvaluationSuiteLike,
  config: BuiltInEvaluationRunConfig = {},
  policy: EvaluationCostBudgetPolicy = {},
): EvaluationCostPlan {
  const targetCount = Math.max(1, config.targetIds?.length ?? suite.targetFunctions.length);
  const testCaseCount = Math.max(1, config.testCaseIds?.length ?? suite.testCases.length);
  const iterations = Math.max(1, Math.floor(config.iterations ?? 1));
  const runCount = targetCount * testCaseCount * iterations;
  const evaluatorInvocationCount = runCount * Math.max(1, suite.evaluators.length);
  const usage = estimateEvaluationRunComputeUsage(suite, config);
  const estimatedComputeSeconds = usage.totalComputeSeconds;
  const estimatedCostUsd = Math.round((estimatedComputeSeconds / 3600) * 0.42 * 100) / 100;
  const warnings: string[] = [];
  if (policy.maxRuns !== undefined && runCount > policy.maxRuns) warnings.push(`Run count ${runCount} exceeds budget ${policy.maxRuns}.`);
  if (policy.maxComputeSeconds !== undefined && estimatedComputeSeconds > policy.maxComputeSeconds) warnings.push(`Compute ${estimatedComputeSeconds}s exceeds budget ${policy.maxComputeSeconds}s.`);
  if (policy.maxEstimatedCostUsd !== undefined && estimatedCostUsd > policy.maxEstimatedCostUsd) warnings.push(`Estimated cost $${estimatedCostUsd} exceeds budget $${policy.maxEstimatedCostUsd}.`);
  return {
    runCount,
    targetInvocationCount: runCount,
    evaluatorInvocationCount,
    blockExecutionCount: runCount,
    llmToolUsageCount: usage.llmToolComputeSeconds > 0 ? runCount : 0,
    estimatedComputeSeconds,
    estimatedCostUsd,
    overBudget: warnings.length > 0,
    confirmationRequired: policy.requireConfirmationAboveUsd !== undefined && estimatedCostUsd >= policy.requireConfirmationAboveUsd,
    warnings,
  };
}

export function planExperimentCost(
  suite: EvaluationSuiteLike,
  experimentConfig: EvaluationExperimentConfig,
  policy: EvaluationCostBudgetPolicy = {},
): EvaluationCostPlan & { experimentPlan: EvaluationExperimentPlan } {
  const experimentPlan = buildEvaluationExperimentPlan(suite, experimentConfig);
  const basePlan = planEvaluationCost(suite, experimentConfig.baseConfig, policy);
  const runCount = basePlan.runCount * experimentPlan.executedCombinations;
  const evaluatorInvocationCount = basePlan.evaluatorInvocationCount * experimentPlan.executedCombinations;
  const estimatedComputeSeconds = experimentPlan.estimatedComputeSeconds;
  const estimatedCostUsd = Math.round((estimatedComputeSeconds / 3600) * 0.42 * 100) / 100;
  const warnings = [...basePlan.warnings, ...experimentPlan.warnings.map((warning) => warning.message)];
  if (policy.maxRuns !== undefined && runCount > policy.maxRuns) warnings.push(`Experiment run count ${runCount} exceeds budget ${policy.maxRuns}.`);
  if (policy.maxComputeSeconds !== undefined && estimatedComputeSeconds > policy.maxComputeSeconds) warnings.push(`Experiment compute ${estimatedComputeSeconds}s exceeds budget ${policy.maxComputeSeconds}s.`);
  if (policy.maxEstimatedCostUsd !== undefined && estimatedCostUsd > policy.maxEstimatedCostUsd) warnings.push(`Experiment estimated cost $${estimatedCostUsd} exceeds budget $${policy.maxEstimatedCostUsd}.`);
  return {
    ...basePlan,
    experimentPlan,
    runCount,
    targetInvocationCount: runCount,
    evaluatorInvocationCount,
    blockExecutionCount: runCount,
    llmToolUsageCount: basePlan.llmToolUsageCount * experimentPlan.executedCombinations,
    estimatedComputeSeconds,
    estimatedCostUsd,
    overBudget: warnings.length > 0,
    confirmationRequired: policy.requireConfirmationAboveUsd !== undefined && estimatedCostUsd >= policy.requireConfirmationAboveUsd,
    warnings,
  };
}

export function runBudgetedEvaluationSuite(
  suite: EvaluationSuiteLike,
  config: BuiltInEvaluationRunConfig,
  policy: EvaluationCostBudgetPolicy,
  options: { confirmed?: boolean } = {},
): { costPlan: EvaluationCostPlan; run?: BuiltInEvaluationRunResult; blockedReason?: string } {
  const costPlan = planEvaluationCost(suite, config, policy);
  if (costPlan.overBudget) return { costPlan, blockedReason: costPlan.warnings[0] };
  if (costPlan.confirmationRequired && !options.confirmed) return { costPlan, blockedReason: 'High-cost evaluation requires confirmation.' };
  return { costPlan, run: runEvaluationSuiteBuiltIns(suite, config) };
}

export function runBudgetedExperiment(
  suite: EvaluationSuiteLike,
  config: EvaluationExperimentConfig,
  policy: EvaluationCostBudgetPolicy,
  options: { confirmed?: boolean } = {},
): { costPlan: ReturnType<typeof planExperimentCost>; results?: EvaluationExperimentResults; blockedReason?: string } {
  const costPlan = planExperimentCost(suite, config, policy);
  if (costPlan.overBudget) return { costPlan, blockedReason: costPlan.warnings[0] };
  if (costPlan.confirmationRequired && !options.confirmed) return { costPlan, blockedReason: 'High-cost experiment grid requires confirmation.' };
  return { costPlan, results: runEvaluationExperiment(suite, config) };
}

export interface AnalyzerJobRequest {
  id: string;
  suiteId?: string;
  config: EvaluationResultsAnalyzerConfig;
  status: 'queued';
}

export function createAnalyzerJobRequest(id: string, suiteId: string | undefined, config: EvaluationResultsAnalyzerConfig): AnalyzerJobRequest {
  return { id, suiteId, config, status: 'queued' };
}
