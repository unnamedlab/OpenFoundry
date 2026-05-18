import { useMemo, useState, type ReactNode } from 'react';

import type {
  EvaluationEvaluator,
  EvaluationSuiteColumn,
  EvaluationTestCase,
  EvaluationTargetFunction,
} from '@/lib/api/evals';
import {
  defaultEvaluationTargetVersion,
  evaluationTargetVersionOptions,
  runEvaluationSuiteBuiltIns,
  type BuiltInEvaluationRunResult,
} from '@/lib/evals/builtins';
import {
  buildLogicAutomationDraft,
  buildLogicAutomationEventChart,
  buildLogicAutomationProposal,
  buildLogicUsageSurfaces,
  buildLogicRunHistoryDatasetRow,
  buildLogicSecurityBoundary,
  buildDebuggerBlockTraces,
  calculateLogicMetrics,
  compareLogicSavedVersions,
  createLogicRunHistoryDatasetConfig,
  createLogicSavedVersion,
  estimateLogicComputeUsage,
  estimateLogicEvaluationComputeUsage,
  executeDraftLogicPreview,
  filterLogicRunsForViewer,
  addLogicFileToBranch,
  editLogicFileOnBranch,
  getLogicBranchMergeReadiness,
  limitLogicRunHistoryDatasetRows,
  logicBranchFunctionAvailable,
  logicExecutionModePolicy,
  logicFilePermissionDecision,
  logicProjectScopedRunHistoryDatasetRid,
  mergeLogicFileBranch,
  publishLogicSavedVersion,
  publishLogicVersionOnBranch,
  rebaseLogicFileOnBranch,
  removeLogicFileFromBranch,
  requestLogicBranchReview,
  reviewLogicBranchProposal,
  validateLlmBlock,
  validateApplyActionTool,
  validateCalculatorTool,
  validateConditionalBlock,
  validateCreateVariableBlock,
  validateExecuteFunctionTool,
  validateLogicOutputs,
  validateLoopBlock,
  validateQueryObjectsTool,
  type LogicActionToolConfig,
  type LogicBranchAdapterResource,
  type LogicBranchMergeReadiness,
  type LogicComputeUsageAttribution,
  type LogicComputeUsageSummary,
  type LogicDebuggerBlockTrace,
  type LogicPreviewRunResult,
  type LogicCalculatorToolConfig,
  type LogicExecuteFunctionToolConfig,
  type LogicConditionalBlockConfig,
  type LogicFileSecurityPolicy,
  type LogicLlmBlockConfig,
  type LogicLoopBlockConfig,
  type LogicAutomationEditMode,
  type LogicMetricsSummary,
  type LogicMetricsWindow,
  type LogicOutputDefinition,
  type LogicPermissionExecutionMode,
  type LogicPermissionDecision,
  type LogicRunHistoryDatasetConfig,
  type LogicRunHistoryDatasetRow,
  type LogicRunHistoryRecord,
  type LogicSecurityBoundary,
  type LogicSavedVersion,
  type LogicUsageBundle,
  type LogicValueType,
  type LogicVersionComparison,
  type LogicVersionDefinition,
  type LogicVariableBlockConfig,
} from '@/lib/logic/blocks';
import {
  LOGIC_INPUT_TYPES,
  validateLogicInputBoard,
  validateLogicInputDefinition,
  type LogicInputDefinition,
  type LogicInputType,
} from '@/lib/logic/inputs';

const RIGHT_RAIL = [
  'Uses',
  'Automations',
  'Evaluations',
  'Run history',
  'Version history',
  'Branching',
  'Metrics',
  'Compute',
  'Execution settings',
  'Security',
] as const;

const QUERY_OBJECT_TYPES = ['Customer', 'Order', 'Shipment'] as const;
const QUERY_PROPERTIES: Record<string, string[]> = {
  Customer: ['name', 'tier', 'status', 'openCases', 'region'],
  Order: ['orderId', 'status', 'value', 'createdAt'],
  Shipment: ['shipmentId', 'carrier', 'eta', 'riskScore'],
};

const BLOCK_OUTPUT_TYPES: Record<string, LogicValueType> = {
  'llm.text': 'string',
  'llm.structured': 'json',
  'calculator.riskScore': 'double',
  'loop.recommendations': 'list',
  'action.preview': 'ontology_edit_bundle',
};

const SAMPLE_INPUTS: LogicInputDefinition[] = [
  {
    id: 'input-1',
    name: 'Customer record',
    apiName: 'customerRecord',
    type: 'object',
    required: true,
    objectTypeId: 'Customer',
    description: 'Ontology object selected by the caller.',
  },
  {
    id: 'input-2',
    name: 'Complaint text',
    apiName: 'complaintText',
    type: 'string',
    required: true,
    defaultValue: 'Late shipment reported by customer.',
    description: 'Free-text prompt context.',
  },
  {
    id: 'input-3',
    name: 'Reference media',
    apiName: 'referenceMedia',
    type: 'media_reference',
    required: false,
    mediaSetRid: 'media.set.demo',
    description: 'Optional image/audio/video evidence from Media Sets.',
  },
  {
    id: 'input-4',
    name: 'Response model',
    apiName: 'responseModel',
    type: 'model',
    required: true,
    modelVariableKind: 'llm',
    compatibleModelKinds: ['llm', 'vision'],
    description: 'Model variable passed into the Use LLM block.',
  },
  {
    id: 'input-5',
    name: 'Base risk',
    apiName: 'baseRisk',
    type: 'integer',
    required: true,
    defaultValue: '35',
    description: 'Numeric signal for calculator tooling.',
  },
  {
    id: 'input-6',
    name: 'Delay hours',
    apiName: 'delayHours',
    type: 'double',
    required: true,
    defaultValue: '6',
    description: 'Exact computation input for LLM workflows.',
  },
  {
    id: 'input-7',
    name: 'Related shipments',
    apiName: 'relatedShipments',
    type: 'object_list',
    required: false,
    objectTypeId: 'Shipment',
    description: 'List input used by loop blocks.',
  },
];

const SAMPLE_LLM_BLOCK: LogicLlmBlockConfig = {
  id: 'llm-risk-summary',
  name: 'Summarize customer risk',
  modelBinding: { mode: 'model_variable', modelVariableApiName: 'responseModel' },
  systemPrompt: 'You are an operations copilot that explains customer risk with concise evidence.',
  taskPrompt: 'Use {{customerRecord}} and {{complaintText}} to recommend the next best action.',
  promptVariableRefs: ['customerRecord', 'complaintText'],
  structuredOutput: {
    kind: 'json_schema',
    schemaJson: '{"type":"object","properties":{"risk":{"type":"string"},"nextAction":{"type":"string"}}}',
  },
  maxOutputTokens: 768,
  toolAccess: [
    {
      kind: 'query_objects',
      name: 'Customer facts',
      objectTypeId: 'Customer',
      selectedProperties: ['name', 'tier', 'status'],
      readableObjectTypeIds: ['Customer', 'Order', 'Shipment'],
      readablePropertiesByObjectType: QUERY_PROPERTIES,
      maxObjects: 8,
    },
    {
      kind: 'apply_action',
      name: 'Open service recovery action',
      actionTypeId: 'create-service-case',
      allowedActionTypeIds: ['create-service-case', 'assign-account-owner'],
      expectedParameters: { customer: 'object', summary: 'string' },
      parameterMappings: { customer: 'customerRecord', summary: 'complaintText' },
      invocationMode: 'preview',
      invocationSurface: 'draft_preview',
      logicPublished: false,
    },
    {
      kind: 'execute_function',
      name: 'Calculate SLA impact',
      functionRid: 'fn.slaImpact.ts',
      functionKind: 'typescript',
      allowedFunctionRids: ['fn.slaImpact.ts', 'fn.route.py', 'logic.existingRisk'],
      signature: { parameters: { complaint: 'string' }, returnType: 'json' },
      parameterMappings: { complaint: 'complaintText' },
      expectedOutputType: 'json',
    },
    {
      kind: 'calculator',
      name: 'Exact risk score',
      expression: '(baseRisk + delayHours * 2) / 100',
      parameterRefs: ['baseRisk', 'delayHours'],
      outputType: 'double',
    },
  ],
};

const SAMPLE_VARIABLE_BLOCK: LogicVariableBlockConfig = {
  id: 'var-escalation-note',
  apiName: 'escalationNote',
  valueType: 'string',
  source: 'literal',
  literalValue: 'Escalate if SLA risk is high.',
};

const SAMPLE_CONDITIONAL_BLOCK: LogicConditionalBlockConfig = {
  id: 'cond-risk-threshold',
  conditionExpression: 'baseRisk > 50 || delayHours > 4',
  trueOutputType: 'string',
  falseOutputType: 'string',
};

const SAMPLE_LOOP_BLOCK: LogicLoopBlockConfig = {
  id: 'loop-related-shipments',
  inputApiName: 'relatedShipments',
  elementVariableApiName: 'shipment',
  indexVariableApiName: 'shipmentIndex',
  bodyOutputType: 'string',
  outputAggregation: 'list',
  finalOutputType: 'list',
  containsActionTool: false,
  parallel: true,
};

const SAMPLE_FINAL_LOGIC_OUTPUTS: LogicOutputDefinition[] = [
  {
    id: 'out-final-answer',
    name: 'Final answer',
    apiName: 'finalAnswer',
    outputType: 'string',
    source: 'block_output',
    sourceId: 'llm.text',
    final: true,
    workshopUsage: 'markdown_display',
  },
];

const INTERMEDIATE_PARAMETER_CANDIDATES: Array<Pick<LogicOutputDefinition, 'id' | 'name' | 'apiName' | 'outputType' | 'sourceId'> & { description: string }> = [
  {
    id: 'out-llm-draft',
    name: 'LLM text draft',
    apiName: 'llmTextDraft',
    outputType: 'string',
    sourceId: 'llm.text',
    description: 'Prompt-rendered text before final output mapping.',
  },
  {
    id: 'out-risk-score',
    name: 'Risk score',
    apiName: 'riskScore',
    outputType: 'double',
    sourceId: 'calculator.riskScore',
    description: 'Calculator block score used by downstream routing.',
  },
  {
    id: 'out-shipment-recommendations',
    name: 'Shipment recommendations',
    apiName: 'shipmentRecommendations',
    outputType: 'list',
    sourceId: 'loop.recommendations',
    description: 'Loop block recommendations for related shipments.',
  },
];

const DEFAULT_INTERMEDIATE_PARAMETER_SOURCE_IDS = ['calculator.riskScore'];

const SAMPLE_EFFECT_LOGIC_OUTPUTS: LogicOutputDefinition[] = [
  {
    id: 'out-action-edits',
    name: 'Action edit preview',
    apiName: 'actionEditPreview',
    outputType: 'ontology_edit_bundle',
    source: 'ontology_edit_bundle',
    sourceId: 'action.preview',
    final: false,
    workshopUsage: 'none',
  },
];

function buildIntermediateParameterOutputs(sourceIds: string[]): LogicOutputDefinition[] {
  const selected = new Set(sourceIds);
  return INTERMEDIATE_PARAMETER_CANDIDATES
    .filter((candidate) => selected.has(candidate.sourceId))
    .map((candidate) => ({
      id: candidate.id,
      name: candidate.name,
      apiName: candidate.apiName,
      outputType: candidate.outputType,
      source: 'intermediate',
      sourceId: candidate.sourceId,
      final: false,
      workshopUsage: 'none',
      intermediateParameter: true,
      exposedBlockOutputId: candidate.sourceId,
    }));
}

function buildLogicOutputs(intermediateSourceIds = DEFAULT_INTERMEDIATE_PARAMETER_SOURCE_IDS): LogicOutputDefinition[] {
  return [
    ...SAMPLE_FINAL_LOGIC_OUTPUTS,
    ...buildIntermediateParameterOutputs(intermediateSourceIds),
    ...SAMPLE_EFFECT_LOGIC_OUTPUTS,
  ];
}

const DEFAULT_RUN_INPUTS: Record<string, string> = {
  customerRecord: 'Customer: Acme Logistics / tier: Gold / open cases: 2',
  complaintText: 'Shipment 4421 missed its SLA. Explain the likely risk and recommended next action.',
  referenceMedia: 'media.set.demo/image-4421',
  responseModel: 'gpt-4.1-mini',
  baseRisk: '35',
  delayHours: '6',
  relatedShipments: '[{"shipmentId":"4421","carrier":"Northwind","riskScore":0.47}]',
};

const CURRENT_LOGIC_ACTOR = { id: 'casey-author', name: 'Casey Author' };
const OTHER_LOGIC_ACTOR = { id: 'morgan-reviewer', name: 'Morgan Reviewer' };
const LOGIC_PROJECT_ID = 'customer-operations';
const DEFAULT_LOGIC_FUNCTION_RID = 'logic.customer-triage';
const DEFAULT_RUN_HISTORY_DATASET_RID = logicProjectScopedRunHistoryDatasetRid(LOGIC_PROJECT_ID);
const LOGIC_PROJECT_SECURITY_RESOURCES = [
  'object_type:Customer',
  'object_type:Shipment',
  'action_type:create-service-case',
  'function:fn.slaImpact.ts',
  'media_set:media.set.demo',
];
const LOGIC_SECURITY_POLICY: LogicFileSecurityPolicy = {
  ownerIds: [CURRENT_LOGIC_ACTOR.id],
  managerIds: [],
  editorIds: [],
  viewerIds: [OTHER_LOGIC_ACTOR.id],
  invokerIds: [CURRENT_LOGIC_ACTOR.id, 'automation-service'],
  allowedObjectTypes: [...QUERY_OBJECT_TYPES],
  readablePropertiesByObjectType: QUERY_PROPERTIES,
  allowedActionTypes: ['create-service-case', 'assign-account-owner'],
  allowedFunctionRids: ['fn.slaImpact.ts', 'fn.route.py', 'logic.existingRisk'],
  allowedMediaSetRids: ['media.set.demo'],
  allowedResultDatasetRids: [DEFAULT_RUN_HISTORY_DATASET_RID],
  projectImportedResourceIds: LOGIC_PROJECT_SECURITY_RESOURCES,
  markingAccessibleResourceIds: LOGIC_PROJECT_SECURITY_RESOURCES,
  sensitivePropertiesByObjectType: {
    Customer: ['status', 'openCases'],
    Shipment: ['riskScore'],
  },
  broadObjectAccessThreshold: 5,
  promptReviewRequired: true,
  redactionPolicyId: 'logic-customer-minimization-v1',
  modelAllowlist: ['gpt-4.1-mini', 'gpt-4.1'],
  exportLoggingRestricted: true,
};

type ConfigTab = 'inputs' | 'blocks' | 'outputs';
type ResourceRailEntry = (typeof RIGHT_RAIL)[number];
type LogicEvaluationSuiteSource = 'logic_preview' | 'evals_sidebar' | 'aip_evals_app';

interface LogicEvaluationSuitePreview {
  id: string;
  name: string;
  source: LogicEvaluationSuiteSource;
  targetFunctions: EvaluationTargetFunction[];
  testCaseColumns: EvaluationSuiteColumn[];
  testCases: EvaluationTestCase[];
  evaluators: EvaluationEvaluator[];
  runHistory: BuiltInEvaluationRunResult[];
  resultsDatasetRid: string;
  permissions: Record<string, string[]>;
  createdAtIso: string;
}

const EMPTY_LOGIC_DEFINITION: LogicVersionDefinition = { inputs: [], blocks: [], outputs: [] };

function buildLogicDefinition(
  inputs: LogicInputDefinition[],
  llmBlock: LogicLlmBlockConfig,
  intermediateSourceIds = DEFAULT_INTERMEDIATE_PARAMETER_SOURCE_IDS,
): LogicVersionDefinition {
  return {
    inputs,
    blocks: [{
      ...llmBlock,
      kind: 'use_llm',
      type: 'use_llm',
    }],
    outputs: buildLogicOutputs(intermediateSourceIds),
  };
}

function createInitialVersion(): LogicSavedVersion {
  const initialDefinition = buildLogicDefinition(SAMPLE_INPUTS, SAMPLE_LLM_BLOCK);
  return {
    ...createLogicSavedVersion(EMPTY_LOGIC_DEFINITION, initialDefinition, 'OpenFoundry demo author', new Date('2026-05-13T09:00:00Z'), 7),
    id: 'logic-version-demo-7',
  };
}

function runRetentionExpiresAt(started: Date, executionMode: LogicPermissionExecutionMode) {
  if (executionMode === 'project_scoped') return new Date(started.getTime() + 100 * 365 * 24 * 60 * 60 * 1000).toISOString();
  return new Date(started.getTime() + 24 * 60 * 60 * 1000).toISOString();
}

function computeUsageWithAttribution(
  usage: LogicComputeUsageSummary,
  attribution: Partial<LogicComputeUsageAttribution>,
): LogicComputeUsageSummary {
  const summaryAttribution = { ...usage.attribution, ...attribution };
  return {
    ...usage,
    attribution: summaryAttribution,
    lineItems: usage.lineItems.map((item) => ({
      ...item,
      attribution: { ...item.attribution, ...summaryAttribution, blockId: item.blockId ?? item.attribution.blockId },
    })),
  };
}

function runHistoryRecord(
  run: LogicPreviewRunResult,
  actor = CURRENT_LOGIC_ACTOR,
  options: {
    executionMode?: LogicPermissionExecutionMode;
    datasetConfig?: LogicRunHistoryDatasetConfig;
    inputValues?: Record<string, string>;
    publishedVersion?: LogicSavedVersion;
  } = {},
): LogicRunHistoryRecord {
  const started = new Date(run.metadata.startedAtIso);
  const executionMode = options.executionMode ?? 'user_scoped';
  const inputValues = options.inputValues ?? DEFAULT_RUN_INPUTS;
  const completedAtIso = new Date(started.getTime() + run.durationMs).toISOString();
  const computeUsage = computeUsageWithAttribution(run.metadata.computeUsage, {
    logicFileId: DEFAULT_LOGIC_FUNCTION_RID,
    logicVersionId: options.publishedVersion?.id,
    publishedVersionNumber: options.publishedVersion?.versionNumber,
    actorId: actor.id,
    projectId: LOGIC_PROJECT_ID,
    permissionSubjectId: executionMode === 'project_scoped' ? LOGIC_PROJECT_ID : actor.id,
    invocationSurface: 'draft_preview',
  });
  return {
    id: run.id.replace(/^draft-/, 'logic-run-'),
    actorId: actor.id,
    actorName: actor.name,
    executionMode,
    status: run.status === 'idle' || run.status === 'running' ? 'failed' : run.status,
    invocationSurface: 'draft_preview',
    startedAtIso: started.toISOString(),
    retentionExpiresAtIso: runRetentionExpiresAt(started, executionMode),
    durationMs: run.durationMs,
    errorMessage: run.errors[0]?.message,
    runHistoryDatasetRid: executionMode === 'project_scoped' ? options.datasetConfig?.datasetRid : undefined,
    inputs: inputValues,
    outputs: run.outputs,
    intermediateParameters: run.intermediateParameters,
    model: inputValues.responseModel,
    branchName: 'main',
    publishedVersionId: options.publishedVersion?.id,
    publishedVersionNumber: options.publishedVersion?.versionNumber,
    completedAtIso,
    serviceContext: {
      invocationSurface: 'draft_preview',
      permissionSubject: executionMode === 'project_scoped' ? 'project' : 'initiating_user',
      permissionSubjectId: executionMode === 'project_scoped' ? LOGIC_PROJECT_ID : actor.id,
      initiatingUserId: actor.id,
      projectId: executionMode === 'project_scoped' ? LOGIC_PROJECT_ID : undefined,
      logsVisibleTo: executionMode === 'project_scoped' ? 'project_viewers' : 'initiating_user',
    },
    traceRefs: [{
      id: `trace-${run.id}`,
      kind: 'debugger',
      href: `/logic?run=${encodeURIComponent(run.id)}`,
      visibility: executionMode === 'project_scoped' ? 'project_viewers' : 'initiating_user',
    }],
    computeUsage,
  };
}

function hiddenPeerRun(
  now = new Date(),
  executionMode: LogicPermissionExecutionMode = 'user_scoped',
  datasetConfig?: LogicRunHistoryDatasetConfig,
  publishedVersion?: LogicSavedVersion,
): LogicRunHistoryRecord {
  const started = new Date(now.getTime() - 35 * 60 * 1000);
  return {
    id: 'logic-run-peer-hidden',
    actorId: OTHER_LOGIC_ACTOR.id,
    actorName: OTHER_LOGIC_ACTOR.name,
    executionMode,
    status: 'succeeded',
    invocationSurface: 'workshop',
    startedAtIso: started.toISOString(),
    retentionExpiresAtIso: runRetentionExpiresAt(started, executionMode),
    durationMs: 126,
    runHistoryDatasetRid: executionMode === 'project_scoped' ? datasetConfig?.datasetRid : undefined,
    inputs: { customerRecord: 'Customer: Northwind Freight', complaintText: 'Delivery status changed by another project viewer.' },
    outputs: { finalAnswer: 'Notify account owner.' },
    intermediateParameters: { riskScore: 18 },
    model: 'gpt-4.1-mini',
    branchName: 'main',
    publishedVersionId: publishedVersion?.id,
    publishedVersionNumber: publishedVersion?.versionNumber,
    completedAtIso: new Date(started.getTime() + 126).toISOString(),
    serviceContext: {
      invocationSurface: 'workshop',
      permissionSubject: executionMode === 'project_scoped' ? 'project' : 'initiating_user',
      permissionSubjectId: executionMode === 'project_scoped' ? LOGIC_PROJECT_ID : OTHER_LOGIC_ACTOR.id,
      initiatingUserId: OTHER_LOGIC_ACTOR.id,
      projectId: executionMode === 'project_scoped' ? LOGIC_PROJECT_ID : undefined,
      logsVisibleTo: executionMode === 'project_scoped' ? 'project_viewers' : 'initiating_user',
    },
    traceRefs: [{
      id: 'trace-logic-run-peer-hidden',
      kind: 'logs',
      href: '/logic?run=logic-run-peer-hidden',
      visibility: executionMode === 'project_scoped' ? 'project_viewers' : 'initiating_user',
    }],
  };
}

function buildLogicEvaluationSuitePreview(
  source: LogicEvaluationSuiteSource,
  definition: LogicVersionDefinition,
  latestRun: LogicPreviewRunResult | undefined,
  publishedFunctionRid: string | undefined,
  inputValues: Record<string, string> = {},
  now = new Date(),
): LogicEvaluationSuitePreview {
  const finalOutputs = definition.outputs.filter((output) => output.final);
  const intermediateOutputs = definition.outputs.filter((output) => output.intermediateParameter);
  const evaluatedOutputs = [...finalOutputs, ...intermediateOutputs];
  const outputColumns = (evaluatedOutputs.length > 0 ? evaluatedOutputs : definition.outputs).map((output) => ({
    id: `expected-${output.id}`,
    name: `Expected ${output.name}`,
    apiName: `expected_${output.apiName}`,
    type: output.outputType,
    role: 'expected_output',
    evaluated_output_api_name: output.apiName,
  }));
  const intermediateColumns = intermediateOutputs.map((output) => ({
    id: `intermediate-${output.id}`,
    name: output.name,
    apiName: output.apiName,
    type: output.outputType,
    role: 'intermediate_parameter',
    source_block_output_id: output.exposedBlockOutputId ?? output.sourceId,
  }));
  const targetID = publishedFunctionRid ?? 'logic.customer-triage';
  const actualOutput = finalOutputs[0]?.apiName ?? definition.outputs[0]?.apiName ?? 'finalAnswer';
  const latestOutputValue = (output: LogicOutputDefinition) => (
    output.intermediateParameter
      ? latestRun?.intermediateParameters?.[output.apiName]
      : latestRun?.outputs?.[output.apiName] ?? (output.apiName === 'finalAnswer' ? latestRun?.result : undefined)
  );
  const testCaseValues = Object.fromEntries([
    ...definition.inputs.map((input) => [input.apiName, inputValues[input.apiName] ?? input.defaultValue ?? '']),
    ...intermediateOutputs.map((output) => [output.apiName, latestOutputValue(output) ?? (output.outputType === 'double' ? 0 : 'Intermediate value')]),
    ...outputColumns.map((column, index) => {
      const output = (evaluatedOutputs.length > 0 ? evaluatedOutputs : definition.outputs)[index];
      return [column.apiName, output ? latestOutputValue(output) ?? (output.outputType === 'double' ? 0 : 'Expected result') : 'Expected result'];
    }),
  ]);
  const generatedNameHint = String(testCaseValues.complaintText ?? testCaseValues.reviewText ?? 'Logic preview case')
    .replace(/[^A-Za-z0-9 ]/g, ' ')
    .split(/\s+/)
    .filter(Boolean)
    .slice(0, 5)
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1).toLowerCase())
    .join(' ') || 'Logic Preview Case';
  return {
    id: `eval-suite-${now.getTime().toString(36)}`,
    name: source === 'logic_preview' ? 'Preview regression suite' : source === 'evals_sidebar' ? 'Manual Logic eval suite' : 'AIP Evals suite',
    source,
    targetFunctions: [{
      id: targetID,
      kind: 'logic',
      functionRid: targetID,
      function_rid: targetID,
      version: publishedFunctionRid ? 'published' : 'last_saved_or_preview',
      signature: {
        inputs: definition.inputs.map((input) => ({ apiName: input.apiName, type: input.type })),
        outputs: definition.outputs.map((output) => ({
          apiName: output.apiName,
          outputType: output.outputType,
          intermediateParameter: output.intermediateParameter,
          final: output.final,
          sourceId: output.sourceId,
        })),
      },
    }],
    testCaseColumns: [
      ...definition.inputs.map((input) => ({
        id: input.id,
        name: input.name,
        apiName: input.apiName,
        type: input.type,
        role: 'input',
      })),
      ...intermediateColumns,
      ...outputColumns,
    ],
    testCases: [{
      id: `case-${latestRun?.id ?? now.getTime().toString(36)}`,
      name: generatedNameHint,
      source: latestRun ? 'logic_preview' : 'manual',
      values: testCaseValues,
      metadata: latestRun ? {
        preview_run_id: latestRun.id,
        preview_started_at: latestRun.metadata.startedAtIso,
        source_surface: 'logic_preview',
      } : { source_surface: source },
      generated_name_hint: generatedNameHint,
    }],
    evaluators: outputColumns.map((column, index) => ({
      id: `exact-${(evaluatedOutputs[index]?.apiName ?? column.apiName).replaceAll('_', '-')}`,
      kind: 'built_in',
      evaluator: 'exact_string_match',
      target_id: targetID,
      mappings: { actual: evaluatedOutputs[index]?.apiName ?? actualOutput, expected: column.apiName },
      target_mappings: { [targetID]: { actual: evaluatedOutputs[index]?.apiName ?? actualOutput, expected: column.apiName } },
      objective: { metric: 'matches', target: true },
    })),
    runHistory: [],
    resultsDatasetRid: `ri.foundry.dataset.eval.${now.getTime().toString(36)}`,
    permissions: { owners: [CURRENT_LOGIC_ACTOR.id], editors: [], viewers: [] },
    createdAtIso: now.toISOString(),
  };
}

function StatusPill({ children, tone = 'info' }: { children: ReactNode; tone?: 'info' | 'success' | 'warning' }) {
  const color = tone === 'success' ? 'var(--status-success)' : tone === 'warning' ? 'var(--status-warning)' : 'var(--status-info)';
  const bg = tone === 'success' ? 'var(--status-success-bg)' : tone === 'warning' ? 'var(--status-warning-bg)' : 'var(--status-info-bg)';
  return (
    <span style={{ borderRadius: 999, background: bg, color, padding: '2px 8px', fontSize: 12, fontWeight: 600 }}>
      {children}
    </span>
  );
}

function Field({ label, children }: { label: string; children: ReactNode }) {
  return (
    <label style={{ display: 'grid', gap: 4 }}>
      <span className="of-eyebrow">{label}</span>
      {children}
    </label>
  );
}

function computeSecondsLabel(value: number) {
  return `${Math.round(value).toLocaleString()} compute-sec`;
}

function ComputeWarningList({ usage }: { usage?: LogicComputeUsageSummary }) {
  if (!usage || usage.warnings.length === 0) return null;
  return (
    <div className="of-panel" style={{ padding: 10, display: 'grid', gap: 6, borderColor: 'var(--status-warning)' }}>
      <strong style={{ fontSize: 13 }}>Usage warning</strong>
      {usage.warnings.map((warning) => (
        <span key={`${warning.field}-${warning.message}`} className="of-text-muted" style={{ fontSize: 12 }}>
          {warning.message}
        </span>
      ))}
    </div>
  );
}

function ComputeSummaryStrip({ usage }: { usage: LogicComputeUsageSummary }) {
  return (
    <div className="of-panel-muted" style={{ padding: 10, display: 'grid', gridTemplateColumns: 'repeat(4, minmax(120px, 1fr))', gap: 8 }}>
      <div><p className="of-eyebrow">Total</p><strong>{computeSecondsLabel(usage.totalComputeSeconds)}</strong></div>
      <div><p className="of-eyebrow">LLM blocks</p><strong>{computeSecondsLabel(usage.llmBlockComputeSeconds)}</strong></div>
      <div><p className="of-eyebrow">Tools</p><strong>{computeSecondsLabel(usage.llmToolComputeSeconds)}</strong></div>
      <div><p className="of-eyebrow">Downstream</p><strong>{computeSecondsLabel(usage.downstreamComputeSeconds)}</strong></div>
    </div>
  );
}

function InputsBoard({ inputs, selectedId, onSelect, onChange }: {
  inputs: LogicInputDefinition[];
  selectedId: string;
  onSelect: (id: string) => void;
  onChange: (input: LogicInputDefinition) => void;
}) {
  const selected = inputs.find((input) => input.id === selectedId) ?? inputs[0];
  const selectedIssues = validateLogicInputDefinition(selected);
  const boardIssues = validateLogicInputBoard(inputs);

  return (
    <div style={{ display: 'grid', gridTemplateColumns: 'minmax(220px, 0.8fr) minmax(280px, 1fr)', gap: 10 }}>
      <div className="of-panel-muted" style={{ padding: 10 }}>
        <div style={{ display: 'flex', justifyContent: 'space-between', gap: 8, alignItems: 'center' }}>
          <p className="of-eyebrow">Logic inputs</p>
          <StatusPill tone={boardIssues.length === 0 ? 'success' : 'warning'}>{boardIssues.length} issues</StatusPill>
        </div>
        <div style={{ display: 'grid', gap: 8, marginTop: 8 }}>
          {inputs.map((input) => {
            const issues = validateLogicInputDefinition(input);
            return (
              <button
                key={input.id}
                type="button"
                onClick={() => onSelect(input.id)}
                className="of-panel"
                style={{
                  padding: 10,
                  textAlign: 'left',
                  borderColor: input.id === selected.id ? 'var(--border-focus)' : 'var(--border-default)',
                  background: input.id === selected.id ? 'var(--status-info-bg)' : 'var(--bg-panel)',
                }}
              >
                <div style={{ display: 'flex', justifyContent: 'space-between', gap: 8 }}>
                  <strong>{input.name}</strong>
                  <span className="of-text-muted">{input.type}</span>
                </div>
                <div className="of-text-muted" style={{ marginTop: 4 }}>
                  {input.apiName} · {input.required ? 'required' : 'optional'}
                </div>
                {issues.length > 0 && <div style={{ color: 'var(--status-warning)', marginTop: 4 }}>{issues[0].message}</div>}
              </button>
            );
          })}
        </div>
      </div>

      <div className="of-panel-muted" style={{ padding: 10 }}>
        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 8 }}>
          <p className="of-eyebrow">Selected input</p>
          <StatusPill tone={selectedIssues.length === 0 ? 'success' : 'warning'}>
            {selectedIssues.length === 0 ? 'valid' : 'needs attention'}
          </StatusPill>
        </div>
        <div style={{ display: 'grid', gap: 10, marginTop: 10 }}>
          <Field label="Display name">
            <input className="of-input" value={selected.name} onChange={(event) => onChange({ ...selected, name: event.target.value })} />
          </Field>
          <Field label="API name">
            <input className="of-input" value={selected.apiName} onChange={(event) => onChange({ ...selected, apiName: event.target.value })} />
          </Field>
          <Field label="Type">
            <select
              className="of-select"
              value={selected.type}
              onChange={(event) => onChange({ ...selected, type: event.target.value as LogicInputType })}
            >
              {LOGIC_INPUT_TYPES.map((type) => <option key={type} value={type}>{type}</option>)}
            </select>
          </Field>
          <label style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
            <input type="checkbox" checked={selected.required} onChange={(event) => onChange({ ...selected, required: event.target.checked })} />
            Required input
          </label>
          <Field label="Default value">
            <input
              className="of-input"
              value={selected.defaultValue ?? ''}
              placeholder="Typed default value, JSON for arrays/structs"
              onChange={(event) => onChange({ ...selected, defaultValue: event.target.value })}
            />
          </Field>
          <Field label="Object type / model compatibility">
            <input
              className="of-input"
              value={selected.objectTypeId ?? selected.modelVariableKind ?? selected.mediaSetRid ?? ''}
              placeholder="Customer, object set backing type, llm, or media set RID"
              onChange={(event) => {
                const value = event.target.value;
                if (selected.type === 'model') onChange({ ...selected, modelVariableKind: value as LogicInputDefinition['modelVariableKind'] });
                else if (selected.type === 'media_reference') onChange({ ...selected, mediaSetRid: value });
                else onChange({ ...selected, objectTypeId: value, objectSetObjectTypeId: selected.type === 'object_set' ? value : selected.objectSetObjectTypeId });
              }}
            />
          </Field>
          {selectedIssues.length > 0 && (
            <div className="of-status-warning" style={{ padding: 10, borderRadius: 4 }}>
              <strong>Validation</strong>
              <ul style={{ margin: '6px 0 0', paddingLeft: 18 }}>
                {selectedIssues.map((issue) => <li key={`${issue.field}-${issue.message}`}>{issue.message}</li>)}
              </ul>
            </div>
          )}
        </div>
      </div>
    </div>
  );
}

function BlocksBoard({ inputs, llmBlock, onChange }: {
  inputs: LogicInputDefinition[];
  llmBlock: LogicLlmBlockConfig;
  onChange: (block: LogicLlmBlockConfig) => void;
}) {
  const issues = validateLlmBlock(llmBlock, inputs);
  const queryTool = llmBlock.toolAccess.find((tool) => tool.kind === 'query_objects');
  const actionTool = llmBlock.toolAccess.find((tool): tool is LogicActionToolConfig => tool.kind === 'apply_action');
  const functionTool = llmBlock.toolAccess.find((tool): tool is LogicExecuteFunctionToolConfig => tool.kind === 'execute_function');
  const calculatorTool = llmBlock.toolAccess.find((tool): tool is LogicCalculatorToolConfig => tool.kind === 'calculator');
  const queryIssues = queryTool?.kind === 'query_objects' ? validateQueryObjectsTool(queryTool) : [];
  const actionIssues = actionTool ? validateApplyActionTool(actionTool, inputs) : [];
  const functionIssues = functionTool ? validateExecuteFunctionTool(functionTool, inputs) : [];
  const calculatorIssues = calculatorTool ? validateCalculatorTool(calculatorTool, inputs) : [];
  const variableIssues = validateCreateVariableBlock(SAMPLE_VARIABLE_BLOCK, inputs, BLOCK_OUTPUT_TYPES);
  const conditionalIssues = validateConditionalBlock(SAMPLE_CONDITIONAL_BLOCK);
  const loopIssues = validateLoopBlock(SAMPLE_LOOP_BLOCK, inputs);
  const modelInputs = inputs.filter((input) => input.type === 'model');
  function replaceTool(nextTool: LogicLlmBlockConfig['toolAccess'][number]) {
    onChange({ ...llmBlock, toolAccess: llmBlock.toolAccess.map((tool) => (tool.kind === nextTool.kind ? nextTool : tool)) });
  }
  return (
    <div style={{ display: 'grid', gap: 10 }}>
      <div className="of-panel-muted" style={{ padding: 12 }}>
        <div style={{ display: 'flex', justifyContent: 'space-between', gap: 8, alignItems: 'center' }}>
          <div>
            <p className="of-eyebrow">Use LLM block</p>
            <strong>{llmBlock.name}</strong>
          </div>
          <StatusPill tone={issues.some((issue) => issue.severity === 'error') ? 'warning' : 'success'}>
            {issues.filter((issue) => issue.severity === 'error').length} errors · {issues.filter((issue) => issue.severity === 'warning').length} warnings
          </StatusPill>
        </div>
        <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 10, marginTop: 12 }}>
          <Field label="Model binding">
            <select
              className="of-select"
              value={llmBlock.modelBinding.mode}
              onChange={(event) => onChange({ ...llmBlock, modelBinding: { ...llmBlock.modelBinding, mode: event.target.value as 'fixed' | 'model_variable' } })}
            >
              <option value="fixed">Fixed model</option>
              <option value="model_variable">Model variable for Evals</option>
            </select>
          </Field>
          {llmBlock.modelBinding.mode === 'model_variable' ? (
            <Field label="Model variable input">
              <select
                className="of-select"
                value={llmBlock.modelBinding.modelVariableApiName ?? ''}
                onChange={(event) => onChange({ ...llmBlock, modelBinding: { mode: 'model_variable', modelVariableApiName: event.target.value } })}
              >
                {modelInputs.map((input) => <option key={input.id} value={input.apiName}>{input.apiName}</option>)}
              </select>
            </Field>
          ) : (
            <Field label="Provider model">
              <input
                className="of-input"
                value={llmBlock.modelBinding.providerId ?? 'gpt-4.1-mini'}
                onChange={(event) => onChange({ ...llmBlock, modelBinding: { mode: 'fixed', providerId: event.target.value } })}
              />
            </Field>
          )}
        </div>
        <div style={{ display: 'grid', gap: 10, marginTop: 10 }}>
          <Field label="System prompt">
            <textarea className="of-textarea" style={{ minHeight: 86 }} value={llmBlock.systemPrompt} onChange={(event) => onChange({ ...llmBlock, systemPrompt: event.target.value })} />
          </Field>
          <Field label="Task prompt">
            <textarea className="of-textarea" style={{ minHeight: 96 }} value={llmBlock.taskPrompt} onChange={(event) => onChange({ ...llmBlock, taskPrompt: event.target.value })} />
          </Field>
          <div>
            <p className="of-eyebrow">Prompt variables</p>
            <div style={{ display: 'flex', flexWrap: 'wrap', gap: 6 }}>
              {inputs.map((input) => {
                const checked = llmBlock.promptVariableRefs.includes(input.apiName);
                return (
                  <label key={input.id} className="of-chip" style={{ display: 'inline-flex', gap: 5, alignItems: 'center' }}>
                    <input
                      type="checkbox"
                      checked={checked}
                      onChange={(event) => {
                        const refs = event.target.checked
                          ? [...llmBlock.promptVariableRefs, input.apiName]
                          : llmBlock.promptVariableRefs.filter((ref) => ref !== input.apiName);
                        onChange({ ...llmBlock, promptVariableRefs: refs });
                      }}
                    />
                    {input.apiName}
                  </label>
                );
              })}
            </div>
          </div>
          <Field label="Structured output type">
            <select
              className="of-select"
              value={llmBlock.structuredOutput.kind}
              onChange={(event) => onChange({ ...llmBlock, structuredOutput: { ...llmBlock.structuredOutput, kind: event.target.value as LogicLlmBlockConfig['structuredOutput']['kind'] } })}
            >
              <option value="text">Text</option>
              <option value="json_schema">JSON schema</option>
              <option value="object">Object</option>
              <option value="object_list">Object list</option>
              <option value="ontology_edit_bundle">Ontology edit bundle</option>
            </select>
          </Field>
          <Field label="JSON schema">
            <textarea className="of-textarea" style={{ minHeight: 74 }} value={llmBlock.structuredOutput.schemaJson ?? ''} onChange={(event) => onChange({ ...llmBlock, structuredOutput: { ...llmBlock.structuredOutput, schemaJson: event.target.value } })} />
          </Field>
        </div>
      </div>

      {queryTool && (
        <div className="of-panel-muted" style={{ padding: 12 }}>
          <div style={{ display: 'flex', justifyContent: 'space-between', gap: 8 }}>
            <div>
              <p className="of-eyebrow">Tool access</p>
              <strong>Query objects</strong>
            </div>
            <StatusPill tone={queryIssues.some((issue) => issue.severity === 'error') ? 'warning' : queryIssues.length ? 'warning' : 'success'}>
              {queryIssues.length} access/token notes
            </StatusPill>
          </div>
          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 10, marginTop: 10 }}>
            <Field label="Readable object type">
              <select
                className="of-select"
                value={queryTool.objectTypeId}
                onChange={(event) => replaceTool({ ...queryTool, objectTypeId: event.target.value, selectedProperties: QUERY_PROPERTIES[event.target.value]?.slice(0, 3) ?? [] })}
              >
                {QUERY_OBJECT_TYPES.map((type) => <option key={type} value={type}>{type}</option>)}
              </select>
            </Field>
            <Field label="Max objects">
              <input
                className="of-input"
                type="number"
                min={1}
                value={queryTool.maxObjects}
                onChange={(event) => replaceTool({ ...queryTool, maxObjects: Number(event.target.value) })}
              />
            </Field>
          </div>
          <div style={{ marginTop: 10 }}>
            <p className="of-eyebrow">Selected readable properties</p>
            <div style={{ display: 'flex', flexWrap: 'wrap', gap: 6 }}>
              {(QUERY_PROPERTIES[queryTool.objectTypeId] ?? []).map((property) => {
                const checked = queryTool.selectedProperties.includes(property);
                return (
                  <label key={property} className="of-chip" style={{ display: 'inline-flex', gap: 5, alignItems: 'center' }}>
                    <input
                      type="checkbox"
                      checked={checked}
                      onChange={(event) => {
                        const selectedProperties = event.target.checked
                          ? [...queryTool.selectedProperties, property]
                          : queryTool.selectedProperties.filter((candidate) => candidate !== property);
                        replaceTool({ ...queryTool, selectedProperties });
                      }}
                    />
                    {property}
                  </label>
                );
              })}
            </div>
          </div>
          {queryIssues.length > 0 && (
            <div className="of-status-warning" style={{ padding: 10, borderRadius: 4, marginTop: 10 }}>
              <strong>Tool access validation</strong>
              <ul style={{ margin: '6px 0 0', paddingLeft: 18 }}>
                {queryIssues.map((issue) => <li key={`${issue.field}-${issue.message}`}>{issue.message}</li>)}
              </ul>
            </div>
          )}
        </div>
      )}

      {actionTool && (
        <div className="of-panel-muted" style={{ padding: 12 }}>
          <div style={{ display: 'flex', justifyContent: 'space-between', gap: 8 }}>
            <div>
              <p className="of-eyebrow">Ontology edits</p>
              <strong>Apply action</strong>
            </div>
            <StatusPill tone={actionIssues.some((issue) => issue.severity === 'error') ? 'warning' : 'success'}>{actionIssues.length} edit guardrails</StatusPill>
          </div>
          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 10, marginTop: 10 }}>
            <Field label="Action type">
              <select className="of-select" value={actionTool.actionTypeId} onChange={(event) => replaceTool({ ...actionTool, actionTypeId: event.target.value })}>
                {actionTool.allowedActionTypeIds.map((actionType) => <option key={actionType} value={actionType}>{actionType}</option>)}
              </select>
            </Field>
            <Field label="Invocation mode">
              <select className="of-select" value={actionTool.invocationMode} onChange={(event) => replaceTool({ ...actionTool, invocationMode: event.target.value as LogicActionToolConfig['invocationMode'] })}>
                <option value="preview">Preview proposed edits only</option>
                <option value="commit">Commit when published + action/automation invoked</option>
              </select>
            </Field>
          </div>
          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 10, marginTop: 10 }}>
            {Object.entries(actionTool.expectedParameters).map(([parameter]) => (
              <Field key={parameter} label={`Parameter: ${parameter}`}>
                <select className="of-select" value={actionTool.parameterMappings[parameter] ?? ''} onChange={(event) => replaceTool({ ...actionTool, parameterMappings: { ...actionTool.parameterMappings, [parameter]: event.target.value } })}>
                  {inputs.map((input) => <option key={input.id} value={input.apiName}>{input.apiName}</option>)}
                </select>
              </Field>
            ))}
          </div>
          <p className="of-text-muted" style={{ margin: '10px 0 0' }}>Preview records proposed Ontology edits in the debugger; real edits require published Logic plus action or automation invocation.</p>
        </div>
      )}

      {functionTool && (
        <div className="of-panel-muted" style={{ padding: 12 }}>
          <div style={{ display: 'flex', justifyContent: 'space-between', gap: 8 }}>
            <div>
              <p className="of-eyebrow">Function tools</p>
              <strong>Execute function</strong>
            </div>
            <StatusPill tone={functionIssues.some((issue) => issue.severity === 'error') ? 'warning' : 'success'}>{functionTool.functionKind} · {functionIssues.length} signature notes</StatusPill>
          </div>
          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 10, marginTop: 10 }}>
            <Field label="Function">
              <select className="of-select" value={functionTool.functionRid} onChange={(event) => replaceTool({ ...functionTool, functionRid: event.target.value })}>
                {functionTool.allowedFunctionRids.map((rid) => <option key={rid} value={rid}>{rid}</option>)}
              </select>
            </Field>
            <Field label="Function kind">
              <select className="of-select" value={functionTool.functionKind} onChange={(event) => replaceTool({ ...functionTool, functionKind: event.target.value as LogicExecuteFunctionToolConfig['functionKind'] })}>
                <option value="typescript">TypeScript</option>
                <option value="python">Python</option>
                <option value="existing_logic">Existing Logic</option>
                <option value="function_on_objects">Function on objects</option>
              </select>
            </Field>
          </div>
          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 10, marginTop: 10 }}>
            {Object.entries(functionTool.signature.parameters).map(([parameter]) => (
              <Field key={parameter} label={`Parameter: ${parameter}`}>
                <select className="of-select" value={functionTool.parameterMappings[parameter] ?? ''} onChange={(event) => replaceTool({ ...functionTool, parameterMappings: { ...functionTool.parameterMappings, [parameter]: event.target.value } })}>
                  {inputs.map((input) => <option key={input.id} value={input.apiName}>{input.apiName}</option>)}
                </select>
              </Field>
            ))}
          </div>
        </div>
      )}

      {calculatorTool && (
        <div className="of-panel-muted" style={{ padding: 12 }}>
          <div style={{ display: 'flex', justifyContent: 'space-between', gap: 8 }}>
            <div>
              <p className="of-eyebrow">Exact computation</p>
              <strong>Calculator</strong>
            </div>
            <StatusPill tone={calculatorIssues.some((issue) => issue.severity === 'error') ? 'warning' : 'success'}>{calculatorIssues.length} math notes</StatusPill>
          </div>
          <Field label="Expression">
            <input className="of-input" value={calculatorTool.expression} onChange={(event) => replaceTool({ ...calculatorTool, expression: event.target.value })} />
          </Field>
          <p className="of-text-muted" style={{ margin: '8px 0 0' }}>Calculator uses deterministic arithmetic for values the LLM should not estimate.</p>
        </div>
      )}

      <div className="of-panel-muted" style={{ padding: 12 }}>
        <div style={{ display: 'flex', justifyContent: 'space-between', gap: 8 }}>
          <div>
            <p className="of-eyebrow">Control flow</p>
            <strong>Create variable, conditional, and loop</strong>
          </div>
          <StatusPill tone={[...variableIssues, ...conditionalIssues, ...loopIssues].some((issue) => issue.severity === 'error') ? 'warning' : 'success'}>
            {[...variableIssues, ...conditionalIssues, ...loopIssues].length} flow notes
          </StatusPill>
        </div>
        <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr 1fr', gap: 8, marginTop: 10 }}>
          <div className="of-panel" style={{ padding: 10 }}>
            <p className="of-eyebrow">Create variable</p>
            <strong>{SAMPLE_VARIABLE_BLOCK.apiName}</strong>
            <p className="of-text-muted" style={{ margin: '6px 0 0' }}>{SAMPLE_VARIABLE_BLOCK.valueType} from {SAMPLE_VARIABLE_BLOCK.source}</p>
          </div>
          <div className="of-panel" style={{ padding: 10 }}>
            <p className="of-eyebrow">Conditional</p>
            <strong>{SAMPLE_CONDITIONAL_BLOCK.conditionExpression}</strong>
            <p className="of-text-muted" style={{ margin: '6px 0 0' }}>{SAMPLE_CONDITIONAL_BLOCK.trueOutputType} / {SAMPLE_CONDITIONAL_BLOCK.falseOutputType}</p>
          </div>
          <div className="of-panel" style={{ padding: 10 }}>
            <p className="of-eyebrow">Loop</p>
            <strong>{SAMPLE_LOOP_BLOCK.inputApiName}</strong>
            <p className="of-text-muted" style={{ margin: '6px 0 0' }}>{SAMPLE_LOOP_BLOCK.parallel ? 'parallel' : 'sequential'} · {SAMPLE_LOOP_BLOCK.outputAggregation} aggregation</p>
          </div>
        </div>
        <p className="of-text-muted" style={{ margin: '10px 0 0' }}>Loops validate list/object-list inputs, element/index variables, output aggregation, and action-aware parallelization.</p>
      </div>
    </div>
  );
}

function OutputsBoard({ outputs, selectedIntermediateSourceIds, latestRun, onToggleIntermediateSource }: {
  outputs: LogicOutputDefinition[];
  selectedIntermediateSourceIds: string[];
  latestRun?: LogicPreviewRunResult;
  onToggleIntermediateSource: (sourceId: string) => void;
}) {
  const outputIssues = validateLogicOutputs(outputs, BLOCK_OUTPUT_TYPES);
  const selected = new Set(selectedIntermediateSourceIds);
  return (
    <div style={{ display: 'grid', gap: 8 }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', gap: 8, alignItems: 'center' }}>
        <p className="of-eyebrow">Logic outputs</p>
        <StatusPill tone={outputIssues.some((issue) => issue.severity === 'error') ? 'warning' : 'success'}>{outputIssues.length} output notes</StatusPill>
      </div>
      <div className="of-panel-muted" style={{ padding: 12, display: 'grid', gap: 10 }}>
        <div style={{ display: 'flex', justifyContent: 'space-between', gap: 8, alignItems: 'center', flexWrap: 'wrap' }}>
          <div>
            <strong>Intermediate parameters</strong>
            <p className="of-text-muted" style={{ margin: '4px 0 0' }}>Expose selected block outputs for evaluator mappings and results datasets.</p>
          </div>
          <StatusPill tone={selected.size > 0 ? 'success' : 'info'}>{selected.size} exposed</StatusPill>
        </div>
        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(220px, 1fr))', gap: 8 }}>
          {INTERMEDIATE_PARAMETER_CANDIDATES.map((candidate) => {
            const enabled = selected.has(candidate.sourceId);
            return (
              <button
                key={candidate.sourceId}
                type="button"
                className="of-panel"
                onClick={() => onToggleIntermediateSource(candidate.sourceId)}
                style={{ padding: 10, textAlign: 'left', cursor: 'pointer', borderColor: enabled ? 'var(--status-success)' : undefined }}
              >
                <div style={{ display: 'flex', justifyContent: 'space-between', gap: 8, alignItems: 'center' }}>
                  <strong>{candidate.name}</strong>
                  <StatusPill tone={enabled ? 'success' : 'info'}>{enabled ? 'exposed' : 'hidden'}</StatusPill>
                </div>
                <p className="of-text-muted" style={{ margin: '6px 0 0' }}>{candidate.description}</p>
                <span className="of-chip" style={{ marginTop: 8 }}>{candidate.sourceId} · {candidate.outputType}</span>
              </button>
            );
          })}
        </div>
      </div>
      {outputs.map((output) => {
        const previewValue = output.intermediateParameter
          ? latestRun?.intermediateParameters?.[output.apiName]
          : latestRun?.outputs?.[output.apiName];
        return (
        <div key={output.id} className="of-panel-muted" style={{ padding: 12 }}>
          <div style={{ display: 'flex', justifyContent: 'space-between', gap: 8 }}>
            <strong>{output.name}</strong>
            <StatusPill>{output.outputType}</StatusPill>
          </div>
          <p className="of-text-muted" style={{ margin: '6px 0 0' }}>
            {output.final ? 'Final Logic function output' : output.intermediateParameter ? 'Intermediate evaluator parameter' : 'Intermediate output'} · source {output.sourceId} · Workshop {output.workshopUsage}
          </p>
          {output.intermediateParameter && previewValue !== undefined && (
            <pre style={{ margin: '8px 0 0', overflow: 'auto', fontSize: 12 }}>{JSON.stringify({ [output.apiName]: previewValue }, null, 2)}</pre>
          )}
        </div>
        );
      })}
      <div className="of-panel-muted" style={{ padding: 12 }}>
        <strong>Supported output families</strong>
        <p className="of-text-muted" style={{ margin: '6px 0 0' }}>Primitive values, objects, object lists/sets, structs, media references, and Ontology edit bundles where locally supported.</p>
      </div>
    </div>
  );
}


function TraceCard({ block, expanded, onToggle }: {
  block: LogicDebuggerBlockTrace;
  expanded: boolean;
  onToggle: () => void;
}) {
  return (
    <div className="of-panel-muted" style={{ padding: 10 }}>
      <button type="button" className="of-button" onClick={onToggle} style={{ width: '100%', justifyContent: 'space-between' }}>
        <span>{expanded ? '▾' : '▸'} {block.title}</span>
        <span>{block.status} · {block.durationMs} ms</span>
      </button>
      {expanded && (
        <div style={{ display: 'grid', gap: 8, marginTop: 8 }}>
          <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap' }}>
            <StatusPill tone={block.status === 'error' ? 'warning' : 'success'}>{block.status}</StatusPill>
            <StatusPill>{block.retention === 'local_session' ? 'local draft trace' : 'policy retained'}</StatusPill>
            <StatusPill tone="success">security filtered</StatusPill>
          </div>
          {block.prompt && (
            <div className="of-panel" style={{ padding: 8 }}>
              <p className="of-eyebrow">Prompt</p>
              <pre style={{ margin: 0, whiteSpace: 'pre-wrap', fontSize: 12 }}>{JSON.stringify(block.prompt, null, 2)}</pre>
            </div>
          )}
          <div className="of-panel" style={{ padding: 8 }}>
            <p className="of-eyebrow">Inputs / outputs</p>
            <pre style={{ margin: 0, overflow: 'auto', fontSize: 12 }}>{JSON.stringify({ inputs: block.inputs, outputs: block.outputs }, null, 2)}</pre>
          </div>
          <div className="of-panel" style={{ padding: 8 }}>
            <p className="of-eyebrow">Tool calls</p>
            <pre style={{ margin: 0, overflow: 'auto', fontSize: 12 }}>{JSON.stringify(block.toolCalls, null, 2)}</pre>
          </div>
          {block.errors.length > 0 && (
            <div className="of-status-warning" style={{ padding: 8, borderRadius: 4 }}>
              <strong>Errors</strong>
              <ul style={{ margin: '6px 0 0', paddingLeft: 18 }}>
                {block.errors.map((issue) => <li key={`${issue.field}-${issue.message}`}>{issue.message}</li>)}
              </ul>
            </div>
          )}
        </div>
      )}
    </div>
  );
}

function DebuggerPanel({ run, inputValues }: {
  run?: LogicPreviewRunResult;
  inputValues: Record<string, string>;
}) {
  const [expandedBlockIds, setExpandedBlockIds] = useState<Set<string>>(() => new Set(['input-binding', SAMPLE_LLM_BLOCK.id, 'final-output']));
  const [toolCallsCleared, setToolCallsCleared] = useState(false);
  const blocks = useMemo(() => buildDebuggerBlockTraces(run, inputValues, toolCallsCleared), [inputValues, run, toolCallsCleared]);

  function toggleBlock(id: string) {
    setExpandedBlockIds((current) => {
      const next = new Set(current);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  }

  return (
    <section className="of-panel" style={{ padding: 12, minHeight: 680 }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', gap: 8, alignItems: 'center' }}>
        <div>
          <div className="of-eyebrow">Debugger</div>
          <h2 className="of-heading-md" style={{ margin: 0 }}>Block trace</h2>
        </div>
        <div style={{ display: 'flex', gap: 6, alignItems: 'center' }}>
          <StatusPill tone={run?.status === 'failed' ? 'warning' : run ? 'success' : 'info'}>{run?.status ?? 'not run'}</StatusPill>
          <button type="button" className="of-button" onClick={() => setToolCallsCleared((current) => !current)} disabled={!run}>
            {toolCallsCleared ? 'Restore tool calls' : 'Clear tool calls'}
          </button>
        </div>
      </div>
      <p className="of-text-muted" style={{ margin: '8px 0 0' }}>
        Draft traces are security-filtered and retained locally for this session; published or automation runs would use platform retention policy.
      </p>
      <div style={{ display: 'grid', gap: 10, marginTop: 12 }}>
        {blocks.length === 0 ? (
          <div className="of-panel-muted" style={{ padding: 12 }}>
            Run the draft Logic function to open the debugger with inputs, prompts, tool calls, outputs, errors, and the final result.
          </div>
        ) : blocks.map((block) => (
          <TraceCard key={block.id} block={block} expanded={expandedBlockIds.has(block.id)} onToggle={() => toggleBlock(block.id)} />
        ))}
      </div>
    </section>
  );
}

function RunPanel({ inputs, llmBlock, inputValues, computeUsage, onInputChange, latestRun, recentRuns, onRun, onSelectRun, onAddAsTestCase }: {
  inputs: LogicInputDefinition[];
  llmBlock: LogicLlmBlockConfig;
  inputValues: Record<string, string>;
  computeUsage: LogicComputeUsageSummary;
  onInputChange: (apiName: string, value: string) => void;
  latestRun?: LogicPreviewRunResult;
  recentRuns: LogicPreviewRunResult[];
  onRun: () => void;
  onSelectRun: (run: LogicPreviewRunResult) => void;
  onAddAsTestCase: () => void;
}) {
  const canRun = validateLogicInputBoard(inputs).length === 0 && validateLlmBlock(llmBlock, inputs).filter((issue) => issue.severity === 'error').length === 0;
  return (
    <section className="of-panel" style={{ padding: 12, minHeight: 680 }}>
      <div className="of-eyebrow">Run panel</div>
      <h2 className="of-heading-md" style={{ margin: 0 }}>Draft preview execution</h2>
      <div style={{ display: 'grid', gap: 10, marginTop: 12 }}>
        {inputs.filter((input) => input.required || ['customerRecord', 'complaintText', 'baseRisk', 'delayHours'].includes(input.apiName)).map((input) => (
          <Field key={input.id} label={input.apiName}>
            {input.apiName === 'complaintText' ? (
              <textarea className="of-textarea" value={inputValues[input.apiName] ?? ''} onChange={(event) => onInputChange(input.apiName, event.target.value)} style={{ minHeight: 88 }} />
            ) : (
              <input className="of-input" value={inputValues[input.apiName] ?? ''} onChange={(event) => onInputChange(input.apiName, event.target.value)} />
            )}
          </Field>
        ))}
        <ComputeWarningList usage={computeUsage} />
        <ComputeSummaryStrip usage={computeUsage} />
        <div style={{ display: 'flex', gap: 8 }}>
          <button type="button" className="of-button of-button--primary" disabled={!canRun} onClick={onRun}>Run draft</button>
          <button type="button" className="of-button" disabled={!latestRun || !canRun} onClick={onRun}>Rerun latest</button>
          <button type="button" className="of-button" disabled={!latestRun} onClick={onAddAsTestCase}>Add as test case</button>
        </div>
        <div className="of-panel-muted" style={{ padding: 12 }}>
          <div style={{ display: 'flex', justifyContent: 'space-between', gap: 8 }}>
            <strong>Latest result</strong>
            <StatusPill tone={latestRun?.status === 'failed' ? 'warning' : latestRun ? 'success' : 'info'}>{latestRun?.status ?? 'idle'}</StatusPill>
          </div>
          <p className="of-text-muted" style={{ margin: '6px 0 0' }}>{latestRun?.result ?? 'No draft run yet. Edit inputs and run without publishing.'}</p>
          {latestRun && (
            <dl style={{ display: 'grid', gridTemplateColumns: 'auto 1fr', gap: '4px 8px', margin: '10px 0 0' }}>
              <dt className="of-text-muted">Duration</dt><dd style={{ margin: 0 }}>{latestRun.durationMs} ms</dd>
              <dt className="of-text-muted">Compute</dt><dd style={{ margin: 0 }}>{computeSecondsLabel(latestRun.metadata.computeUsage.totalComputeSeconds)}</dd>
              <dt className="of-text-muted">Run ID</dt><dd style={{ margin: 0 }}>{latestRun.id}</dd>
              <dt className="of-text-muted">Started</dt><dd style={{ margin: 0 }}>{latestRun.metadata.startedAtIso}</dd>
              <dt className="of-text-muted">Metadata</dt><dd style={{ margin: 0 }}>{latestRun.metadata.toolCallCount} tool calls · {latestRun.metadata.retainedUntil}</dd>
            </dl>
          )}
        </div>
        <div className="of-panel-muted" style={{ padding: 12 }}>
          <strong>Recent runs</strong>
          <div style={{ display: 'grid', gap: 6, marginTop: 8 }}>
            {recentRuns.length === 0 ? <p className="of-text-muted" style={{ margin: 0 }}>Runs from this draft session appear here.</p> : recentRuns.map((run) => (
              <button key={run.id} type="button" className="of-button" onClick={() => onSelectRun(run)} style={{ justifyContent: 'space-between' }}>
                <span>{run.id}</span>
                <span>{run.status} · {run.durationMs} ms</span>
              </button>
            ))}
          </div>
        </div>
      </div>
    </section>
  );
}

function VersionHistoryPanel({ versions, comparison, publishedFunctionRid, compareBaseId, compareHeadId, onCompareBaseChange, onCompareHeadChange }: {
  versions: LogicSavedVersion[];
  comparison?: LogicVersionComparison;
  publishedFunctionRid?: string;
  compareBaseId: string;
  compareHeadId: string;
  onCompareBaseChange: (id: string) => void;
  onCompareHeadChange: (id: string) => void;
}) {
  const blockChanges = comparison?.summary.blocks ?? [];
  const promptChanges = comparison?.summary.promptChanges ?? [];
  const modelChanges = comparison?.summary.modelChanges ?? [];
  return (
    <section className="of-panel" style={{ padding: 12 }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', gap: 10, alignItems: 'center' }}>
        <div>
          <div className="of-eyebrow">Version history</div>
          <h2 className="of-heading-md" style={{ margin: 0 }}>Save, publish, and compare</h2>
        </div>
        <StatusPill tone={publishedFunctionRid ? 'success' : 'info'}>
          {publishedFunctionRid ? `Callable ${publishedFunctionRid}` : 'No published function'}
        </StatusPill>
      </div>

      <div style={{ display: 'grid', gridTemplateColumns: 'minmax(260px, 0.7fr) minmax(420px, 1fr)', gap: 10, marginTop: 12 }}>
        <div className="of-panel-muted" style={{ padding: 10 }}>
          <div style={{ display: 'flex', justifyContent: 'space-between', gap: 8 }}>
            <strong>Saved versions</strong>
            <span className="of-text-muted">{versions.length} total</span>
          </div>
          <div style={{ display: 'grid', gap: 8, marginTop: 10 }}>
            {versions.map((version) => (
              <div key={version.id} className="of-panel" style={{ padding: 10 }}>
                <div style={{ display: 'flex', justifyContent: 'space-between', gap: 8 }}>
                  <strong>v{version.versionNumber}</strong>
                  <StatusPill tone={version.status === 'published' ? 'success' : version.status === 'superseded' ? 'warning' : 'info'}>{version.status}</StatusPill>
                </div>
                <p className="of-text-muted" style={{ margin: '6px 0 0' }}>
                  {version.author} · {new Date(version.createdAtIso).toLocaleString()}
                </p>
                <p className="of-text-muted" style={{ margin: '6px 0 0' }}>
                  {version.changeSummary.blocks.length} block changes · {version.changeSummary.inputs.length} input changes · {version.changeSummary.outputs.length} output changes
                </p>
              </div>
            ))}
          </div>
        </div>

        <div className="of-panel-muted" style={{ padding: 10 }}>
          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 8 }}>
            <Field label="Base version">
              <select className="of-select" value={compareBaseId} onChange={(event) => onCompareBaseChange(event.target.value)}>
                {versions.map((version) => <option key={version.id} value={version.id}>v{version.versionNumber} · {version.status}</option>)}
              </select>
            </Field>
            <Field label="Head version">
              <select className="of-select" value={compareHeadId} onChange={(event) => onCompareHeadChange(event.target.value)}>
                {versions.map((version) => <option key={version.id} value={version.id}>v{version.versionNumber} · {version.status}</option>)}
              </select>
            </Field>
          </div>

          <div style={{ display: 'grid', gridTemplateColumns: 'repeat(3, minmax(0, 1fr))', gap: 8, marginTop: 12 }}>
            {(['added', 'edited', 'removed'] as const).map((changeType) => (
              <div key={changeType} className="of-panel" style={{ padding: 10 }}>
                <p className="of-eyebrow">{changeType} blocks</p>
                <strong>{blockChanges.filter((change) => change.changeType === changeType).length}</strong>
                <div style={{ display: 'grid', gap: 4, marginTop: 8 }}>
                  {blockChanges.filter((change) => change.changeType === changeType).map((change) => (
                    <span key={`${changeType}-${change.id}`} className="of-chip">{change.name || change.id}</span>
                  ))}
                </div>
              </div>
            ))}
          </div>

          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 8, marginTop: 8 }}>
            <div className="of-panel" style={{ padding: 10 }}>
              <p className="of-eyebrow">Prompt changes</p>
              <strong>{promptChanges.length}</strong>
              <div style={{ display: 'grid', gap: 4, marginTop: 8 }}>
                {promptChanges.map((change) => <span key={`prompt-${change.blockId}`} className="of-chip">{change.blockName || change.blockId}</span>)}
              </div>
            </div>
            <div className="of-panel" style={{ padding: 10 }}>
              <p className="of-eyebrow">Model changes</p>
              <strong>{modelChanges.length}</strong>
              <div style={{ display: 'grid', gap: 4, marginTop: 8 }}>
                {modelChanges.map((change) => <span key={`model-${change.blockId}`} className="of-chip">{change.blockName || change.blockId}</span>)}
              </div>
            </div>
          </div>
        </div>
      </div>
    </section>
  );
}

function BranchingPanel({ resource, readiness, onAdd, onEdit, onPublish, onReview, onApprove, onRebase, onMerge, onRemove }: {
  resource?: LogicBranchAdapterResource;
  readiness?: LogicBranchMergeReadiness;
  onAdd: () => void;
  onEdit: () => void;
  onPublish: () => void;
  onReview: () => void;
  onApprove: () => void;
  onRebase: () => void;
  onMerge: () => void;
  onRemove: () => void;
}) {
  const availableOnBranch = resource ? logicBranchFunctionAvailable(resource, resource.branchId) : false;
  const availableOnMain = resource ? logicBranchFunctionAvailable(resource, 'main') : false;
  return (
    <section className="of-panel" style={{ padding: 12 }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', gap: 10, alignItems: 'center', flexWrap: 'wrap' }}>
        <div>
          <div className="of-eyebrow">Branching</div>
          <h2 className="of-heading-md" style={{ margin: 0 }}>Global Branch Logic adapter</h2>
        </div>
        <StatusPill tone={resource?.status === 'active' ? 'success' : resource ? 'warning' : 'info'}>
          {resource ? resource.status : 'not on branch'}
        </StatusPill>
      </div>

      {!resource ? (
        <div className="of-panel-muted" style={{ padding: 12, marginTop: 12, display: 'flex', justifyContent: 'space-between', gap: 10, alignItems: 'center' }}>
          <span className="of-text-muted">Add this Logic file to a Global Branch to save isolated drafts, publish a branched pre-release, review, rebase, and merge.</span>
          <button type="button" className="of-button of-button--primary" onClick={onAdd}>Add to branch</button>
        </div>
      ) : (
        <div style={{ display: 'grid', gap: 10, marginTop: 12 }}>
          <div className="of-panel-muted" style={{ padding: 10, display: 'grid', gridTemplateColumns: 'minmax(260px, 1fr) repeat(4, auto)', gap: 10, alignItems: 'center' }}>
            <div>
              <p className="of-eyebrow">{resource.branchName}</p>
              <strong>{resource.logicFileId}</strong>
              <p className="of-text-muted" style={{ margin: '4px 0 0' }}>{resource.resourceRid}</p>
            </div>
            <span className="of-chip">main base v{resource.mainBaseVersion.versionNumber}</span>
            <span className="of-chip">latest main v{resource.mainCurrentVersion.versionNumber}</span>
            <span className="of-chip">branch v{resource.branchVersion.versionNumber}</span>
            <span className="of-chip">{resource.publication?.tag ?? 'not published'}</span>
          </div>

          <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
            <button type="button" className="of-button" onClick={onEdit} disabled={resource.status !== 'active'}>Edit on branch</button>
            <button type="button" className="of-button" onClick={onPublish} disabled={resource.status !== 'active'}>Publish branch</button>
            <button type="button" className="of-button" onClick={onReview} disabled={resource.status !== 'active'}>Request review</button>
            <button type="button" className="of-button" onClick={onApprove} disabled={!resource.proposal || resource.pendingApprovalCount === 0}>Approve</button>
            <button type="button" className="of-button" onClick={onRebase}>Rebase</button>
            <button type="button" className="of-button of-button--primary" onClick={onMerge} disabled={!readiness?.mergeable}>Merge</button>
            <button type="button" className="of-button" onClick={onRemove} disabled={resource.status !== 'active'}>Remove from branch</button>
          </div>

          {resource.removalBlockedReason && (
            <div className="of-status-warning" style={{ padding: 10, borderRadius: 4 }}>{resource.removalBlockedReason}</div>
          )}

          <div style={{ display: 'grid', gridTemplateColumns: 'minmax(300px, 0.8fr) minmax(420px, 1.2fr)', gap: 10 }}>
            <div className="of-panel-muted" style={{ padding: 10, display: 'grid', gap: 8 }}>
              <p className="of-eyebrow">Isolation and usage</p>
              <span className="of-chip">{availableOnBranch ? 'callable on branch' : 'not callable on branch yet'}</span>
              <span className="of-chip">{availableOnMain ? 'visible on main' : 'hidden from main'}</span>
              <span className="of-chip">hidden from other branches</span>
              <span className="of-chip">{resource.publication?.functionRid ?? 'publish to create branched pre-release'}</span>
              <div className="of-panel" style={{ padding: 8 }}>
                <p className="of-eyebrow">Review flow</p>
                <div style={{ display: 'grid', gap: 4, marginTop: 6 }}>
                  {(resource.proposal?.reviews ?? []).length === 0 ? (
                    <span className="of-text-muted">No reviewer approvals requested.</span>
                  ) : resource.proposal?.reviews.map((review) => (
                    <span key={review.reviewerId} className="of-text-muted">{review.reviewerName} · {review.status}</span>
                  ))}
                </div>
              </div>
            </div>

            <div className="of-panel-muted" style={{ padding: 10, display: 'grid', gap: 8 }}>
              <div style={{ display: 'flex', justifyContent: 'space-between', gap: 8 }}>
                <p className="of-eyebrow">Merge requirements</p>
                <StatusPill tone={readiness?.mergeable ? 'success' : 'warning'}>{readiness?.mergeable ? 'mergeable' : 'blocked'}</StatusPill>
              </div>
              {(readiness?.checks ?? []).map((check) => (
                <div key={check.id} className="of-panel" style={{ padding: 8, display: 'grid', gridTemplateColumns: '1fr auto', gap: 8 }}>
                  <div>
                    <strong>{check.label}</strong>
                    <p className="of-text-muted" style={{ margin: '4px 0 0' }}>{check.message}</p>
                    {check.issues?.slice(0, 2).map((issue) => (
                      <p key={`${check.id}-${issue.field}-${issue.message}`} className="of-text-muted" style={{ margin: '4px 0 0', color: 'var(--status-warning)' }}>{issue.message}</p>
                    ))}
                  </div>
                  <StatusPill tone={check.status === 'passed' ? 'success' : 'warning'}>{check.status}</StatusPill>
                </div>
              ))}
              {resource.conflicts.length > 0 && (
                <div className="of-status-warning" style={{ padding: 8, borderRadius: 4 }}>
                  {resource.conflicts.length} rebase conflict{resource.conflicts.length === 1 ? '' : 's'} require split-screen manual resolution.
                </div>
              )}
            </div>
          </div>

          <div className="of-panel-muted" style={{ padding: 10 }}>
            <p className="of-eyebrow">Adapter operation log</p>
            <div style={{ display: 'grid', gap: 6, marginTop: 8 }}>
              {resource.operations.slice(-6).map((entry) => (
                <div key={`${entry.operation}-${entry.atIso}-${entry.detail}`} style={{ display: 'grid', gridTemplateColumns: '92px 1fr auto', gap: 8, fontSize: 12 }}>
                  <strong>{entry.operation}</strong>
                  <span className="of-text-muted">{entry.detail}</span>
                  <span className="of-text-muted">{new Date(entry.atIso).toLocaleTimeString()}</span>
                </div>
              ))}
            </div>
          </div>
        </div>
      )}
    </section>
  );
}

function UsesPanel({ usage, onPublish }: {
  usage: LogicUsageBundle;
  onPublish: () => void;
}) {
  return (
    <section className="of-panel" style={{ padding: 12 }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', gap: 10, alignItems: 'center' }}>
        <div>
          <div className="of-eyebrow">Uses</div>
          <h2 className="of-heading-md" style={{ margin: 0 }}>Published function surfaces</h2>
        </div>
        <StatusPill tone={usage.published ? (usage.returnsOntologyEdits ? 'warning' : 'success') : 'info'}>
          {usage.published ? `Callable ${usage.functionRid}` : 'Publish required'}
        </StatusPill>
      </div>

      {!usage.published && (
        <div className="of-panel-muted" style={{ padding: 10, marginTop: 12, display: 'flex', justifyContent: 'space-between', gap: 10, alignItems: 'center' }}>
          <span className="of-text-muted">A published version is required before Workshop, Logic, Automate, or API callers can bind this function.</span>
          <button className="of-button of-button--primary" type="button" onClick={onPublish}>Publish</button>
        </div>
      )}

      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(2, minmax(320px, 1fr))', gap: 10, marginTop: 12 }}>
        {usage.surfaces.map((surface) => (
          <div key={surface.id} className="of-panel-muted" style={{ padding: 10, display: 'grid', gap: 8 }}>
            <div style={{ display: 'flex', justifyContent: 'space-between', gap: 8, alignItems: 'center' }}>
              <div>
                <strong>{surface.label}</strong>
                <p className="of-text-muted" style={{ margin: '4px 0 0' }}>{surface.description}</p>
              </div>
              <StatusPill tone={surface.status === 'available' ? 'success' : surface.status === 'blocked' ? 'warning' : 'info'}>
                {surface.status.replace('_', ' ')}
              </StatusPill>
            </div>
            <div style={{ display: 'flex', flexWrap: 'wrap', gap: 6 }}>
              {surface.requirements.map((requirement) => <span key={requirement} className="of-chip">{requirement}</span>)}
            </div>
            {surface.blockedReason && (
              <div className="of-panel" style={{ padding: 8 }}>
                <span className="of-text-muted">{surface.blockedReason}</span>
              </div>
            )}
            {surface.snippet && (
              <div className="of-panel" style={{ padding: 8, display: 'grid', gap: 6 }}>
                <div style={{ display: 'flex', justifyContent: 'space-between', gap: 8 }}>
                  <span className="of-eyebrow">{surface.snippet.label}</span>
                  <span className="of-text-soft">{surface.snippet.language}</span>
                </div>
                <pre style={{ margin: 0, whiteSpace: 'pre-wrap', overflow: 'auto', fontSize: 12, lineHeight: 1.45 }}>{surface.snippet.body}</pre>
              </div>
            )}
            <a className="of-button" href={surface.href} style={{ justifyContent: 'space-between' }}>
              <span>
                {surface.status === 'blocked'
                  ? 'View limitation'
                  : surface.id === 'automate'
                    ? 'Create automation'
                    : surface.id === 'action_workflow'
                      ? 'Create action type'
                      : 'Open surface'}
              </span>
              <span className="of-text-soft">›</span>
            </a>
          </div>
        ))}
      </div>

      {usage.actionTypeDraft && (
        <div className="of-panel-muted" style={{ padding: 10, marginTop: 12, display: 'grid', gap: 10 }}>
          <div style={{ display: 'flex', justifyContent: 'space-between', gap: 10, alignItems: 'center', flexWrap: 'wrap' }}>
            <div>
              <p className="of-eyebrow">Logic-backed action</p>
              <strong>{usage.actionTypeDraft.displayName}</strong>
              <p className="of-text-muted" style={{ margin: '4px 0 0' }}>
                {usage.actionTypeDraft.functionRid} v{usage.actionTypeDraft.publishedVersionNumber}
                {usage.actionTypeDraft.ontologyEditOutputApiName ? ` / ${usage.actionTypeDraft.ontologyEditOutputApiName}` : ''}
              </p>
            </div>
            <a className="of-button of-button--primary" href={usage.actionTypeDraft.href}>Create action type</a>
          </div>
          <div style={{ display: 'grid', gridTemplateColumns: 'repeat(2, minmax(260px, 1fr))', gap: 8 }}>
            <div className="of-panel" style={{ padding: 8 }}>
              <p className="of-eyebrow">Workshop execution</p>
              <pre style={{ margin: '6px 0 0', whiteSpace: 'pre-wrap', fontSize: 12 }}>{JSON.stringify(usage.actionTypeDraft.workshopButton, null, 2)}</pre>
            </div>
            <div className="of-panel" style={{ padding: 8 }}>
              <p className="of-eyebrow">Branch-aware preview</p>
              <pre style={{ margin: '6px 0 0', whiteSpace: 'pre-wrap', fontSize: 12 }}>{JSON.stringify(usage.actionTypeDraft.branchPreview, null, 2)}</pre>
            </div>
          </div>
          <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap' }}>
            {usage.actionTypeDraft.guardrails.map((guardrail) => <span key={guardrail} className="of-chip">{guardrail}</span>)}
          </div>
        </div>
      )}
    </section>
  );
}

function AutomationsPanel({ draft, mode, onModeChange, onPublish }: {
  draft: ReturnType<typeof buildLogicAutomationDraft>;
  mode: LogicAutomationEditMode;
  onModeChange: (mode: LogicAutomationEditMode) => void;
  onPublish: () => void;
}) {
  const chart = draft ? buildLogicAutomationEventChart(draft, new Date('2026-05-13T12:00:00Z')) : [];
  const proposal = draft ? buildLogicAutomationProposal(draft, new Date('2026-05-13T12:00:00Z')) : undefined;
  const maxTriggered = Math.max(1, ...chart.map((bucket) => bucket.triggered));
  return (
    <section className="of-panel" style={{ padding: 12 }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', gap: 10, alignItems: 'center', flexWrap: 'wrap' }}>
        <div>
          <div className="of-eyebrow">Automations</div>
          <h2 className="of-heading-md" style={{ margin: 0 }}>Logic effects in Automate</h2>
        </div>
        <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
          <select className="of-select" value={mode} onChange={(event) => onModeChange(event.target.value as LogicAutomationEditMode)} style={{ width: 210 }}>
            <option value="stage_for_review">Stage proposals for review</option>
            <option value="auto_apply">Apply edits automatically</option>
          </select>
          <StatusPill tone={draft ? 'success' : 'warning'}>{draft ? draft.status : 'publish edit output'}</StatusPill>
        </div>
      </div>

      {!draft ? (
        <div className="of-panel-muted" style={{ padding: 12, marginTop: 12, display: 'flex', justifyContent: 'space-between', gap: 10, alignItems: 'center' }}>
          <span className="of-text-muted">Create Automation becomes available after publishing Logic with an Ontology edit output.</span>
          <button className="of-button of-button--primary" type="button" onClick={onPublish}>Publish</button>
        </div>
      ) : (
        <div style={{ display: 'grid', gap: 10, marginTop: 12 }}>
          <div className="of-panel-muted" style={{ padding: 10, display: 'grid', gridTemplateColumns: 'minmax(260px, 0.9fr) minmax(320px, 1.1fr) auto', gap: 10, alignItems: 'center' }}>
            <div>
              <p className="of-eyebrow">Pre-populated automation</p>
              <strong>{draft.name}</strong>
              <p className="of-text-muted" style={{ margin: '4px 0 0' }}>
                {draft.functionRid} v{draft.publishedVersionNumber} · {draft.ontologyEditOutputApiName}
              </p>
            </div>
            <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap' }}>
              <span className="of-chip">{draft.trigger.type.replaceAll('_', ' ')}</span>
              <span className="of-chip">{draft.actionTypeId}</span>
              <span className="of-chip">{draft.editMode.replaceAll('_', ' ')}</span>
            </div>
            <a className="of-button of-button--primary" href={draft.href}>Create in Automate</a>
          </div>

          <div style={{ display: 'grid', gridTemplateColumns: 'minmax(320px, 0.9fr) minmax(420px, 1.1fr)', gap: 10 }}>
            <div className="of-panel-muted" style={{ padding: 10 }}>
              <p className="of-eyebrow">Automation event chart</p>
              <div style={{ display: 'grid', gap: 8, marginTop: 10 }}>
                {chart.map((bucket) => (
                  <div key={bucket.label} style={{ display: 'grid', gridTemplateColumns: '48px 1fr auto', gap: 8, alignItems: 'center' }}>
                    <span className="of-text-muted">{bucket.label}</span>
                    <div style={{ height: 12, borderRadius: 4, background: 'var(--bg-panel)' }}>
                      <div style={{ width: `${Math.max(6, (bucket.triggered / maxTriggered) * 100)}%`, height: '100%', borderRadius: 4, background: 'var(--status-info)' }} />
                    </div>
                    <span className="of-text-muted">{bucket.triggered} events · {bucket.staged} staged · {bucket.applied} applied</span>
                  </div>
                ))}
              </div>
            </div>

            <div className="of-panel-muted" style={{ padding: 10, display: 'grid', gap: 10 }}>
              <div style={{ display: 'flex', justifyContent: 'space-between', gap: 8 }}>
                <div>
                  <p className="of-eyebrow">Agent proposal detail</p>
                  <strong>{proposal?.summary}</strong>
                </div>
                <StatusPill tone={proposal?.status === 'open' ? 'warning' : 'success'}>{proposal?.status}</StatusPill>
              </div>
              <pre style={{ margin: 0, overflow: 'auto', fontSize: 12, maxHeight: 170 }}>{JSON.stringify(proposal?.proposedActionPreview, null, 2)}</pre>
              <div className="of-panel" style={{ padding: 8 }}>
                <p className="of-eyebrow">Decision log handoff</p>
                <div style={{ display: 'grid', gap: 4, marginTop: 6 }}>
                  {proposal?.decisionLog.map((entry) => (
                    <span key={entry.id} className="of-text-muted">{entry.actor} · {entry.event} · {new Date(entry.atIso).toLocaleTimeString()}</span>
                  ))}
                </div>
              </div>
            </div>
          </div>
        </div>
      )}
    </section>
  );
}

function EvaluationsPanel({ suites, latestRun, runTargetVersions, computePlans, onRunTargetVersionChange, onCreate, onCreateFromPreview, onRunSuite, onRunTestCase }: {
  suites: LogicEvaluationSuitePreview[];
  latestRun?: LogicPreviewRunResult;
  runTargetVersions: Record<string, string>;
  computePlans: Record<string, LogicComputeUsageSummary>;
  onRunTargetVersionChange: (targetId: string, version: string) => void;
  onCreate: (source: LogicEvaluationSuiteSource) => void;
  onCreateFromPreview: () => void;
  onRunSuite: (suiteId: string) => void;
  onRunTestCase: (suiteId: string, testCaseId: string) => void;
}) {
  return (
    <section className="of-panel" style={{ padding: 12 }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', gap: 10, alignItems: 'center', flexWrap: 'wrap' }}>
        <div>
          <div className="of-eyebrow">Evaluations</div>
          <h2 className="of-heading-md" style={{ margin: 0 }}>AIP Evals suites</h2>
        </div>
        <StatusPill tone="info">{suites.length} suite{suites.length === 1 ? '' : 's'}</StatusPill>
      </div>

      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(4, minmax(180px, 1fr))', gap: 10, marginTop: 12 }}>
        <button type="button" className="of-panel-muted" onClick={onCreateFromPreview} disabled={!latestRun} style={{ padding: 10, textAlign: 'left', cursor: latestRun ? 'pointer' : 'not-allowed' }}>
          <strong>Add preview as test case</strong>
          <p className="of-text-muted" style={{ margin: '6px 0 0' }}>Create a suite from the latest Logic preview run.</p>
        </button>
        <button type="button" className="of-panel-muted" onClick={() => onCreate('evals_sidebar')} style={{ padding: 10, textAlign: 'left', cursor: 'pointer' }}>
          <strong>Set up tests manually</strong>
          <p className="of-text-muted" style={{ margin: '6px 0 0' }}>Start with target inputs, expected output columns, and no run rows.</p>
        </button>
        <button type="button" className="of-panel-muted" onClick={() => onCreate('aip_evals_app')} style={{ padding: 10, textAlign: 'left', cursor: 'pointer' }}>
          <strong>Generate evals</strong>
          <p className="of-text-muted" style={{ margin: '6px 0 0' }}>Bootstrap columns and an exact-match evaluator.</p>
        </button>
        <a className="of-panel-muted" href="/aip-evals?source=evals_sidebar&target=logic.customer-triage" style={{ padding: 10, textAlign: 'left', textDecoration: 'none', color: 'inherit' }}>
          <strong>Open AIP Evals app</strong>
          <p className="of-text-muted" style={{ margin: '6px 0 0' }}>Manage suite CRUD, permissions, and placement.</p>
        </a>
      </div>

      <div style={{ display: 'grid', gap: 8, marginTop: 12 }}>
        {suites.length === 0 ? (
          <p className="of-text-muted" style={{ margin: 0 }}>No evaluation suites have been created from this Logic function yet.</p>
        ) : suites.map((suite) => {
          const computePlan = computePlans[suite.id];
          const intermediateColumns = suite.testCaseColumns.filter((column) => column.role === 'intermediate_parameter');
          return (
          <div key={suite.id} className="of-panel-muted" style={{ padding: 10, display: 'grid', gap: 10 }}>
            <div style={{ display: 'grid', gridTemplateColumns: '1fr repeat(6, auto)', gap: 12, alignItems: 'center' }}>
              <div>
                <strong>{suite.name}</strong>
                <p className="of-text-muted" style={{ margin: '4px 0 0' }}>{suite.source.replaceAll('_', ' ')} · {suite.targetFunctions[0]?.functionRid ?? 'no target'}</p>
              </div>
              <span className="of-chip">{suite.testCaseColumns.length} columns</span>
              <span className="of-chip">{suite.testCases.length} cases</span>
              <span className="of-chip">{suite.evaluators.length} evaluators</span>
              <span className="of-chip">{intermediateColumns.length} intermediates</span>
              <span className="of-chip">{suite.runHistory.length} runs</span>
              <span className="of-chip">{suite.resultsDatasetRid}</span>
            </div>
            {intermediateColumns.length > 0 && (
              <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap' }}>
                {intermediateColumns.map((column) => <span key={column.id} className="of-chip">{column.apiName} · {column.type}</span>)}
              </div>
            )}
            {computePlan && (
              <>
                <ComputeWarningList usage={computePlan} />
                <div className="of-panel" style={{ padding: 8, display: 'flex', justifyContent: 'space-between', gap: 8, flexWrap: 'wrap' }}>
                  <span className="of-text-muted">Estimated suite run usage</span>
                  <strong>{computeSecondsLabel(computePlan.totalComputeSeconds)}</strong>
                  <span className="of-text-muted">{computePlan.runCount} target invocation{computePlan.runCount === 1 ? '' : 's'} · {computeSecondsLabel(computePlan.evaluatorComputeSeconds)} evaluator work</span>
                </div>
              </>
            )}
            <div style={{ display: 'grid', gridTemplateColumns: 'minmax(220px, 1fr) auto', gap: 8, alignItems: 'end' }}>
              <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(190px, 1fr))', gap: 8 }}>
                {suite.targetFunctions.map((target) => (
                  <Field key={`${suite.id}-${target.id}`} label={`${target.id} version`}>
                    <select className="of-select" value={runTargetVersions[target.id] ?? defaultEvaluationTargetVersion(target)} onChange={(event) => onRunTargetVersionChange(target.id, event.target.value)}>
                      {evaluationTargetVersionOptions(target).map((option) => <option key={option.value} value={option.value}>{option.label}</option>)}
                    </select>
                  </Field>
                ))}
              </div>
              <button type="button" className="of-button of-button--primary" onClick={() => onRunSuite(suite.id)}>Run suite</button>
            </div>
            {suite.runHistory[0] ? (
              <div className="of-panel" style={{ padding: 10, display: 'grid', gap: 8 }}>
                <div style={{ display: 'flex', justifyContent: 'space-between', gap: 8, alignItems: 'center' }}>
                  <strong>Latest run</strong>
                  <StatusPill tone={suite.runHistory[0].passed ? 'success' : 'warning'}>
                    {Math.round(suite.runHistory[0].passRate * 100)}% pass
                  </StatusPill>
                </div>
                <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(180px, 1fr))', gap: 8 }}>
                  {suite.runHistory[0].testCaseResults.map((result) => (
                    <div key={`${suite.runHistory[0].id}-${result.testCaseId}`} className="of-panel-muted" style={{ padding: 8, display: 'grid', gap: 6 }}>
                      <strong>{result.name}</strong>
                      <span className="of-text-muted" style={{ fontSize: 12 }}>{result.passed ? 'passed' : 'failed'} · {result.iterations.length} target iteration{result.iterations.length === 1 ? '' : 's'}</span>
                      <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap' }}>
                        <button type="button" className="of-button" onClick={() => onRunTestCase(suite.id, result.testCaseId)}>Run case</button>
                        {suite.runHistory[0].debuggerLinks.filter((link) => link.testCaseId === result.testCaseId).slice(0, 1).map((link) => (
                          <a key={link.href} className="of-button" href={link.href}>Debugger</a>
                        ))}
                      </div>
                    </div>
                  ))}
                </div>
                {suite.runHistory[0].errors.map((error) => (
                  <span key={error.message} className="of-status-warning" style={{ padding: 8, borderRadius: 4, fontSize: 12 }}>{error.message}</span>
                ))}
                {suite.runHistory[0].resultDatasetRows[0] && (
                  <div className="of-panel-muted" style={{ padding: 8 }}>
                    <p className="of-eyebrow">Results dataset row</p>
                    <pre style={{ margin: 0, overflow: 'auto', fontSize: 12 }}>{JSON.stringify({
                      outputs: suite.runHistory[0].resultDatasetRows[0].outputs,
                      intermediateParameters: suite.runHistory[0].resultDatasetRows[0].intermediateParameters,
                      ontologySimulation: suite.runHistory[0].resultDatasetRows[0].ontologySimulation ? {
                        id: suite.runHistory[0].resultDatasetRows[0].ontologySimulation.id,
                        realOntologyMutated: suite.runHistory[0].resultDatasetRows[0].ontologySimulation.realOntologyMutated,
                        created: suite.runHistory[0].resultDatasetRows[0].ontologySimulation.createdObjects.length,
                        edited: suite.runHistory[0].resultDatasetRows[0].ontologySimulation.editedObjects.length,
                        deleted: suite.runHistory[0].resultDatasetRows[0].ontologySimulation.deletedObjects.length,
                      } : undefined,
                    }, null, 2)}</pre>
                  </div>
                )}
              </div>
            ) : (
              <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap' }}>
                {suite.testCases.map((testCase) => (
                  <button key={testCase.id} type="button" className="of-button" onClick={() => onRunTestCase(suite.id, testCase.id)}>
                    Run {testCase.name}
                  </button>
                ))}
              </div>
            )}
          </div>
          );
        })}
      </div>
    </section>
  );
}

function ExecutionSettingsPanel({ mode, datasetConfig, datasetRows, onModeChange, onDatasetRidChange, onMaxRowsChange }: {
  mode: LogicPermissionExecutionMode;
  datasetConfig: LogicRunHistoryDatasetConfig;
  datasetRows: LogicRunHistoryDatasetRow[];
  onModeChange: (mode: LogicPermissionExecutionMode) => void;
  onDatasetRidChange: (rid: string) => void;
  onMaxRowsChange: (maxRows: number) => void;
}) {
  const policy = logicExecutionModePolicy(mode);
  return (
    <section className="of-panel" style={{ padding: 12 }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', gap: 10, alignItems: 'center' }}>
        <div>
          <div className="of-eyebrow">Execution settings</div>
          <h2 className="of-heading-md" style={{ margin: 0 }}>{mode === 'project_scoped' ? 'Project-scoped execution' : 'User-scoped execution'}</h2>
        </div>
        <StatusPill tone={mode === 'project_scoped' ? 'success' : 'info'}>{policy.retainedUntilLabel}</StatusPill>
      </div>
      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(4, minmax(180px, 1fr))', gap: 10, marginTop: 12 }}>
        <div className="of-panel-muted" style={{ padding: 10 }}>
          <Field label="Mode">
            <select className="of-select" value={mode} onChange={(event) => onModeChange(event.target.value as LogicPermissionExecutionMode)}>
              <option value="user_scoped">User scoped</option>
              <option value="project_scoped">Project scoped</option>
            </select>
          </Field>
        </div>
        <div className="of-panel-muted" style={{ padding: 10 }}>
          <p className="of-eyebrow">Effective permissions</p>
          <strong>{policy.permissionSubject.replace('_', ' ')}</strong>
          <p className="of-text-muted" style={{ margin: '6px 0 0' }}>
            {mode === 'project_scoped' ? `Project ${LOGIC_PROJECT_ID}` : `${CURRENT_LOGIC_ACTOR.name}`}
          </p>
        </div>
        <div className="of-panel-muted" style={{ padding: 10 }}>
          <p className="of-eyebrow">Log visibility</p>
          <strong>{policy.logVisibility.replace('_', ' ')}</strong>
          <p className="of-text-muted" style={{ margin: '6px 0 0' }}>{mode === 'project_scoped' ? 'Shared with project viewers.' : 'Private to the initiating user.'}</p>
        </div>
        <div className="of-panel-muted" style={{ padding: 10 }}>
          <p className="of-eyebrow">Preservation</p>
          <strong>{mode === 'project_scoped' ? `${datasetConfig.maxRows.toLocaleString()} rows` : `${policy.retentionHours} hours`}</strong>
          <p className="of-text-muted" style={{ margin: '6px 0 0' }}>{mode === 'project_scoped' ? 'Append rows, prune oldest.' : 'Expire by timestamp.'}</p>
        </div>
      </div>

      <div className="of-panel-muted" style={{ padding: 10, display: 'grid', gap: 10, marginTop: 12 }}>
        <div style={{ display: 'grid', gridTemplateColumns: 'minmax(300px, 1fr) 160px auto', gap: 10, alignItems: 'end' }}>
          <Field label="Run history dataset RID">
            <input className="of-input" value={datasetConfig.datasetRid} onChange={(event) => onDatasetRidChange(event.target.value)} disabled={mode !== 'project_scoped'} />
          </Field>
          <Field label="Max rows">
            <input className="of-input" type="number" min={1} max={10000} value={datasetConfig.maxRows} onChange={(event) => onMaxRowsChange(Number(event.target.value))} disabled={mode !== 'project_scoped'} />
          </Field>
          <StatusPill tone={mode === 'project_scoped' ? 'success' : 'info'}>
            {mode === 'project_scoped' ? `${datasetRows.length} visible row${datasetRows.length === 1 ? '' : 's'}` : 'inactive'}
          </StatusPill>
        </div>
        <div style={{ display: 'grid', gridTemplateColumns: 'minmax(220px, 0.8fr) minmax(360px, 1.2fr)', gap: 10 }}>
          <div className="of-panel" style={{ padding: 10 }}>
            <p className="of-eyebrow">Dataset schema</p>
            <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap', marginTop: 8 }}>
              {datasetConfig.schema.map((column) => (
                <span key={column.name} className="of-chip">{column.name}{column.permissionScoped ? ' · scoped' : ''}</span>
              ))}
            </div>
          </div>
          <div className="of-panel" style={{ padding: 10 }}>
            <p className="of-eyebrow">Latest dataset row</p>
            {mode === 'project_scoped' && datasetRows[0] ? (
              <pre style={{ margin: '8px 0 0', maxHeight: 180, overflow: 'auto', fontSize: 12 }}>
                {JSON.stringify(datasetRows[0], null, 2)}
              </pre>
            ) : (
              <p className="of-text-muted" style={{ margin: '8px 0 0' }}>Project-scoped runs will appear here after execution.</p>
            )}
          </div>
        </div>
      </div>
    </section>
  );
}

function RunHistoryPanel({ runs, hiddenCount, datasetRows }: {
  runs: LogicRunHistoryRecord[];
  hiddenCount: number;
  datasetRows: LogicRunHistoryDatasetRow[];
}) {
  return (
    <section className="of-panel" style={{ padding: 12 }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', gap: 10, alignItems: 'center' }}>
        <div>
          <div className="of-eyebrow">Run history</div>
          <h2 className="of-heading-md" style={{ margin: 0 }}>Visible execution logs</h2>
        </div>
        <StatusPill tone={hiddenCount === 0 ? 'success' : 'info'}>{hiddenCount} peer log{hiddenCount === 1 ? '' : 's'} hidden</StatusPill>
      </div>
      {datasetRows.length > 0 && (
        <div className="of-panel-muted" style={{ padding: 10, marginTop: 12, display: 'grid', gridTemplateColumns: '1fr auto auto', gap: 10, alignItems: 'center' }}>
          <div>
            <p className="of-eyebrow">Project run history dataset</p>
            <strong>{datasetRows[0].datasetRid}</strong>
          </div>
          <span className="of-chip">{datasetRows.length} retained row{datasetRows.length === 1 ? '' : 's'}</span>
          <span className="of-chip">{datasetRows[0].visibleTo.replace('_', ' ')}</span>
        </div>
      )}
      <div style={{ display: 'grid', gap: 8, marginTop: 12 }}>
        {runs.length === 0 ? (
          <p className="of-text-muted" style={{ margin: 0 }}>No retained logs are visible for {CURRENT_LOGIC_ACTOR.name} yet.</p>
        ) : runs.map((run) => (
          <div key={run.id} className="of-panel-muted" style={{ padding: 10, display: 'grid', gridTemplateColumns: '1fr repeat(5, auto)', gap: 12, alignItems: 'center' }}>
            <div>
              <strong>{run.id}</strong>
              <p className="of-text-muted" style={{ margin: '4px 0 0' }}>
                {run.actorName} · {run.invocationSurface}{run.runHistoryDatasetRid ? ` · ${run.runHistoryDatasetRid}` : ''}
              </p>
            </div>
            <StatusPill tone={run.status === 'failed' ? 'warning' : 'success'}>{run.status}</StatusPill>
            <span className="of-text-muted">{run.executionMode.replace('_', ' ')}</span>
            <span className="of-text-muted">{run.model ?? 'model'} · {run.branchName ?? 'main'}</span>
            <span className="of-text-muted">{run.computeUsage ? computeSecondsLabel(run.computeUsage.totalComputeSeconds) : 'unmetered'}</span>
            <span className="of-text-muted">expires {new Date(run.retentionExpiresAtIso).toLocaleString()}</span>
          </div>
        ))}
      </div>
    </section>
  );
}

function metricValueLabel(metric: LogicMetricsSummary['operationalHealth']['metrics'][number]) {
  if (metric.id === 'token_compute_usage') return `${computeSecondsLabel(metric.value)} · ${metric.unit}`;
  if (metric.unit === 'percent') return `${metric.value}%`;
  if (metric.unit === 'milliseconds') return `${metric.value} ms`;
  return `${metric.value} ${metric.unit.replace('_', ' ')}`;
}

function MetricsPanel({ metrics, window, onWindowChange }: {
  metrics: LogicMetricsSummary;
  window: LogicMetricsWindow;
  onWindowChange: (window: LogicMetricsWindow) => void;
}) {
  const healthTone = metrics.operationalHealth.status === 'healthy' ? 'success' : 'warning';
  return (
    <section className="of-panel" style={{ padding: 12 }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', gap: 10, alignItems: 'center', flexWrap: 'wrap' }}>
        <div>
          <div className="of-eyebrow">Metrics</div>
          <h2 className="of-heading-md" style={{ margin: 0 }}>Logic operational health</h2>
          <p className="of-text-muted" style={{ margin: '4px 0 0' }}>
            Near-real-time style health across failure rate, P95 duration, token/compute usage, tool calls, model availability, dataset writes, and automation backlog.
          </p>
        </div>
        <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
          <StatusPill tone={healthTone}>{metrics.operationalHealth.status}</StatusPill>
          <StatusPill tone="info">viewer permission required</StatusPill>
          <select className="of-input" value={window} onChange={(event) => onWindowChange(event.target.value as LogicMetricsWindow)} style={{ width: 120 }}>
            {(['24h', '7d', '30d', '90d'] as LogicMetricsWindow[]).map((entry) => (
              <option key={entry} value={entry}>{entry}</option>
            ))}
          </select>
        </div>
      </div>
      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(4, minmax(160px, 1fr))', gap: 10, marginTop: 12 }}>
        <div className="of-panel-muted" style={{ padding: 10 }}>
          <p className="of-eyebrow">Failure rate</p>
          <strong style={{ fontSize: 22 }}>{metrics.failureRate}%</strong>
          <p className="of-text-muted" style={{ margin: '6px 0 0' }}>{metrics.failureCount} failed / {metrics.successCount + metrics.failureCount} completed.</p>
        </div>
        <div className="of-panel-muted" style={{ padding: 10 }}>
          <p className="of-eyebrow">P95 duration</p>
          <strong style={{ fontSize: 22 }}>{metrics.p95DurationMs === null ? '—' : `${metrics.p95DurationMs} ms`}</strong>
        </div>
        <div className="of-panel-muted" style={{ padding: 10 }}>
          <p className="of-eyebrow">Compute usage</p>
          <strong style={{ fontSize: 22 }}>{computeSecondsLabel(metrics.totalComputeSeconds)}</strong>
          <p className="of-text-muted" style={{ margin: '6px 0 0' }}>{metrics.totalPromptTokensEstimate} estimated prompt tokens.</p>
        </div>
        <div className="of-panel-muted" style={{ padding: 10 }}>
          <p className="of-eyebrow">Automation backlog</p>
          <strong style={{ fontSize: 22 }}>{metrics.automationProposalBacklog}</strong>
          <p className="of-text-muted" style={{ margin: '6px 0 0' }}>Open staged proposals.</p>
        </div>
      </div>

      <div style={{ display: 'grid', gridTemplateColumns: 'minmax(420px, 1.2fr) minmax(280px, 0.8fr)', gap: 10, marginTop: 12 }}>
        <div className="of-panel-muted" style={{ padding: 10, display: 'grid', gap: 8 }}>
          <div style={{ display: 'flex', justifyContent: 'space-between', gap: 8 }}>
            <p className="of-eyebrow">Health checks</p>
            <span className="of-text-muted">{metrics.recentRuns.length} recent visible run{metrics.recentRuns.length === 1 ? '' : 's'}</span>
          </div>
          {metrics.operationalHealth.metrics.map((metric) => (
            <div key={metric.id} style={{ display: 'grid', gridTemplateColumns: '1fr auto auto', gap: 8, alignItems: 'center', fontSize: 12 }}>
              <span>{metric.label}</span>
              <strong>{metricValueLabel(metric)}</strong>
              <StatusPill tone={metric.status === 'healthy' ? 'success' : 'warning'}>{metric.status}</StatusPill>
            </div>
          ))}
        </div>
        <div className="of-panel-muted" style={{ padding: 10, display: 'grid', gap: 8 }}>
          <p className="of-eyebrow">Surface in</p>
          {metrics.operationalHealth.surfaces.map((surface) => (
            <div key={surface.id} className="of-panel" style={{ padding: 8, display: 'grid', gap: 4 }}>
              <div style={{ display: 'flex', justifyContent: 'space-between', gap: 8 }}>
                <strong>{surface.label}</strong>
                <StatusPill tone={surface.visible ? 'success' : 'warning'}>{surface.visible ? 'visible' : 'hidden'}</StatusPill>
              </div>
              <span className="of-text-muted" style={{ fontSize: 12 }}>{surface.href}</span>
            </div>
          ))}
        </div>
      </div>

      <div style={{ display: 'grid', gridTemplateColumns: 'minmax(240px, 0.8fr) minmax(360px, 1.2fr)', gap: 10, marginTop: 12 }}>
        <div className="of-panel-muted" style={{ padding: 10 }}>
          <p className="of-eyebrow">Failure categories</p>
          <div style={{ display: 'grid', gap: 6, marginTop: 8 }}>
            {metrics.failureCategories.length === 0 ? (
              <p className="of-text-muted" style={{ margin: 0 }}>No failures in this window.</p>
            ) : metrics.failureCategories.map((category) => (
              <div key={category.category} style={{ display: 'flex', justifyContent: 'space-between', gap: 8 }}>
                <span>{category.category.replaceAll('_', ' ')}</span>
                <strong>{category.count}</strong>
              </div>
            ))}
          </div>
        </div>
        <div className="of-panel-muted" style={{ padding: 10 }}>
          <p className="of-eyebrow">Recent run history</p>
          <div style={{ display: 'grid', gap: 6, marginTop: 8 }}>
            {metrics.recentRuns.length === 0 ? (
              <p className="of-text-muted" style={{ margin: 0 }}>Run the draft or invoke a published function to populate metrics.</p>
            ) : metrics.recentRuns.map((run) => (
              <div key={run.id} style={{ display: 'grid', gridTemplateColumns: '1fr auto auto', gap: 10, alignItems: 'center', fontSize: 12 }}>
                <span style={{ overflowWrap: 'anywhere' }}>{run.id}</span>
                <StatusPill tone={run.status === 'failed' ? 'warning' : 'success'}>{run.status}</StatusPill>
                <span className="of-text-muted">{run.durationMs} ms</span>
              </div>
            ))}
          </div>
        </div>
      </div>
      <p className="of-text-muted" style={{ margin: '10px 0 0', fontSize: 12 }}>
        Window: {new Date(metrics.windowStartIso).toLocaleString()} to {new Date(metrics.windowEndIso).toLocaleString()}.
      </p>
    </section>
  );
}
function ComputeUsagePanel({ draftUsage, evaluationPlans, latestRun, runs }: {
  draftUsage: LogicComputeUsageSummary;
  evaluationPlans: Record<string, LogicComputeUsageSummary>;
  latestRun?: LogicPreviewRunResult;
  runs: LogicRunHistoryRecord[];
}) {
  const evaluationPlanValues = Object.values(evaluationPlans);
  const evaluationTotal = evaluationPlanValues.reduce((sum, usage) => sum + usage.totalComputeSeconds, 0);
  const latestActual = latestRun?.metadata.computeUsage;
  const attribution = draftUsage.attribution;
  return (
    <section className="of-panel" style={{ padding: 12 }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', gap: 10, alignItems: 'center', flexWrap: 'wrap' }}>
        <div>
          <div className="of-eyebrow">Compute usage</div>
          <h2 className="of-heading-md" style={{ margin: 0 }}>Logic metering preview</h2>
        </div>
        <StatusPill tone={draftUsage.warnings.length || evaluationPlanValues.some((usage) => usage.warnings.length) ? 'warning' : 'success'}>
          {draftUsage.warnings.length || evaluationPlanValues.some((usage) => usage.warnings.length) ? 'warnings available' : 'within warning thresholds'}
        </StatusPill>
      </div>

      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(3, minmax(220px, 1fr))', gap: 10, marginTop: 12 }}>
        <div className="of-panel-muted" style={{ padding: 10 }}>
          <p className="of-eyebrow">Draft run estimate</p>
          <strong style={{ fontSize: 22 }}>{computeSecondsLabel(draftUsage.totalComputeSeconds)}</strong>
          <p className="of-text-muted" style={{ margin: '6px 0 0' }}>{draftUsage.runCount} run · {draftUsage.lineItems.length} metered line items</p>
        </div>
        <div className="of-panel-muted" style={{ padding: 10 }}>
          <p className="of-eyebrow">Latest run actual</p>
          <strong style={{ fontSize: 22 }}>{latestActual ? computeSecondsLabel(latestActual.totalComputeSeconds) : 'No run'}</strong>
          <p className="of-text-muted" style={{ margin: '6px 0 0' }}>{latestRun?.id ?? 'Execute draft preview to record usage.'}</p>
        </div>
        <div className="of-panel-muted" style={{ padding: 10 }}>
          <p className="of-eyebrow">Queued Evals plans</p>
          <strong style={{ fontSize: 22 }}>{computeSecondsLabel(evaluationTotal)}</strong>
          <p className="of-text-muted" style={{ margin: '6px 0 0' }}>{evaluationPlanValues.length} suite configuration{evaluationPlanValues.length === 1 ? '' : 's'}</p>
        </div>
      </div>

      <div style={{ display: 'grid', gridTemplateColumns: 'minmax(260px, 0.7fr) minmax(420px, 1.3fr)', gap: 10, marginTop: 12 }}>
        <div className="of-panel-muted" style={{ padding: 10, display: 'grid', gap: 10 }}>
          <p className="of-eyebrow">Attribution</p>
          <span className="of-chip">file {attribution.logicFileId ?? DEFAULT_LOGIC_FUNCTION_RID}</span>
          <span className="of-chip">version {attribution.logicVersionId ?? 'draft'}</span>
          <span className="of-chip">surface {attribution.invocationSurface}</span>
          <span className="of-chip">user {attribution.actorId ?? CURRENT_LOGIC_ACTOR.id}</span>
          <span className="of-chip">project {attribution.projectId ?? LOGIC_PROJECT_ID}</span>
          {runs[0]?.computeUsage ? <span className="of-chip">last logged {computeSecondsLabel(runs[0].computeUsage.totalComputeSeconds)}</span> : null}
        </div>
        <div className="of-panel-muted" style={{ padding: 10, display: 'grid', gap: 8 }}>
          <div style={{ display: 'flex', justifyContent: 'space-between', gap: 8 }}>
            <p className="of-eyebrow">Line items</p>
            <span className="of-text-muted">{computeSecondsLabel(draftUsage.downstreamComputeSeconds)} downstream systems</span>
          </div>
          <div style={{ display: 'grid', gap: 6 }}>
            {draftUsage.lineItems.map((item) => (
              <div key={item.id} style={{ display: 'grid', gridTemplateColumns: '1fr auto auto', gap: 8, alignItems: 'center', fontSize: 12 }}>
                <span>
                  <strong>{item.label}</strong>
                  <span className="of-text-muted"> · {item.category.replaceAll('_', ' ')}</span>
                </span>
                <span className="of-text-muted">{item.blockName ?? item.blockId ?? 'suite'}</span>
                <strong>{computeSecondsLabel(item.computeSeconds)}</strong>
              </div>
            ))}
          </div>
          <ComputeWarningList usage={draftUsage} />
        </div>
      </div>
    </section>
  );
}

function SecurityPanel({ policy, boundary, decisions }: {
  policy: LogicFileSecurityPolicy;
  boundary: LogicSecurityBoundary;
  decisions: LogicPermissionDecision[];
}) {
  const ownerLabel = policy.ownerIds.join(', ') || 'none';
  const editorLabel = policy.editorIds.join(', ') || 'owners only';
  const managerLabel = policy.managerIds.join(', ') || 'owners only';
  const invokerLabel = policy.invokerIds.join(', ') || 'owners/editors only';
  const viewerLabel = policy.viewerIds.join(', ') || 'owners/editors only';
  const actorName = (actorId: string) => {
    if (actorId === CURRENT_LOGIC_ACTOR.id) return CURRENT_LOGIC_ACTOR.name;
    if (actorId === OTHER_LOGIC_ACTOR.id) return OTHER_LOGIC_ACTOR.name;
    return actorId;
  };

  return (
    <section className="of-panel" style={{ padding: 12 }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', gap: 10, alignItems: 'center', flexWrap: 'wrap' }}>
        <div>
          <div className="of-eyebrow">Security</div>
          <h2 className="of-heading-md" style={{ margin: 0 }}>Logic permissions and resource boundary</h2>
        </div>
        <StatusPill tone={boundary.ready ? 'success' : 'warning'}>
          {boundary.ready ? 'ready' : `${boundary.issues.length} issue${boundary.issues.length === 1 ? '' : 's'}`}
        </StatusPill>
      </div>

      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(4, minmax(190px, 1fr))', gap: 10, marginTop: 12 }}>
        <div className="of-panel-muted" style={{ padding: 10 }}>
          <p className="of-eyebrow">View</p>
          <strong>{viewerLabel}</strong>
          <p className="of-text-muted" style={{ margin: '6px 0 0' }}>Owners, managers, editors, and viewers.</p>
        </div>
        <div className="of-panel-muted" style={{ padding: 10 }}>
          <p className="of-eyebrow">Edit</p>
          <strong>{editorLabel}</strong>
          <p className="of-text-muted" style={{ margin: '6px 0 0' }}>Owners, managers, and editors.</p>
        </div>
        <div className="of-panel-muted" style={{ padding: 10 }}>
          <p className="of-eyebrow">Manage</p>
          <strong>{managerLabel}</strong>
          <p className="of-text-muted" style={{ margin: '6px 0 0' }}>Owners and managers.</p>
        </div>
        <div className="of-panel-muted" style={{ padding: 10 }}>
          <p className="of-eyebrow">Invoke</p>
          <strong>{invokerLabel}</strong>
          <p className="of-text-muted" style={{ margin: '6px 0 0' }}>Published function execution.</p>
        </div>
      </div>

      <div style={{ display: 'grid', gridTemplateColumns: 'minmax(260px, 0.8fr) minmax(420px, 1.2fr)', gap: 10, marginTop: 12 }}>
        <div className="of-panel-muted" style={{ padding: 10, display: 'grid', gap: 8 }}>
          <div style={{ display: 'flex', justifyContent: 'space-between', gap: 8 }}>
            <p className="of-eyebrow">Permission checks</p>
            <span className="of-text-muted">owner {ownerLabel}</span>
          </div>
          {decisions.map((decision) => (
            <div key={`${decision.actorId}-${decision.action}`} style={{ display: 'grid', gridTemplateColumns: '1fr auto', gap: 8, alignItems: 'center', fontSize: 12 }}>
              <span>
                <strong>{actorName(decision.actorId)}</strong>
                <span className="of-text-muted"> · {decision.action}</span>
              </span>
              <StatusPill tone={decision.allowed ? 'success' : 'warning'}>{decision.allowed ? 'allowed' : 'blocked'}</StatusPill>
              <span className="of-text-muted" style={{ gridColumn: '1 / -1' }}>{decision.reason}</span>
            </div>
          ))}
        </div>

        <div className="of-panel-muted" style={{ padding: 10, display: 'grid', gap: 8 }}>
          <div style={{ display: 'flex', justifyContent: 'space-between', gap: 8 }}>
            <p className="of-eyebrow">LLM-accessible resources</p>
            <span className="of-text-muted">{boundary.llmAccessibleResourceIds.length} resource{boundary.llmAccessibleResourceIds.length === 1 ? '' : 's'}</span>
          </div>
          <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap' }}>
            {boundary.llmAccessibleResourceIds.map((resourceId) => (
              <span key={resourceId} className="of-chip">{resourceId}</span>
            ))}
          </div>
          <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(180px, 1fr))', gap: 8 }}>
            <span className="of-chip">subject {boundary.permissionSubject}</span>
            <span className="of-chip">id {boundary.permissionSubjectId}</span>
            <span className="of-chip">mode {boundary.executionMode}</span>
          </div>
        </div>
      </div>

      <div className="of-panel-muted" style={{ padding: 10, display: 'grid', gap: 8, marginTop: 12 }}>
        <div style={{ display: 'flex', justifyContent: 'space-between', gap: 8, alignItems: 'center' }}>
          <p className="of-eyebrow">Resource boundary</p>
          <span className="of-text-muted">{boundary.resources.length} configured resource{boundary.resources.length === 1 ? '' : 's'}</span>
        </div>
        <div style={{ display: 'grid', gap: 6 }}>
          {boundary.resources.map((resource) => (
            <div key={`${resource.kind}-${resource.id}-${resource.source}`} className="of-panel" style={{ padding: 8, display: 'grid', gridTemplateColumns: 'minmax(220px, 1fr) minmax(200px, 1fr) auto', gap: 8, alignItems: 'center' }}>
              <div>
                <strong>{resource.kind.replaceAll('_', ' ')}</strong>
                <div className="of-text-muted" style={{ fontSize: 12, overflowWrap: 'anywhere' }}>{resource.id}</div>
              </div>
              <div>
                <span className="of-text-muted">{resource.source}</span>
                {resource.properties.length > 0 && (
                  <div style={{ display: 'flex', gap: 4, flexWrap: 'wrap', marginTop: 4 }}>
                    {resource.properties.map((property) => <span key={property} className="of-chip">{property}</span>)}
                  </div>
                )}
              </div>
              <div style={{ display: 'flex', gap: 4, flexWrap: 'wrap', justifyContent: 'flex-end' }}>
                <StatusPill tone={resource.explicitlyConfigured ? 'success' : 'warning'}>explicit</StatusPill>
                <StatusPill tone={resource.permissioned ? 'success' : 'warning'}>permissioned</StatusPill>
                <StatusPill tone={resource.importedIntoProject ? 'success' : 'warning'}>imported</StatusPill>
                <StatusPill tone={resource.markingAccess ? 'success' : 'warning'}>markings</StatusPill>
                {resource.llmAccessible && <StatusPill tone="info">LLM</StatusPill>}
              </div>
            </div>
          ))}
        </div>
      </div>


      <div className="of-panel-muted" style={{ padding: 10, display: 'grid', gap: 8, marginTop: 12 }}>
        <div style={{ display: 'flex', justifyContent: 'space-between', gap: 8, alignItems: 'center' }}>
          <p className="of-eyebrow">Data minimization guardrails</p>
          <span className="of-text-muted">{boundary.minimizationWarnings.length} warning{boundary.minimizationWarnings.length === 1 ? '' : 's'}</span>
        </div>
        <div style={{ display: 'grid', gridTemplateColumns: 'minmax(280px, 1fr) minmax(280px, 1fr)', gap: 8 }}>
          <div className="of-panel" style={{ padding: 8, display: 'grid', gap: 6 }}>
            <strong>Exposure inventory</strong>
            <span className="of-text-muted">{boundary.exposureInventory.prompts.length} prompt block{boundary.exposureInventory.prompts.length === 1 ? '' : 's'} · {boundary.exposureInventory.objectTypes.length} object type{boundary.exposureInventory.objectTypes.length === 1 ? '' : 's'} · {boundary.exposureInventory.actions.length} action{boundary.exposureInventory.actions.length === 1 ? '' : 's'} · {boundary.exposureInventory.functions.length} function{boundary.exposureInventory.functions.length === 1 ? '' : 's'} · {boundary.exposureInventory.mediaReferences.length} media reference{boundary.exposureInventory.mediaReferences.length === 1 ? '' : 's'}</span>
            {boundary.exposureInventory.prompts.map((prompt) => (
              <div key={prompt.blockId} style={{ fontSize: 12 }}>
                <strong>{prompt.blockName}</strong>
                <span className="of-text-muted"> exposes variables {prompt.variableRefs.join(', ') || 'none'} through {prompt.modelBinding.mode}</span>
              </div>
            ))}
          </div>
          <div className="of-panel" style={{ padding: 8, display: 'grid', gap: 6 }}>
            <strong>Governance hooks</strong>
            {boundary.guardrailHooks.map((hook) => (
              <div key={hook.id} style={{ display: 'grid', gridTemplateColumns: '1fr auto', gap: 8, fontSize: 12 }}>
                <span>{hook.label}<span className="of-text-muted"> · {hook.detail}</span></span>
                <StatusPill tone={hook.enabled ? 'success' : 'warning'}>{hook.enabled ? 'enabled' : 'not set'}</StatusPill>
              </div>
            ))}
          </div>
        </div>
        {boundary.minimizationWarnings.length > 0 && (
          <div style={{ display: 'grid', gap: 6 }}>
            {boundary.minimizationWarnings.map((warning) => (
              <div key={`${warning.field}-${warning.message}`} className="of-status-warning" style={{ padding: 8, borderRadius: 4, fontSize: 12 }}>
                <strong>{warning.field}</strong>: {warning.message}
                {warning.properties?.length ? <span> Sensitive properties: {warning.properties.join(', ')}.</span> : null}
              </div>
            ))}
          </div>
        )}
      </div>

      {boundary.issues.length > 0 && (
        <div className="of-panel" style={{ padding: 10, display: 'grid', gap: 6, marginTop: 12, borderColor: 'var(--status-warning)' }}>
          <strong>Security issues</strong>
          {boundary.issues.map((issue) => (
            <div key={`${issue.field}-${issue.message}`} className="of-status-warning" style={{ padding: 8, borderRadius: 4, fontSize: 12 }}>
              <strong>{issue.field}</strong>: {issue.message}
            </div>
          ))}
        </div>
      )}
    </section>
  );
}

export function LogicAuthoringPage() {
  const [activeTab, setActiveTab] = useState<ConfigTab>('inputs');
  const [activeRail, setActiveRail] = useState<ResourceRailEntry>('Version history');
  const [selectedInputId, setSelectedInputId] = useState(SAMPLE_INPUTS[0].id);
  const [inputs, setInputs] = useState<LogicInputDefinition[]>(SAMPLE_INPUTS);
  const [llmBlock, setLlmBlock] = useState<LogicLlmBlockConfig>(SAMPLE_LLM_BLOCK);
  const [selectedIntermediateSourceIds, setSelectedIntermediateSourceIds] = useState<string[]>(DEFAULT_INTERMEDIATE_PARAMETER_SOURCE_IDS);
  const [runInputValues, setRunInputValues] = useState<Record<string, string>>(DEFAULT_RUN_INPUTS);
  const [latestRun, setLatestRun] = useState<LogicPreviewRunResult>();
  const [recentRuns, setRecentRuns] = useState<LogicPreviewRunResult[]>([]);
  const [versionHistory, setVersionHistory] = useState<LogicSavedVersion[]>(() => [createInitialVersion()]);
  const [compareBaseId, setCompareBaseId] = useState('logic-version-demo-7');
  const [compareHeadId, setCompareHeadId] = useState('logic-version-demo-7');
  const [publishedFunctionRid, setPublishedFunctionRid] = useState<string>();
  const [executionMode, setExecutionMode] = useState<LogicPermissionExecutionMode>('user_scoped');
  const [runHistoryDatasetRid, setRunHistoryDatasetRid] = useState(DEFAULT_RUN_HISTORY_DATASET_RID);
  const [runHistoryMaxRows, setRunHistoryMaxRows] = useState(10000);
  const [metricsWindow, setMetricsWindow] = useState<LogicMetricsWindow>('30d');
  const [automationMode, setAutomationMode] = useState<LogicAutomationEditMode>('stage_for_review');
  const [evaluationSuites, setEvaluationSuites] = useState<LogicEvaluationSuitePreview[]>([]);
  const [evalRunTargetVersions, setEvalRunTargetVersions] = useState<Record<string, string>>({});
  const [branchResource, setBranchResource] = useState<LogicBranchAdapterResource>();
  const boardIssues = useMemo(() => validateLogicInputBoard(inputs), [inputs]);
  const currentDefinition = useMemo(() => buildLogicDefinition(inputs, llmBlock, selectedIntermediateSourceIds), [inputs, llmBlock, selectedIntermediateSourceIds]);
  const latestVersionNumber = versionHistory[0]?.versionNumber ?? 0;
  const latestSavedVersionId = versionHistory[0]?.id;
  const publishedVersion = useMemo(() => versionHistory.find((version) => version.status === 'published'), [versionHistory]);
  const draftComputeUsage = useMemo(() => estimateLogicComputeUsage({
    llmBlocks: [llmBlock],
    attribution: {
      logicFileId: DEFAULT_LOGIC_FUNCTION_RID,
      logicVersionId: latestSavedVersionId,
      publishedVersionNumber: publishedVersion?.versionNumber,
      actorId: CURRENT_LOGIC_ACTOR.id,
      projectId: LOGIC_PROJECT_ID,
      permissionSubjectId: executionMode === 'project_scoped' ? LOGIC_PROJECT_ID : CURRENT_LOGIC_ACTOR.id,
      invocationSurface: 'draft_preview',
    },
  }), [executionMode, latestSavedVersionId, llmBlock, publishedVersion?.versionNumber]);
  const runHistoryDatasetConfig = useMemo(() => createLogicRunHistoryDatasetConfig(LOGIC_PROJECT_ID, {
    datasetRid: runHistoryDatasetRid,
    maxRows: runHistoryMaxRows,
  }), [runHistoryDatasetRid, runHistoryMaxRows]);
  const usageBundle = useMemo(() => buildLogicUsageSurfaces({
    functionRid: publishedFunctionRid,
    publishedVersion,
    definition: publishedVersion?.definition ?? currentDefinition,
    baseUrl: typeof window === 'undefined' ? 'http://localhost:8080' : window.location.origin,
  }), [currentDefinition, publishedFunctionRid, publishedVersion]);
  const automationDraft = useMemo(() => buildLogicAutomationDraft({
    functionRid: publishedFunctionRid,
    publishedVersion,
    definition: publishedVersion?.definition ?? currentDefinition,
    mode: automationMode,
  }), [automationMode, currentDefinition, publishedFunctionRid, publishedVersion]);
  const allRunHistory = useMemo(() => {
    const records = recentRuns.map((run) => runHistoryRecord(run, CURRENT_LOGIC_ACTOR, {
      executionMode,
      datasetConfig: runHistoryDatasetConfig,
      inputValues: runInputValues,
      publishedVersion,
    }));
    if (records.length > 0) records.push(hiddenPeerRun(new Date(records[0].startedAtIso), executionMode, runHistoryDatasetConfig, publishedVersion));
    return records;
  }, [executionMode, publishedVersion, recentRuns, runHistoryDatasetConfig, runInputValues]);
  const visibleRunHistory = useMemo(() => filterLogicRunsForViewer(allRunHistory, CURRENT_LOGIC_ACTOR.id), [allRunHistory]);
  const hiddenRunCount = allRunHistory.length - visibleRunHistory.length;
  const runHistoryDatasetRows = useMemo(() => {
    if (executionMode !== 'project_scoped') return [];
    const rows = visibleRunHistory
      .filter((run) => run.executionMode === 'project_scoped')
      .map((run) => buildLogicRunHistoryDatasetRow(run, runHistoryDatasetConfig, {
        functionRid: publishedFunctionRid ?? DEFAULT_LOGIC_FUNCTION_RID,
        branchName: 'main',
        publishedVersionId: publishedVersion?.id,
        publishedVersionNumber: publishedVersion?.versionNumber,
      }));
    return limitLogicRunHistoryDatasetRows(rows, runHistoryDatasetConfig.maxRows);
  }, [executionMode, publishedFunctionRid, publishedVersion, runHistoryDatasetConfig, visibleRunHistory]);
  const logicMetrics = useMemo(() => calculateLogicMetrics(visibleRunHistory, metricsWindow, new Date(), {
    automationProposalBacklog: automationDraft && automationDraft.editMode === 'stage_for_review' ? 1 : 0,
  }), [automationDraft, metricsWindow, visibleRunHistory]);
  const logicSecurityBoundary = useMemo(() => buildLogicSecurityBoundary({
    definition: currentDefinition,
    policy: LOGIC_SECURITY_POLICY,
    executionMode,
    permissionSubjectId: executionMode === 'project_scoped' ? LOGIC_PROJECT_ID : CURRENT_LOGIC_ACTOR.id,
    llmBlocks: [llmBlock],
    resultDatasetRid: runHistoryDatasetConfig.datasetRid,
  }), [currentDefinition, executionMode, llmBlock, runHistoryDatasetConfig.datasetRid]);
  const logicPermissionDecisions = useMemo(() => [
    logicFilePermissionDecision(LOGIC_SECURITY_POLICY, CURRENT_LOGIC_ACTOR.id, 'view'),
    logicFilePermissionDecision(LOGIC_SECURITY_POLICY, CURRENT_LOGIC_ACTOR.id, 'edit'),
    logicFilePermissionDecision(LOGIC_SECURITY_POLICY, CURRENT_LOGIC_ACTOR.id, 'manage'),
    logicFilePermissionDecision(LOGIC_SECURITY_POLICY, CURRENT_LOGIC_ACTOR.id, 'invoke'),
    logicFilePermissionDecision(LOGIC_SECURITY_POLICY, OTHER_LOGIC_ACTOR.id, 'view'),
    logicFilePermissionDecision(LOGIC_SECURITY_POLICY, OTHER_LOGIC_ACTOR.id, 'invoke'),
  ], []);
  const branchReadiness = useMemo(() => branchResource ? getLogicBranchMergeReadiness(branchResource) : undefined, [branchResource]);
  const evaluationComputePlans = useMemo(() => Object.fromEntries(evaluationSuites.map((suite) => [
    suite.id,
    estimateLogicEvaluationComputeUsage({
      llmBlocks: [llmBlock],
      targetCount: suite.targetFunctions.length,
      testCaseCount: suite.testCases.length,
      evaluatorCount: suite.evaluators.length,
      attribution: {
        logicFileId: DEFAULT_LOGIC_FUNCTION_RID,
        logicVersionId: publishedVersion?.id ?? latestSavedVersionId,
        publishedVersionNumber: publishedVersion?.versionNumber,
        actorId: CURRENT_LOGIC_ACTOR.id,
        projectId: LOGIC_PROJECT_ID,
        permissionSubjectId: executionMode === 'project_scoped' ? LOGIC_PROJECT_ID : CURRENT_LOGIC_ACTOR.id,
        invocationSurface: 'eval_run',
        evalRunId: suite.id,
      },
    }),
  ])), [evaluationSuites, executionMode, latestSavedVersionId, llmBlock, publishedVersion]);
  const comparison = useMemo(() => {
    const base = versionHistory.find((version) => version.id === compareBaseId);
    const head = versionHistory.find((version) => version.id === compareHeadId);
    if (!base || !head) return undefined;
    return compareLogicSavedVersions(base, head);
  }, [compareBaseId, compareHeadId, versionHistory]);

  function updateInput(next: LogicInputDefinition) {
    setInputs((current) => current.map((input) => (input.id === next.id ? next : input)));
  }

  function updateRunInput(apiName: string, value: string) {
    setRunInputValues((current) => ({ ...current, [apiName]: value }));
  }

  function toggleIntermediateSource(sourceId: string) {
    setSelectedIntermediateSourceIds((current) => (
      current.includes(sourceId)
        ? current.filter((candidate) => candidate !== sourceId)
        : [...current, sourceId]
    ));
  }

  function runDraftPreview() {
    const run = executeDraftLogicPreview(llmBlock, inputs, runInputValues);
    setLatestRun(run);
    setRecentRuns((current) => [run, ...current.filter((candidate) => candidate.id !== run.id)].slice(0, 5));
  }

  function createEvaluationSuite(source: LogicEvaluationSuiteSource) {
    const suite = buildLogicEvaluationSuitePreview(source, currentDefinition, source === 'logic_preview' ? latestRun : undefined, publishedFunctionRid, runInputValues);
    setEvaluationSuites((current) => [suite, ...current]);
    setActiveRail('Evaluations');
  }

  function updateEvaluationRunVersion(targetId: string, version: string) {
    setEvalRunTargetVersions((current) => ({ ...current, [targetId]: version }));
  }

  function runEvaluationSuite(suiteId: string, testCaseId?: string) {
    setEvaluationSuites((current) => current.map((suite) => {
      if (suite.id !== suiteId) return suite;
      const targetVersions = Object.fromEntries(suite.targetFunctions.map((target) => [
        target.id,
        evalRunTargetVersions[target.id] ?? defaultEvaluationTargetVersion(target),
      ]));
      const run = runEvaluationSuiteBuiltIns(suite, {
        source: 'logic_sidebar',
        targetVersions,
        testCaseIds: testCaseId ? [testCaseId] : undefined,
      });
      return { ...suite, runHistory: [run, ...suite.runHistory].slice(0, 8) };
    }));
    setActiveRail('Evaluations');
  }

  function saveDraftVersion() {
    const base = versionHistory[0]?.definition ?? EMPTY_LOGIC_DEFINITION;
    const next = createLogicSavedVersion(base, currentDefinition, 'Casey Author', new Date(), latestVersionNumber + 1);
    setVersionHistory((current) => [next, ...current]);
    setCompareBaseId(versionHistory[0]?.id ?? next.id);
    setCompareHeadId(next.id);
    setActiveRail('Version history');
  }

  function publishCurrentVersion() {
    const base = versionHistory[0]?.definition ?? EMPTY_LOGIC_DEFINITION;
    const publishableBlock: LogicLlmBlockConfig = {
      ...llmBlock,
      toolAccess: llmBlock.toolAccess.map((tool) => (tool.kind === 'apply_action' ? { ...tool, logicPublished: true } : tool)),
    };
    const publishableDefinition = buildLogicDefinition(inputs, publishableBlock);
    const next = createLogicSavedVersion(base, publishableDefinition, 'Casey Author', new Date(), latestVersionNumber + 1);
    const published = publishLogicSavedVersion([next, ...versionHistory], next.id, new Date());
    setVersionHistory(published);
    setCompareBaseId(versionHistory[0]?.id ?? next.id);
    setCompareHeadId(next.id);
    setPublishedFunctionRid('logic.customer-triage');
    setActiveRail('Uses');
    setLlmBlock(publishableBlock);
  }

  function baseBranchResource() {
    return branchResource ?? addLogicFileToBranch({
      branchId: 'gb-customer-triage-review',
      branchName: 'Customer triage review',
      logicFileId: DEFAULT_LOGIC_FUNCTION_RID,
      mainVersion: versionHistory[0] ?? createInitialVersion(),
      actor: CURRENT_LOGIC_ACTOR.name,
      now: new Date(),
    });
  }

  function addToBranch() {
    setBranchResource(baseBranchResource());
    setActiveRail('Branching');
  }

  function editOnBranch() {
    const resource = baseBranchResource();
    const nextDefinition: LogicVersionDefinition = {
      ...currentDefinition,
      blocks: currentDefinition.blocks.map((candidate, index) => index === 0 ? {
        ...candidate,
        taskPrompt: `${String(candidate.taskPrompt ?? '')}\nBranch scenario: prefer proactive account-owner handoff before merge.`,
      } : candidate),
    };
    setBranchResource(editLogicFileOnBranch(resource, nextDefinition, CURRENT_LOGIC_ACTOR.name, new Date()));
    setActiveRail('Branching');
  }

  function publishBranchVersion() {
    setBranchResource(publishLogicVersionOnBranch(baseBranchResource(), CURRENT_LOGIC_ACTOR.name, new Date()));
    setActiveRail('Branching');
  }

  function requestBranchReview() {
    setBranchResource(requestLogicBranchReview(baseBranchResource(), [{
      reviewerId: OTHER_LOGIC_ACTOR.id,
      reviewerName: OTHER_LOGIC_ACTOR.name,
    }], CURRENT_LOGIC_ACTOR.name, new Date()));
    setActiveRail('Branching');
  }

  function approveBranchReview() {
    if (!branchResource) return;
    setBranchResource(reviewLogicBranchProposal(branchResource, {
      reviewerId: OTHER_LOGIC_ACTOR.id,
      reviewerName: OTHER_LOGIC_ACTOR.name,
      status: 'approved',
      comment: 'Looks ready for main after merge checks pass.',
    }, new Date()));
    setActiveRail('Branching');
  }

  function rebaseBranchVersion() {
    if (!branchResource) return;
    setBranchResource(rebaseLogicFileOnBranch(branchResource, versionHistory[0] ?? branchResource.mainCurrentVersion, CURRENT_LOGIC_ACTOR.name, new Date(), {
      acceptManualResolution: true,
      notes: 'Manual split-screen review completed',
    }));
    setActiveRail('Branching');
  }

  function mergeBranchVersion() {
    if (!branchResource) return;
    const now = new Date();
    const result = mergeLogicFileBranch(branchResource, CURRENT_LOGIC_ACTOR.name, now);
    setBranchResource(result.resource);
    const mergedMainVersion = result.mergedMainVersion;
    if (mergedMainVersion) {
      setVersionHistory((current) => publishLogicSavedVersion([mergedMainVersion, ...current], mergedMainVersion.id, now));
      setCompareBaseId(branchResource.mainCurrentVersion.id);
      setCompareHeadId(mergedMainVersion.id);
      setPublishedFunctionRid(DEFAULT_LOGIC_FUNCTION_RID);
    }
    setActiveRail('Branching');
  }

  function removeBranchVersion() {
    if (!branchResource) return;
    setBranchResource(removeLogicFileFromBranch(branchResource, CURRENT_LOGIC_ACTOR.name, new Date()));
    setActiveRail('Branching');
  }

  return (
    <section className="of-page" style={{ display: 'grid', gap: 10 }}>
      <div className="of-toolbar" style={{ justifyContent: 'space-between' }}>
        <div>
          <div className="of-eyebrow">AIP Logic</div>
          <h1 className="of-heading-lg" style={{ margin: 0 }}>Customer triage logic</h1>
          <div className="of-text-muted">Project: Customer operations / Folder: AIP demos / Draft v{latestVersionNumber + 1}</div>
        </div>
        <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
          <StatusPill tone={boardIssues.length === 0 ? 'success' : 'warning'}>{boardIssues.length === 0 ? 'ready to run' : `${boardIssues.length} input issues`}</StatusPill>
          <button className="of-button" type="button" onClick={saveDraftVersion}>Save draft</button>
          <button className="of-button of-button--primary" type="button" onClick={publishCurrentVersion}>Publish</button>
        </div>
      </div>

      <div style={{ display: 'grid', gridTemplateColumns: 'minmax(520px, 1.4fr) minmax(320px, 0.9fr) minmax(300px, 0.8fr) 170px', gap: 10, alignItems: 'stretch' }}>
        <section className="of-panel" style={{ padding: 12, minHeight: 680 }}>
          <div style={{ display: 'flex', gap: 6, borderBottom: '1px solid var(--border-subtle)', paddingBottom: 8, marginBottom: 10 }}>
            {(['inputs', 'blocks', 'outputs'] as const).map((tab) => (
              <button key={tab} type="button" className={`of-tab ${activeTab === tab ? 'of-tab-active' : ''}`} onClick={() => setActiveTab(tab)}>
                {tab[0].toUpperCase() + tab.slice(1)}
              </button>
            ))}
          </div>
          {activeTab === 'inputs' && <InputsBoard inputs={inputs} selectedId={selectedInputId} onSelect={setSelectedInputId} onChange={updateInput} />}
          {activeTab === 'blocks' && <BlocksBoard inputs={inputs} llmBlock={llmBlock} onChange={setLlmBlock} />}
          {activeTab === 'outputs' && (
            <OutputsBoard
              outputs={currentDefinition.outputs}
              selectedIntermediateSourceIds={selectedIntermediateSourceIds}
              latestRun={latestRun}
              onToggleIntermediateSource={toggleIntermediateSource}
            />
          )}
        </section>

        <DebuggerPanel run={latestRun} inputValues={runInputValues} />

        <RunPanel
          inputs={inputs}
          llmBlock={llmBlock}
          inputValues={runInputValues}
          computeUsage={draftComputeUsage}
          onInputChange={updateRunInput}
          latestRun={latestRun}
          recentRuns={recentRuns}
          onRun={runDraftPreview}
          onSelectRun={setLatestRun}
          onAddAsTestCase={() => createEvaluationSuite('logic_preview')}
        />

        <aside className="of-panel" style={{ padding: 8, minHeight: 680 }} aria-label="Logic resource entry points">
          <div className="of-eyebrow" style={{ padding: '6px 8px' }}>Resource</div>
          <nav style={{ display: 'grid', gap: 6 }}>
            {RIGHT_RAIL.map((entry) => (
              <button
                key={entry}
                type="button"
                onClick={() => setActiveRail(entry)}
                className="of-button"
                style={{ justifyContent: 'space-between', minHeight: 34, background: activeRail === entry ? 'var(--status-info-bg)' : entry === 'Execution settings' || entry === 'Security' ? 'var(--bg-panel-muted)' : 'var(--bg-panel)' }}
              >
                <span>{entry}</span>
                <span className="of-text-soft">›</span>
              </button>
            ))}
          </nav>
        </aside>
      </div>

      {activeRail === 'Version history' && (
        <VersionHistoryPanel
          versions={versionHistory}
          comparison={comparison}
          publishedFunctionRid={publishedFunctionRid}
          compareBaseId={compareBaseId}
          compareHeadId={compareHeadId}
          onCompareBaseChange={setCompareBaseId}
          onCompareHeadChange={setCompareHeadId}
        />
      )}
      {activeRail === 'Branching' && (
        <BranchingPanel
          resource={branchResource}
          readiness={branchReadiness}
          onAdd={addToBranch}
          onEdit={editOnBranch}
          onPublish={publishBranchVersion}
          onReview={requestBranchReview}
          onApprove={approveBranchReview}
          onRebase={rebaseBranchVersion}
          onMerge={mergeBranchVersion}
          onRemove={removeBranchVersion}
        />
      )}
      {activeRail === 'Uses' && (
        <UsesPanel
          usage={usageBundle}
          onPublish={publishCurrentVersion}
        />
      )}
      {activeRail === 'Automations' && (
        <AutomationsPanel
          draft={automationDraft}
          mode={automationMode}
          onModeChange={setAutomationMode}
          onPublish={publishCurrentVersion}
        />
      )}
      {activeRail === 'Evaluations' && (
        <EvaluationsPanel
          suites={evaluationSuites}
          latestRun={latestRun}
          runTargetVersions={evalRunTargetVersions}
          computePlans={evaluationComputePlans}
          onRunTargetVersionChange={updateEvaluationRunVersion}
          onCreate={createEvaluationSuite}
          onCreateFromPreview={() => createEvaluationSuite('logic_preview')}
          onRunSuite={(suiteId) => runEvaluationSuite(suiteId)}
          onRunTestCase={(suiteId, testCaseId) => runEvaluationSuite(suiteId, testCaseId)}
        />
      )}
      {activeRail === 'Run history' && (
        <RunHistoryPanel
          runs={visibleRunHistory}
          hiddenCount={hiddenRunCount}
          datasetRows={runHistoryDatasetRows}
        />
      )}
      {activeRail === 'Metrics' && (
        <MetricsPanel
          metrics={logicMetrics}
          window={metricsWindow}
          onWindowChange={setMetricsWindow}
        />
      )}
      {activeRail === 'Compute' && (
        <ComputeUsagePanel
          draftUsage={draftComputeUsage}
          evaluationPlans={evaluationComputePlans}
          latestRun={latestRun}
          runs={visibleRunHistory}
        />
      )}
      {activeRail === 'Execution settings' && (
        <ExecutionSettingsPanel
          mode={executionMode}
          datasetConfig={runHistoryDatasetConfig}
          datasetRows={runHistoryDatasetRows}
          onModeChange={setExecutionMode}
          onDatasetRidChange={setRunHistoryDatasetRid}
          onMaxRowsChange={setRunHistoryMaxRows}
        />
      )}
      {activeRail === 'Security' && (
        <SecurityPanel
          policy={LOGIC_SECURITY_POLICY}
          boundary={logicSecurityBoundary}
          decisions={logicPermissionDecisions}
        />
      )}
    </section>
  );
}
