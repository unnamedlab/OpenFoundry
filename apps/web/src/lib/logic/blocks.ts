import { isValidLogicInputApiName, validateLogicInputDefinition, type LogicInputDefinition, type LogicInputType } from './inputs';

export type LogicStructuredOutputKind = 'text' | 'json_schema' | 'object' | 'object_list' | 'ontology_edit_bundle';
export type LogicToolKind = 'query_objects' | 'apply_action' | 'execute_function' | 'calculator';
export type LogicSeverity = 'error' | 'warning';
export type LogicFunctionKind = 'typescript' | 'python' | 'existing_logic' | 'function_on_objects';
export type LogicValueType = LogicInputType | 'json' | 'ontology_edit_bundle';

export interface LogicIssue {
  severity: LogicSeverity;
  field: string;
  message: string;
}

export interface LogicStructuredOutputConfig {
  kind: LogicStructuredOutputKind;
  schemaJson?: string;
}

export interface LogicQueryObjectsToolConfig {
  kind: 'query_objects';
  name: string;
  objectTypeId: string;
  selectedProperties: string[];
  readableObjectTypeIds: string[];
  readablePropertiesByObjectType: Record<string, string[]>;
  maxObjects: number;
}

export interface LogicActionToolConfig {
  kind: 'apply_action';
  name: string;
  actionTypeId: string;
  allowedActionTypeIds: string[];
  expectedParameters: Record<string, LogicValueType>;
  parameterMappings: Record<string, string>;
  invocationMode: 'preview' | 'commit';
  invocationSurface: 'draft_preview' | 'published_action' | 'automation';
  logicPublished: boolean;
}

export interface LogicExecuteFunctionToolConfig {
  kind: 'execute_function';
  name: string;
  functionRid: string;
  functionKind: LogicFunctionKind;
  allowedFunctionRids: string[];
  signature: {
    parameters: Record<string, LogicValueType>;
    returnType: LogicValueType;
  };
  parameterMappings: Record<string, string>;
  expectedOutputType: LogicValueType;
}

export interface LogicCalculatorToolConfig {
  kind: 'calculator';
  name: string;
  expression: string;
  parameterRefs: string[];
  outputType: 'integer' | 'double';
}

export type LogicToolConfig =
  | LogicQueryObjectsToolConfig
  | LogicActionToolConfig
  | LogicExecuteFunctionToolConfig
  | LogicCalculatorToolConfig;

export interface LogicLlmBlockConfig {
  id: string;
  name: string;
  modelBinding: {
    mode: 'fixed' | 'model_variable';
    providerId?: string;
    modelVariableApiName?: string;
  };
  systemPrompt: string;
  taskPrompt: string;
  promptVariableRefs: string[];
  toolAccess: LogicToolConfig[];
  structuredOutput: LogicStructuredOutputConfig;
  maxOutputTokens: number;
}

export interface LogicDebuggerTraceMetadata {
  blockId: string;
  renderedPrompt: {
    system: string;
    task: string;
    variables: string[];
  };
  toolCalls: Array<{
    toolName: string;
    kind: LogicToolKind;
    objectTypeId?: string;
    actionTypeId?: string;
    functionRid?: string;
    expression?: string;
    selectedProperties?: string[];
    parameterMappings?: Record<string, string>;
    maxObjects?: number;
    status: 'not_run' | 'preview_only' | 'ok' | 'error';
  }>;
  proposedOntologyEdits: Array<{
    actionTypeId: string;
    parameters: Record<string, string>;
    applyState: 'preview_only' | 'ready_for_commit';
  }>;
  output: {
    structuredOutputKind: LogicStructuredOutputKind;
    preview: string;
  };
  tokenUsage: {
    promptTokensEstimate: number;
    maxOutputTokens: number;
    computeUnitsEstimate: number;
  };
  computeUsage: LogicComputeUsageSummary;
  errors: LogicIssue[];
}

const TOKEN_CHARS = 4;
const PROPERTY_WARNING_THRESHOLD = 12;
const TOOL_OBJECT_WARNING_THRESHOLD = 3;
const CALCULATOR_EXPRESSION_PATTERN = /^[\d\s+\-*/()._%A-Za-z]+$/;
const LOGIC_LLM_BLOCK_COMPUTE_SECONDS = 4;
const LOGIC_LLM_TOOL_COMPUTE_SECONDS = 8;
const LOGIC_EVALUATOR_COMPUTE_SECONDS = 1;
const LOGIC_EXPENSIVE_RUN_WARNING_SECONDS = 100;
const LOGIC_EXPENSIVE_EVAL_WARNING_SECONDS = 250;
const LOGIC_PROMPT_TOKEN_WARNING_THRESHOLD = 8_000;

export type LogicComputeUsageCategory =
  | 'llm_block_execution'
  | 'llm_tool_execution'
  | 'downstream_foundry_compute'
  | 'eval_target_invocation'
  | 'eval_evaluator_invocation';

export type LogicComputeUsageSurface =
  | 'draft_preview'
  | 'published_logic'
  | 'workshop_widget'
  | 'action_execution'
  | 'automate_run'
  | 'eval_run'
  | 'experiment_run'
  | 'api';

export interface LogicComputeUsageAttribution {
  logicFileId?: string;
  logicVersionId?: string;
  publishedVersionNumber?: number;
  blockId?: string;
  actorId?: string;
  projectId?: string;
  permissionSubjectId?: string;
  invocationSurface: LogicComputeUsageSurface | string;
  automateRunId?: string;
  actionExecutionId?: string;
  workshopWidgetId?: string;
  evalRunId?: string;
  experimentRunId?: string;
}

export interface LogicComputeUsageLineItem {
  id: string;
  category: LogicComputeUsageCategory;
  label: string;
  computeSeconds: number;
  runMultiplier: number;
  blockId?: string;
  blockName?: string;
  toolName?: string;
  toolKind?: LogicToolKind;
  downstreamSystem?: 'ontology_query' | 'action_execution' | 'function_execution' | 'data_transformation' | 'local';
  promptTokensEstimate?: number;
  maxOutputTokens?: number;
  attribution: LogicComputeUsageAttribution;
}

export interface LogicComputeUsageWarning {
  severity: 'warning';
  field: string;
  message: string;
  actual?: number;
  threshold?: number;
}

export interface LogicComputeUsageSummary {
  totalComputeSeconds: number;
  llmBlockComputeSeconds: number;
  llmToolComputeSeconds: number;
  downstreamComputeSeconds: number;
  evaluatorComputeSeconds: number;
  promptTokensEstimate: number;
  runCount: number;
  lineItems: LogicComputeUsageLineItem[];
  warnings: LogicComputeUsageWarning[];
  attribution: LogicComputeUsageAttribution;
}

export function estimatePromptTokens(text: string): number {
  return Math.ceil(text.length / TOKEN_CHARS);
}

function defaultComputeAttribution(overrides: Partial<LogicComputeUsageAttribution> = {}): LogicComputeUsageAttribution {
  return {
    invocationSurface: overrides.invocationSurface ?? 'draft_preview',
    ...overrides,
  };
}

function downstreamUsageForTool(tool: LogicToolConfig): {
  computeSeconds: number;
  downstreamSystem: LogicComputeUsageLineItem['downstreamSystem'];
  label: string;
} {
  if (tool.kind === 'query_objects') {
    const scannedCells = Math.max(1, tool.maxObjects) * Math.max(1, tool.selectedProperties.length);
    return {
      computeSeconds: Math.max(1, Math.ceil(scannedCells / 40)),
      downstreamSystem: 'ontology_query',
      label: `Ontology query ${tool.objectTypeId}`,
    };
  }
  if (tool.kind === 'apply_action') {
    return {
      computeSeconds: tool.invocationMode === 'commit' ? 4 : 2,
      downstreamSystem: 'action_execution',
      label: `Action ${tool.actionTypeId}`,
    };
  }
  if (tool.kind === 'execute_function') {
    return {
      computeSeconds: tool.functionKind === 'existing_logic' ? 6 : 4,
      downstreamSystem: 'function_execution',
      label: `Function ${tool.functionRid}`,
    };
  }
  return {
    computeSeconds: 0,
    downstreamSystem: 'local',
    label: `Calculator ${tool.name}`,
  };
}

function computeWarningThreshold(surface: string) {
  return surface === 'eval_run' || surface === 'experiment_run'
    ? LOGIC_EXPENSIVE_EVAL_WARNING_SECONDS
    : LOGIC_EXPENSIVE_RUN_WARNING_SECONDS;
}

export function summarizeLogicComputeUsage(
  lineItems: LogicComputeUsageLineItem[],
  input: {
    runCount?: number;
    attribution?: Partial<LogicComputeUsageAttribution>;
    warningContext?: string;
  } = {},
): LogicComputeUsageSummary {
  const attribution = defaultComputeAttribution(input.attribution);
  const totalComputeSeconds = lineItems.reduce((sum, item) => sum + item.computeSeconds, 0);
  const promptTokensEstimate = lineItems.reduce((sum, item) => sum + (item.promptTokensEstimate ?? 0), 0);
  const warnings: LogicComputeUsageWarning[] = [];
  const threshold = computeWarningThreshold(attribution.invocationSurface);
  if (totalComputeSeconds >= threshold) {
    warnings.push({
      severity: 'warning',
      field: 'compute.totalComputeSeconds',
      message: `${input.warningContext ?? 'This configuration'} is estimated at ${totalComputeSeconds} compute-seconds before downstream billing details.`,
      actual: totalComputeSeconds,
      threshold,
    });
  }
  if (promptTokensEstimate >= LOGIC_PROMPT_TOKEN_WARNING_THRESHOLD) {
    warnings.push({
      severity: 'warning',
      field: 'compute.promptTokensEstimate',
      message: 'Prompt inputs are token-heavy; reduce object/text context before running at scale.',
      actual: promptTokensEstimate,
      threshold: LOGIC_PROMPT_TOKEN_WARNING_THRESHOLD,
    });
  }
  const downstreamComputeSeconds = lineItems
    .filter((item) => item.category === 'downstream_foundry_compute')
    .reduce((sum, item) => sum + item.computeSeconds, 0);
  if (downstreamComputeSeconds >= totalComputeSeconds / 2 && downstreamComputeSeconds > 0) {
    warnings.push({
      severity: 'warning',
      field: 'compute.downstreamComputeSeconds',
      message: 'Downstream Foundry calls dominate this estimate; review query, action, and function fan-out.',
      actual: downstreamComputeSeconds,
    });
  }
  return {
    totalComputeSeconds,
    llmBlockComputeSeconds: lineItems.filter((item) => item.category === 'llm_block_execution').reduce((sum, item) => sum + item.computeSeconds, 0),
    llmToolComputeSeconds: lineItems.filter((item) => item.category === 'llm_tool_execution').reduce((sum, item) => sum + item.computeSeconds, 0),
    downstreamComputeSeconds,
    evaluatorComputeSeconds: lineItems.filter((item) => item.category === 'eval_evaluator_invocation').reduce((sum, item) => sum + item.computeSeconds, 0),
    promptTokensEstimate,
    runCount: Math.max(1, Math.floor(input.runCount ?? 1)),
    lineItems,
    warnings,
    attribution,
  };
}

export function meterLogicLlmBlockComputeUsage(
  block: LogicLlmBlockConfig,
  input: {
    runCount?: number;
    attribution?: Partial<LogicComputeUsageAttribution>;
  } = {},
): LogicComputeUsageSummary {
  const runCount = Math.max(1, Math.floor(input.runCount ?? 1));
  const attribution = defaultComputeAttribution(input.attribution);
  const blockAttribution = { ...attribution, blockId: block.id };
  const promptTokensEstimate = estimatePromptTokens(`${block.systemPrompt}\n${block.taskPrompt}`);
  const lineItems: LogicComputeUsageLineItem[] = [
    {
      id: `${block.id}:llm-block`,
      category: 'llm_block_execution',
      label: `LLM block ${block.name}`,
      blockId: block.id,
      blockName: block.name,
      computeSeconds: LOGIC_LLM_BLOCK_COMPUTE_SECONDS * runCount,
      runMultiplier: runCount,
      promptTokensEstimate: promptTokensEstimate * runCount,
      maxOutputTokens: block.maxOutputTokens,
      attribution: blockAttribution,
    },
  ];
  block.toolAccess.forEach((tool, index) => {
    const toolId = `${block.id}:tool:${index}:${tool.kind}`;
    lineItems.push({
      id: `${toolId}:llm-tool`,
      category: 'llm_tool_execution',
      label: `Tool execution ${tool.name}`,
      blockId: block.id,
      blockName: block.name,
      toolName: tool.name,
      toolKind: tool.kind,
      computeSeconds: LOGIC_LLM_TOOL_COMPUTE_SECONDS * runCount,
      runMultiplier: runCount,
      attribution: blockAttribution,
    });
    const downstream = downstreamUsageForTool(tool);
    if (downstream.computeSeconds > 0) {
      lineItems.push({
        id: `${toolId}:downstream`,
        category: 'downstream_foundry_compute',
        label: downstream.label,
        blockId: block.id,
        blockName: block.name,
        toolName: tool.name,
        toolKind: tool.kind,
        downstreamSystem: downstream.downstreamSystem,
        computeSeconds: downstream.computeSeconds * runCount,
        runMultiplier: runCount,
        attribution: blockAttribution,
      });
    }
  });
  return summarizeLogicComputeUsage(lineItems, {
    runCount,
    attribution,
    warningContext: `Logic block ${block.name}`,
  });
}

function knownInputApiNames(inputs: LogicInputDefinition[]): Set<string> {
  return new Set(inputs.map((input) => input.apiName));
}

function inputTypeByApiName(inputs: LogicInputDefinition[]): Map<string, LogicInputType> {
  return new Map(inputs.map((input) => [input.apiName, input.type]));
}

function valueTypeCompatible(inputType: LogicInputType | undefined, expected: LogicValueType): boolean {
  if (!inputType) return false;
  if (expected === 'json') return true;
  if (expected === 'double') return ['double', 'float', 'integer', 'long', 'short'].includes(inputType);
  if (expected === 'float') return ['float', 'integer', 'long', 'short'].includes(inputType);
  if (expected === 'long') return ['long', 'integer', 'short'].includes(inputType);
  if (expected === 'integer') return ['integer', 'short'].includes(inputType);
  if (expected === 'array') return inputType === 'array' || inputType === 'list';
  return inputType === expected;
}

function outputTypeCompatible(actual: LogicValueType | undefined, expected: LogicValueType): boolean {
  if (!actual) return false;
  if (expected === 'json') return true;
  if (expected === 'double') return ['double', 'float', 'integer', 'long', 'short'].includes(actual);
  if (expected === 'array') return actual === 'array' || actual === 'list';
  return actual === expected;
}

function validateStructuredOutput(output: LogicStructuredOutputConfig): LogicIssue[] {
  if (output.kind !== 'json_schema' || !output.schemaJson?.trim()) return [];
  try {
    const parsed = JSON.parse(output.schemaJson) as unknown;
    if (typeof parsed !== 'object' || parsed === null || Array.isArray(parsed)) {
      return [{ severity: 'error', field: 'structuredOutput.schemaJson', message: 'Structured JSON schema must be a JSON object.' }];
    }
  } catch {
    return [{ severity: 'error', field: 'structuredOutput.schemaJson', message: 'Structured JSON schema must be valid JSON.' }];
  }
  return [];
}

export function validateQueryObjectsTool(tool: LogicQueryObjectsToolConfig): LogicIssue[] {
  const issues: LogicIssue[] = [];
  if (!tool.name.trim()) {
    issues.push({ severity: 'error', field: 'tool.name', message: 'Query objects tool name is required.' });
  }
  if (!tool.objectTypeId.trim()) {
    issues.push({ severity: 'error', field: 'tool.objectTypeId', message: 'Query objects requires an object type.' });
    return issues;
  }
  if (!tool.readableObjectTypeIds.includes(tool.objectTypeId)) {
    issues.push({ severity: 'error', field: 'tool.objectTypeId', message: 'Selected object type is not readable by this Logic function or user.' });
  }
  const readableProperties = new Set(tool.readablePropertiesByObjectType[tool.objectTypeId] ?? []);
  if (tool.selectedProperties.length === 0) {
    issues.push({ severity: 'error', field: 'tool.selectedProperties', message: 'Select at least one readable property for query results.' });
  }
  for (const property of tool.selectedProperties) {
    if (!readableProperties.has(property)) {
      issues.push({ severity: 'error', field: `tool.selectedProperties.${property}`, message: `Property ${property} is not readable on ${tool.objectTypeId}.` });
    }
  }
  if (tool.selectedProperties.length > PROPERTY_WARNING_THRESHOLD) {
    issues.push({ severity: 'warning', field: 'tool.selectedProperties', message: 'Exposing many properties can inflate prompts; select only fields the LLM needs.' });
  }
  if (tool.maxObjects > 50) {
    issues.push({ severity: 'warning', field: 'tool.maxObjects', message: 'Large object result limits can be token-expensive; consider lowering max objects or adding filters.' });
  }
  return issues;
}

export function validateApplyActionTool(tool: LogicActionToolConfig, inputs: LogicInputDefinition[]): LogicIssue[] {
  const issues: LogicIssue[] = [];
  const inputTypes = inputTypeByApiName(inputs);
  if (!tool.name.trim()) {
    issues.push({ severity: 'error', field: 'tool.name', message: 'Apply action tool name is required.' });
  }
  if (!tool.actionTypeId.trim()) {
    issues.push({ severity: 'error', field: 'tool.actionTypeId', message: 'Select an action type before exposing Apply action to the LLM.' });
  } else if (!tool.allowedActionTypeIds.includes(tool.actionTypeId)) {
    issues.push({ severity: 'error', field: 'tool.actionTypeId', message: 'Selected action type is not permitted for this Logic function or user.' });
  }
  for (const [parameter, expectedType] of Object.entries(tool.expectedParameters)) {
    const mappedInput = tool.parameterMappings[parameter];
    if (!mappedInput) {
      issues.push({ severity: 'error', field: `tool.parameterMappings.${parameter}`, message: `Action parameter ${parameter} must be mapped.` });
      continue;
    }
    if (!valueTypeCompatible(inputTypes.get(mappedInput), expectedType)) {
      issues.push({ severity: 'error', field: `tool.parameterMappings.${parameter}`, message: `Action parameter ${parameter} mapping is not type-compatible with ${expectedType}.` });
    }
  }
  if (tool.invocationMode === 'commit') {
    if (!tool.logicPublished) {
      issues.push({ severity: 'error', field: 'tool.invocationMode', message: 'Real Ontology edits require published Logic.' });
    }
    if (tool.invocationSurface !== 'published_action' && tool.invocationSurface !== 'automation') {
      issues.push({ severity: 'error', field: 'tool.invocationSurface', message: 'Real Ontology edits require action or automation invocation.' });
    }
  }
  return issues;
}

export function validateExecuteFunctionTool(tool: LogicExecuteFunctionToolConfig, inputs: LogicInputDefinition[]): LogicIssue[] {
  const issues: LogicIssue[] = [];
  const inputTypes = inputTypeByApiName(inputs);
  if (!tool.name.trim()) {
    issues.push({ severity: 'error', field: 'tool.name', message: 'Execute function tool name is required.' });
  }
  if (!tool.functionRid.trim()) {
    issues.push({ severity: 'error', field: 'tool.functionRid', message: 'Select a TypeScript, Python, existing Logic, or function-on-objects function.' });
  } else if (!tool.allowedFunctionRids.includes(tool.functionRid)) {
    issues.push({ severity: 'error', field: 'tool.functionRid', message: 'Selected function is not executable by this Logic function or user.' });
  }
  for (const [parameter, expectedType] of Object.entries(tool.signature.parameters)) {
    const mappedInput = tool.parameterMappings[parameter];
    if (!mappedInput) {
      issues.push({ severity: 'error', field: `tool.parameterMappings.${parameter}`, message: `Function parameter ${parameter} must be mapped.` });
      continue;
    }
    if (!valueTypeCompatible(inputTypes.get(mappedInput), expectedType)) {
      issues.push({ severity: 'error', field: `tool.parameterMappings.${parameter}`, message: `Function parameter ${parameter} mapping is not type-compatible with ${expectedType}.` });
    }
  }
  if (!outputTypeCompatible(tool.signature.returnType, tool.expectedOutputType)) {
    issues.push({ severity: 'error', field: 'tool.expectedOutputType', message: 'Function return type is not compatible with the configured tool output.' });
  }
  return issues;
}

function calculatorRefs(expression: string): string[] {
  return Array.from(new Set(expression.match(/[A-Za-z_][A-Za-z0-9_]*/g) ?? []));
}

export function validateCalculatorTool(tool: LogicCalculatorToolConfig, inputs: LogicInputDefinition[]): LogicIssue[] {
  const issues: LogicIssue[] = [];
  const inputTypes = inputTypeByApiName(inputs);
  if (!tool.name.trim()) {
    issues.push({ severity: 'error', field: 'tool.name', message: 'Calculator tool name is required.' });
  }
  if (!tool.expression.trim()) {
    issues.push({ severity: 'error', field: 'tool.expression', message: 'Calculator expression is required.' });
    return issues;
  }
  if (!CALCULATOR_EXPRESSION_PATTERN.test(tool.expression)) {
    issues.push({ severity: 'error', field: 'tool.expression', message: 'Calculator supports numbers, input variables, parentheses, and + - * / % only.' });
  }
  const refs = calculatorRefs(tool.expression);
  for (const ref of refs) {
    const inputType = inputTypes.get(ref);
    if (!inputType) {
      issues.push({ severity: 'error', field: `tool.expression.${ref}`, message: `Calculator variable ${ref} does not match a Logic input.` });
    } else if (!['double', 'float', 'integer', 'long', 'short'].includes(inputType)) {
      issues.push({ severity: 'error', field: `tool.expression.${ref}`, message: `Calculator variable ${ref} must be numeric.` });
    }
  }
  for (const ref of tool.parameterRefs) {
    if (!refs.includes(ref)) {
      issues.push({ severity: 'warning', field: `tool.parameterRefs.${ref}`, message: `Calculator parameter ${ref} is configured but not used in the expression.` });
    }
  }
  return issues;
}

export function evaluateCalculatorExpression(expression: string, values: Record<string, number>): number {
  const tokens = expression.match(/\d+(?:\.\d+)?|[A-Za-z_][A-Za-z0-9_]*|[()+\-*/%]/g) ?? [];
  const output: string[] = [];
  const operators: string[] = [];
  const precedence: Record<string, number> = { '+': 1, '-': 1, '*': 2, '/': 2, '%': 2 };
  for (const token of tokens) {
    if (/^\d/.test(token)) {
      output.push(token);
    } else if (/^[A-Za-z_]/.test(token)) {
      const value = values[token];
      if (typeof value !== 'number') throw new Error(`Missing calculator value for ${token}`);
      output.push(String(value));
    } else if (token === '(') {
      operators.push(token);
    } else if (token === ')') {
      while (operators.length && operators[operators.length - 1] !== '(') output.push(operators.pop() ?? '');
      if (operators.pop() !== '(') throw new Error('Mismatched parentheses');
    } else {
      while (operators.length && precedence[operators[operators.length - 1]] >= precedence[token]) output.push(operators.pop() ?? '');
      operators.push(token);
    }
  }
  while (operators.length) {
    const operator = operators.pop() ?? '';
    if (operator === '(') throw new Error('Mismatched parentheses');
    output.push(operator);
  }
  const stack: number[] = [];
  for (const token of output) {
    if (!Number.isNaN(Number(token))) {
      stack.push(Number(token));
      continue;
    }
    const b = stack.pop();
    const a = stack.pop();
    if (a === undefined || b === undefined) throw new Error('Invalid calculator expression');
    if (token === '+') stack.push(a + b);
    else if (token === '-') stack.push(a - b);
    else if (token === '*') stack.push(a * b);
    else if (token === '/') stack.push(a / b);
    else if (token === '%') stack.push(a % b);
  }
  if (stack.length !== 1) throw new Error('Invalid calculator expression');
  return stack[0];
}

export interface LogicPromptVariableSafety {
  apiName: string;
  /**
   * 'value' substitutes the raw value (escaped for prompt injection), 'fragment' allows
   * pre-approved prompt fragments and validates them against `allowedValues`.
   */
  kind: 'value' | 'fragment';
  allowedValues?: string[];
  maxLength?: number;
}

export interface LogicModelVariableSafety {
  apiName: string;
  allowedModelIds?: string[];
  defaultModelId?: string;
}

export interface LogicParameterizationSafetyConfig {
  promptVariables?: LogicPromptVariableSafety[];
  modelVariables?: LogicModelVariableSafety[];
}

export interface LogicParameterizationResolution {
  ready: boolean;
  issues: LogicIssue[];
  /** Resolved model id (from fixed binding or the model variable). */
  resolvedModelId?: string;
  /** Resolved prompt text after variable substitution. */
  renderedSystemPrompt: string;
  renderedTaskPrompt: string;
  /** Map of variable api name to the actual substituted value (after escaping). */
  resolvedVariableValues: Record<string, string>;
}

const PROMPT_VARIABLE_DEFAULT_MAX_LENGTH = 4000;
const PROMPT_INJECTION_PATTERN = /(?:^|\n)\s*(?:system|assistant|developer)\s*:/i;

function escapePromptValue(value: unknown): string {
  if (value === undefined || value === null) return '';
  const stringValue = typeof value === 'string' ? value : (() => {
    try { return JSON.stringify(value); } catch { return String(value); }
  })();
  return stringValue
    .replace(/```/g, '` ``')
    .replace(/<\|\s*system\s*\|>/gi, '<system>')
    .replace(/<\|\s*assistant\s*\|>/gi, '<assistant>');
}

function inputValueString(rawValue: unknown): string {
  if (rawValue === undefined || rawValue === null) return '';
  if (typeof rawValue === 'string') return rawValue;
  if (typeof rawValue === 'number' || typeof rawValue === 'boolean') return String(rawValue);
  try { return JSON.stringify(rawValue); } catch { return String(rawValue); }
}

function substituteFragments(template: string, values: Record<string, string>): string {
  return template.replace(/\{\{\s*([A-Za-z][A-Za-z0-9_]*)\s*\}\}/g, (match, name: string) => (name in values ? values[name] : match));
}

export function resolveLogicBlockParameterization(
  block: LogicLlmBlockConfig,
  inputs: LogicInputDefinition[],
  inputValues: Record<string, unknown> = {},
  safety: LogicParameterizationSafetyConfig = {},
): LogicParameterizationResolution {
  const issues: LogicIssue[] = [];
  const promptSafetyMap = new Map((safety.promptVariables ?? []).map((entry) => [entry.apiName, entry]));
  const modelSafetyMap = new Map((safety.modelVariables ?? []).map((entry) => [entry.apiName, entry]));
  const resolvedVariableValues: Record<string, string> = {};

  // Resolve model variable
  let resolvedModelId: string | undefined;
  if (block.modelBinding.mode === 'fixed') {
    resolvedModelId = block.modelBinding.providerId?.trim() || undefined;
  } else {
    const apiName = block.modelBinding.modelVariableApiName ?? '';
    const safetyEntry = modelSafetyMap.get(apiName);
    const rawValue = inputValueString(inputValues[apiName]).trim() || safetyEntry?.defaultModelId?.trim() || '';
    if (!rawValue) {
      issues.push({ severity: 'error', field: `modelVariable.${apiName}`, message: `Model variable ${apiName} requires a value or default.` });
    } else if (safetyEntry?.allowedModelIds && safetyEntry.allowedModelIds.length > 0 && !safetyEntry.allowedModelIds.includes(rawValue)) {
      issues.push({ severity: 'error', field: `modelVariable.${apiName}`, message: `Model ${rawValue} is not in the allowed model list for variable ${apiName}.` });
    } else {
      resolvedModelId = rawValue;
    }
  }

  // Resolve prompt variables
  for (const ref of block.promptVariableRefs) {
    const input = inputs.find((candidate) => candidate.apiName === ref);
    if (!input) {
      issues.push({ severity: 'error', field: `promptVariableRefs.${ref}`, message: `Prompt variable ${ref} does not match a Logic input.` });
      continue;
    }
    const rawValue = inputValues[ref] ?? input.defaultValue;
    const safetyEntry = promptSafetyMap.get(ref);
    const maxLength = safetyEntry?.maxLength ?? PROMPT_VARIABLE_DEFAULT_MAX_LENGTH;
    if (rawValue === undefined && input.required) {
      issues.push({ severity: 'error', field: `promptVariableRefs.${ref}`, message: `Required prompt variable ${ref} has no value.` });
      continue;
    }
    const stringValue = inputValueString(rawValue);
    if (stringValue.length > maxLength) {
      issues.push({ severity: 'error', field: `promptVariableRefs.${ref}`, message: `Prompt variable ${ref} exceeds max length ${maxLength}.` });
      continue;
    }
    if (safetyEntry?.allowedValues && safetyEntry.allowedValues.length > 0 && !safetyEntry.allowedValues.includes(stringValue)) {
      issues.push({ severity: 'error', field: `promptVariableRefs.${ref}`, message: `Prompt variable ${ref} value is not in the allowed list.` });
      continue;
    }
    if (safetyEntry?.kind === 'fragment') {
      // Fragments are pre-approved; only allow them when they appear in the allowed values list.
      if (!safetyEntry.allowedValues || !safetyEntry.allowedValues.includes(stringValue)) {
        issues.push({ severity: 'error', field: `promptVariableRefs.${ref}`, message: `Prompt fragment variable ${ref} requires an explicit allowed list and a matching value to prevent prompt injection.` });
        continue;
      }
      resolvedVariableValues[ref] = stringValue;
    } else {
      if (PROMPT_INJECTION_PATTERN.test(stringValue)) {
        issues.push({ severity: 'warning', field: `promptVariableRefs.${ref}`, message: `Prompt variable ${ref} appears to contain role-tag text and will be neutralised before substitution.` });
      }
      resolvedVariableValues[ref] = escapePromptValue(stringValue);
    }
  }

  const renderedSystemPrompt = substituteFragments(block.systemPrompt ?? '', resolvedVariableValues);
  const renderedTaskPrompt = substituteFragments(block.taskPrompt ?? '', resolvedVariableValues);

  // Detect unresolved placeholders left behind so prompts don't ship with `{{name}}` markers.
  const unresolvedPattern = /\{\{\s*([A-Za-z][A-Za-z0-9_]*)\s*\}\}/g;
  const combinedRendered = `${renderedSystemPrompt}\n${renderedTaskPrompt}`;
  let match: RegExpExecArray | null;
  const reported = new Set<string>();
  while ((match = unresolvedPattern.exec(combinedRendered)) !== null) {
    if (reported.has(match[1])) continue;
    reported.add(match[1]);
    issues.push({ severity: 'warning', field: `prompt.${match[1]}`, message: `Prompt references {{${match[1]}}} but no matching variable was resolved.` });
  }

  return {
    ready: !issues.some((issue) => issue.severity === 'error'),
    issues,
    resolvedModelId,
    renderedSystemPrompt,
    renderedTaskPrompt,
    resolvedVariableValues,
  };
}

export function validateLlmBlock(block: LogicLlmBlockConfig, inputs: LogicInputDefinition[]): LogicIssue[] {
  const issues: LogicIssue[] = [];
  const inputNames = knownInputApiNames(inputs);
  if (!block.name.trim()) {
    issues.push({ severity: 'error', field: 'name', message: 'Use LLM block name is required.' });
  }
  if (block.modelBinding.mode === 'fixed') {
    if (!block.modelBinding.providerId?.trim()) {
      issues.push({ severity: 'error', field: 'modelBinding.providerId', message: 'Select a fixed model provider or switch to a model variable.' });
    }
  } else {
    const apiName = block.modelBinding.modelVariableApiName ?? '';
    const modelInput = inputs.find((input) => input.apiName === apiName);
    if (!isValidLogicInputApiName(apiName)) {
      issues.push({ severity: 'error', field: 'modelBinding.modelVariableApiName', message: 'Model variable reference must be a valid input API name.' });
    } else if (!modelInput || modelInput.type !== 'model') {
      issues.push({ severity: 'error', field: 'modelBinding.modelVariableApiName', message: 'Model variable reference must point to a model input.' });
    }
  }
  if (!block.taskPrompt.trim()) {
    issues.push({ severity: 'error', field: 'taskPrompt', message: 'Task prompt is required for a Use LLM block.' });
  }
  for (const ref of block.promptVariableRefs) {
    if (!inputNames.has(ref)) {
      issues.push({ severity: 'error', field: `promptVariableRefs.${ref}`, message: `Prompt variable ${ref} does not match a Logic input.` });
    }
  }
  issues.push(...validateStructuredOutput(block.structuredOutput));
  for (const tool of block.toolAccess) {
    if (tool.kind === 'query_objects') issues.push(...validateQueryObjectsTool(tool));
    if (tool.kind === 'apply_action') issues.push(...validateApplyActionTool(tool, inputs));
    if (tool.kind === 'execute_function') issues.push(...validateExecuteFunctionTool(tool, inputs));
    if (tool.kind === 'calculator') issues.push(...validateCalculatorTool(tool, inputs));
  }
  if (block.toolAccess.filter((tool) => tool.kind === 'query_objects').length > TOOL_OBJECT_WARNING_THRESHOLD) {
    issues.push({ severity: 'warning', field: 'toolAccess', message: 'Too many query tools can expose more object context than the prompt needs.' });
  }
  const tokenEstimate = estimatePromptTokens(`${block.systemPrompt}\n${block.taskPrompt}`);
  if (tokenEstimate + block.maxOutputTokens > 8000) {
    issues.push({ severity: 'warning', field: 'maxOutputTokens', message: 'Prompt plus output budget may exceed common interactive model context windows.' });
  }
  return issues;
}

function toolHasError(tool: LogicToolConfig, issues: LogicIssue[]): boolean {
  const errorFields = issues.filter((issue) => issue.severity === 'error').map((issue) => issue.field);
  if (tool.kind === 'query_objects') {
    return errorFields.some((field) => field === 'tool.objectTypeId' || field === 'tool.selectedProperties' || field.startsWith('tool.selectedProperties.'));
  }
  if (tool.kind === 'apply_action') {
    return errorFields.some((field) => field === 'tool.actionTypeId' || field.startsWith('tool.parameterMappings.') || field.startsWith('tool.invocation'));
  }
  if (tool.kind === 'execute_function') {
    return errorFields.some((field) => field === 'tool.functionRid' || field.startsWith('tool.parameterMappings.') || field === 'tool.expectedOutputType');
  }
  if (tool.kind === 'calculator') return errorFields.some((field) => field.startsWith('tool.expression'));
  return false;
}

export function buildLlmDebuggerTrace(block: LogicLlmBlockConfig, inputs: LogicInputDefinition[]): LogicDebuggerTraceMetadata {
  const issues = validateLlmBlock(block, inputs);
  const promptTokensEstimate = estimatePromptTokens(`${block.systemPrompt}\n${block.taskPrompt}`);
  const computeUsage = meterLogicLlmBlockComputeUsage(block, {
    attribution: { invocationSurface: 'draft_preview', blockId: block.id },
  });
  return {
    blockId: block.id,
    renderedPrompt: {
      system: block.systemPrompt,
      task: block.taskPrompt,
      variables: block.promptVariableRefs,
    },
    toolCalls: block.toolAccess.map((tool) => ({
      toolName: tool.name,
      kind: tool.kind,
      objectTypeId: tool.kind === 'query_objects' ? tool.objectTypeId : undefined,
      actionTypeId: tool.kind === 'apply_action' ? tool.actionTypeId : undefined,
      functionRid: tool.kind === 'execute_function' ? tool.functionRid : undefined,
      expression: tool.kind === 'calculator' ? tool.expression : undefined,
      selectedProperties: tool.kind === 'query_objects' ? tool.selectedProperties : undefined,
      parameterMappings: tool.kind === 'apply_action' || tool.kind === 'execute_function' ? tool.parameterMappings : undefined,
      maxObjects: tool.kind === 'query_objects' ? tool.maxObjects : undefined,
      status: toolHasError(tool, issues) ? 'error' : tool.kind === 'apply_action' && tool.invocationMode === 'preview' ? 'preview_only' : 'not_run',
    })),
    proposedOntologyEdits: block.toolAccess
      .filter((tool): tool is LogicActionToolConfig => tool.kind === 'apply_action')
      .map((tool) => ({
        actionTypeId: tool.actionTypeId,
        parameters: tool.parameterMappings,
        applyState: tool.invocationMode === 'commit' ? 'ready_for_commit' : 'preview_only',
      })),
    output: {
      structuredOutputKind: block.structuredOutput.kind,
      preview: block.structuredOutput.kind === 'text' ? 'LLM text response preview' : 'Structured output will be validated before final output mapping.',
    },
    tokenUsage: {
      promptTokensEstimate,
      maxOutputTokens: block.maxOutputTokens,
      computeUnitsEstimate: Math.max(1, Math.ceil(computeUsage.totalComputeSeconds / LOGIC_LLM_BLOCK_COMPUTE_SECONDS)),
    },
    computeUsage,
    errors: issues,
  };
}

export type LogicVariableSource = 'literal' | 'input' | 'block_output';
export type LogicOutputSource = 'block_output' | 'intermediate' | 'ontology_edit_bundle';
export type WorkshopOutputUsage = 'none' | 'markdown_display' | 'general_display';

export interface LogicVariableBlockConfig {
  id: string;
  apiName: string;
  valueType: LogicValueType;
  source: LogicVariableSource;
  literalValue?: string;
  inputApiName?: string;
  blockOutputId?: string;
}

export interface LogicConditionalBranchConfig {
  id: string;
  conditionExpression?: string;
  outputType?: LogicValueType;
  returnsOntologyEdits?: boolean;
  takeNoAction?: boolean;
}

export interface LogicConditionalBlockConfig {
  id: string;
  conditionExpression: string;
  trueOutputType: LogicValueType;
  falseOutputType: LogicValueType;
  branches?: LogicConditionalBranchConfig[];
}

export interface LogicLoopBlockConfig {
  id: string;
  inputApiName: string;
  elementVariableApiName: string;
  indexVariableApiName: string;
  bodyOutputType: LogicValueType;
  outputAggregation: 'list' | 'first' | 'count' | 'none';
  finalOutputType: LogicValueType;
  containsActionTool: boolean;
  parallel: boolean;
  arrayToListInserted?: boolean;
}

export interface LogicOutputDefinition {
  id: string;
  name: string;
  apiName: string;
  outputType: LogicValueType;
  source: LogicOutputSource;
  sourceId: string;
  final: boolean;
  workshopUsage: WorkshopOutputUsage;
  intermediateParameter?: boolean;
  exposedBlockOutputId?: string;
}

export interface LogicVersionBlockDefinition extends Record<string, unknown> {
  id: string;
  name?: string;
  kind?: string;
  type?: string;
  systemPrompt?: string;
  taskPrompt?: string;
  modelBinding?: LogicLlmBlockConfig['modelBinding'];
}

export interface LogicVersionDefinition {
  inputs: LogicInputDefinition[];
  blocks: LogicVersionBlockDefinition[];
  outputs: LogicOutputDefinition[];
}

function logicLlmBlockFromDefinitionBlock(block: LogicVersionBlockDefinition): LogicLlmBlockConfig | undefined {
  const kind = String(block.kind ?? block.type ?? '').toLowerCase();
  const raw = block as LogicVersionBlockDefinition & Partial<LogicLlmBlockConfig>;
  const toolAccess = Array.isArray(raw.toolAccess) ? raw.toolAccess : [];
  const hasLlmShape = Boolean(raw.systemPrompt || raw.taskPrompt || raw.modelBinding || toolAccess.length > 0);
  if (!['use_llm', 'llm', 'aip_logic_llm'].includes(kind) && !hasLlmShape) return undefined;
  return {
    id: block.id,
    name: block.name ?? block.id,
    modelBinding: raw.modelBinding ?? { mode: 'fixed', providerId: 'default-model' },
    systemPrompt: raw.systemPrompt ?? '',
    taskPrompt: raw.taskPrompt ?? '',
    promptVariableRefs: Array.isArray(raw.promptVariableRefs) ? raw.promptVariableRefs : [],
    toolAccess,
    structuredOutput: raw.structuredOutput ?? { kind: 'text' },
    maxOutputTokens: typeof raw.maxOutputTokens === 'number' ? raw.maxOutputTokens : 1024,
  };
}

export function estimateLogicComputeUsage(input: {
  definition?: LogicVersionDefinition;
  llmBlocks?: LogicLlmBlockConfig[];
  runCount?: number;
  attribution?: Partial<LogicComputeUsageAttribution>;
  warningContext?: string;
}): LogicComputeUsageSummary {
  const runCount = Math.max(1, Math.floor(input.runCount ?? 1));
  const attribution = defaultComputeAttribution(input.attribution);
  const blocks = input.llmBlocks ?? input.definition?.blocks
    .map(logicLlmBlockFromDefinitionBlock)
    .filter((block): block is LogicLlmBlockConfig => Boolean(block)) ?? [];
  const lineItems = blocks.flatMap((block) => meterLogicLlmBlockComputeUsage(block, {
    runCount,
    attribution: { ...attribution, blockId: block.id },
  }).lineItems);
  return summarizeLogicComputeUsage(lineItems, {
    runCount,
    attribution,
    warningContext: input.warningContext ?? 'Logic run',
  });
}

export function estimateLogicEvaluationComputeUsage(input: {
  definition?: LogicVersionDefinition;
  llmBlocks?: LogicLlmBlockConfig[];
  targetCount: number;
  testCaseCount: number;
  evaluatorCount: number;
  iterations?: number;
  attribution?: Partial<LogicComputeUsageAttribution>;
}): LogicComputeUsageSummary {
  const iterations = Math.max(1, Math.floor(input.iterations ?? 1));
  const runCount = Math.max(1, input.targetCount) * Math.max(1, input.testCaseCount) * iterations;
  const attribution = defaultComputeAttribution({ invocationSurface: 'eval_run', ...input.attribution });
  const targetUsage = estimateLogicComputeUsage({
    definition: input.definition,
    llmBlocks: input.llmBlocks,
    runCount,
    attribution,
    warningContext: 'Evaluation target invocations',
  });
  const evaluatorInvocations = Math.max(0, input.evaluatorCount) * Math.max(0, input.targetCount) * Math.max(0, input.testCaseCount) * iterations;
  const evaluatorLineItems: LogicComputeUsageLineItem[] = evaluatorInvocations > 0 ? [{
    id: `eval:${attribution.evalRunId ?? 'suite'}:evaluators`,
    category: 'eval_evaluator_invocation',
    label: 'Built-in evaluator invocations',
    computeSeconds: evaluatorInvocations * LOGIC_EVALUATOR_COMPUTE_SECONDS,
    runMultiplier: evaluatorInvocations,
    attribution,
  }] : [];
  return summarizeLogicComputeUsage([...targetUsage.lineItems, ...evaluatorLineItems], {
    runCount,
    attribution,
    warningContext: 'Evaluation run',
  });
}

export type LogicVersionStatus = 'draft' | 'published' | 'superseded';

export interface LogicVersionComponentChange {
  id: string;
  name?: string;
  kind?: string;
  changeType: 'added' | 'edited' | 'removed';
}

export interface LogicVersionValueChange {
  blockId: string;
  blockName?: string;
  changeType: 'edited';
  oldValue?: unknown;
  newValue?: unknown;
}

export interface LogicVersionChangeSummary {
  inputs: LogicVersionComponentChange[];
  blocks: LogicVersionComponentChange[];
  outputs: LogicVersionComponentChange[];
  promptChanges: LogicVersionValueChange[];
  modelChanges: LogicVersionValueChange[];
}

export interface LogicSavedVersion {
  id: string;
  versionNumber: number;
  author: string;
  createdAtIso: string;
  status: LogicVersionStatus;
  definition: LogicVersionDefinition;
  changeSummary: LogicVersionChangeSummary;
  publishedAtIso?: string;
}

export interface LogicVersionComparison {
  baseVersionId: string;
  headVersionId: string;
  baseVersionNumber: number;
  headVersionNumber: number;
  summary: LogicVersionChangeSummary;
}

interface VersionComponentSnapshot {
  id: string;
  name?: string;
  kind?: string;
  raw: string;
  prompt?: unknown;
  promptRaw: string;
  model?: unknown;
  modelRaw: string;
}

const PROMPT_VERSION_FIELDS = ['systemPrompt', 'system_prompt', 'taskPrompt', 'task_prompt', 'prompt', 'promptTemplate', 'prompt_template', 'promptVariableRefs', 'prompt_variable_refs', 'structuredOutput', 'structured_output'];
const MODEL_VERSION_FIELDS = ['modelBinding', 'model_binding', 'model', 'providerId', 'provider_id', 'modelVariableApiName', 'model_variable_api_name'];

function stableStringify(value: unknown): string {
  if (Array.isArray(value)) return `[${value.map(stableStringify).join(',')}]`;
  if (value && typeof value === 'object') {
    return `{${Object.entries(value as Record<string, unknown>)
      .filter(([, entryValue]) => entryValue !== undefined)
      .sort(([left], [right]) => left.localeCompare(right))
      .map(([key, entryValue]) => `${JSON.stringify(key)}:${stableStringify(entryValue)}`)
      .join(',')}}`;
  }
  return JSON.stringify(value);
}

function versionStringField(item: Record<string, unknown>, keys: string[]): string | undefined {
  for (const key of keys) {
    const value = item[key];
    if (typeof value === 'string' && value.trim()) return value;
  }
  return undefined;
}

function versionSubset(item: Record<string, unknown>, keys: string[]): unknown | undefined {
  const entries = keys
    .filter((key) => item[key] !== undefined)
    .map((key) => [key, item[key]] as const);
  return entries.length ? Object.fromEntries(entries) : undefined;
}

function versionComponentSnapshot(item: Record<string, unknown>, fallbackId: string): VersionComponentSnapshot {
  const id = versionStringField(item, ['id', 'apiName', 'api_name', 'name']) ?? fallbackId;
  const prompt = versionSubset(item, PROMPT_VERSION_FIELDS);
  const model = versionSubset(item, MODEL_VERSION_FIELDS);
  return {
    id,
    name: versionStringField(item, ['name', 'displayName', 'display_name', 'apiName', 'api_name']),
    kind: versionStringField(item, ['type', 'kind', 'blockType', 'block_type', 'outputType', 'output_type']),
    raw: stableStringify(item),
    prompt,
    promptRaw: prompt ? stableStringify(prompt) : '',
    model,
    modelRaw: model ? stableStringify(model) : '',
  };
}

function versionComponents(items: Array<Record<string, unknown>>, prefix: string): VersionComponentSnapshot[] {
  return items.map((item, index) => versionComponentSnapshot(item, `${prefix}[${index}]`));
}

function diffVersionComponents(base: VersionComponentSnapshot[], head: VersionComponentSnapshot[]): LogicVersionComponentChange[] {
  const baseById = new Map(base.map((item) => [item.id, item]));
  const headById = new Map(head.map((item) => [item.id, item]));
  const changes: LogicVersionComponentChange[] = [];
  for (const [id, headItem] of headById) {
    const baseItem = baseById.get(id);
    if (!baseItem) changes.push({ id, name: headItem.name, kind: headItem.kind, changeType: 'added' });
    else if (baseItem.raw !== headItem.raw) changes.push({ id, name: headItem.name, kind: headItem.kind, changeType: 'edited' });
  }
  for (const [id, baseItem] of baseById) {
    if (!headById.has(id)) changes.push({ id, name: baseItem.name, kind: baseItem.kind, changeType: 'removed' });
  }
  return changes.sort((left, right) => left.changeType.localeCompare(right.changeType) || left.id.localeCompare(right.id));
}

function diffVersionValueChanges(base: VersionComponentSnapshot[], head: VersionComponentSnapshot[], field: 'prompt' | 'model'): LogicVersionValueChange[] {
  const baseById = new Map(base.map((item) => [item.id, item]));
  return head.flatMap((headItem) => {
    const baseItem = baseById.get(headItem.id);
    if (!baseItem) return [];
    const baseRaw = field === 'prompt' ? baseItem.promptRaw : baseItem.modelRaw;
    const headRaw = field === 'prompt' ? headItem.promptRaw : headItem.modelRaw;
    if (baseRaw === headRaw) return [];
    return [{
      blockId: headItem.id,
      blockName: headItem.name,
      changeType: 'edited' as const,
      oldValue: field === 'prompt' ? baseItem.prompt : baseItem.model,
      newValue: field === 'prompt' ? headItem.prompt : headItem.model,
    }];
  }).sort((left, right) => left.blockId.localeCompare(right.blockId));
}

export function compareLogicVersionDefinitions(base: LogicVersionDefinition, head: LogicVersionDefinition): LogicVersionChangeSummary {
  const baseInputs = versionComponents(base.inputs as unknown as Array<Record<string, unknown>>, 'inputs');
  const headInputs = versionComponents(head.inputs as unknown as Array<Record<string, unknown>>, 'inputs');
  const baseBlocks = versionComponents(base.blocks, 'blocks');
  const headBlocks = versionComponents(head.blocks, 'blocks');
  const baseOutputs = versionComponents(base.outputs as unknown as Array<Record<string, unknown>>, 'outputs');
  const headOutputs = versionComponents(head.outputs as unknown as Array<Record<string, unknown>>, 'outputs');
  return {
    inputs: diffVersionComponents(baseInputs, headInputs),
    blocks: diffVersionComponents(baseBlocks, headBlocks),
    outputs: diffVersionComponents(baseOutputs, headOutputs),
    promptChanges: diffVersionValueChanges(baseBlocks, headBlocks, 'prompt'),
    modelChanges: diffVersionValueChanges(baseBlocks, headBlocks, 'model'),
  };
}

export function createLogicSavedVersion(
  base: LogicVersionDefinition,
  head: LogicVersionDefinition,
  author: string,
  now: Date,
  versionNumber: number,
): LogicSavedVersion {
  return {
    id: `logic-version-${versionNumber}-${now.getTime().toString(36)}`,
    versionNumber,
    author,
    createdAtIso: now.toISOString(),
    status: 'draft',
    definition: head,
    changeSummary: compareLogicVersionDefinitions(base, head),
  };
}

export function publishLogicSavedVersion(versions: LogicSavedVersion[], versionId: string, now: Date): LogicSavedVersion[] {
  return versions.map((version) => {
    if (version.id === versionId) {
      return { ...version, status: 'published', publishedAtIso: now.toISOString() };
    }
    if (version.status === 'published') return { ...version, status: 'superseded' };
    return version;
  });
}

export function compareLogicSavedVersions(base: LogicSavedVersion, head: LogicSavedVersion): LogicVersionComparison {
  return {
    baseVersionId: base.id,
    headVersionId: head.id,
    baseVersionNumber: base.versionNumber,
    headVersionNumber: head.versionNumber,
    summary: compareLogicVersionDefinitions(base.definition, head.definition),
  };
}

export type LogicBranchResourceStatus = 'active' | 'removed' | 'merged';
export type LogicBranchAdapterOperation = 'add' | 'remove' | 'edit' | 'publish' | 'review' | 'rebase' | 'merge';
export type LogicBranchReviewStatus = 'pending' | 'approved' | 'rejected';
export type LogicBranchProposalStatus = 'draft' | 'in_review' | 'approved' | 'rejected' | 'merged';
export type LogicBranchMergeCheckId =
  | 'resource_present'
  | 'published_on_branch'
  | 'up_to_date_with_main'
  | 'publishable_state'
  | 'no_pending_approvals';

export interface LogicBranchOperationLogEntry {
  operation: LogicBranchAdapterOperation;
  actor: string;
  atIso: string;
  detail: string;
}

export interface LogicBranchPublication {
  functionRid: string;
  versionId: string;
  versionNumber: number;
  tag: 'Branched pre-release';
  availableOnBranchId: string;
  callableSurfaces: Array<'workshop' | 'function_backed_actions' | 'branch_ontology_objects'>;
}

export interface LogicBranchReview {
  reviewerId: string;
  reviewerName: string;
  status: LogicBranchReviewStatus;
  decidedAtIso?: string;
  comment?: string;
}

export interface LogicBranchProposal {
  id: string;
  status: LogicBranchProposalStatus;
  createdBy: string;
  createdAtIso: string;
  reviews: LogicBranchReview[];
}

export interface LogicBranchRebaseConflict {
  id: string;
  component: 'input' | 'block' | 'output' | 'prompt' | 'model';
  componentId: string;
  mainChange?: string;
  branchChange?: string;
  requiresManualResolution: boolean;
}

export interface LogicBranchAdapterResource {
  resourceRid: string;
  logicFileId: string;
  branchId: string;
  branchName: string;
  status: LogicBranchResourceStatus;
  mainBaseVersion: LogicSavedVersion;
  mainCurrentVersion: LogicSavedVersion;
  branchVersion: LogicSavedVersion;
  publication?: LogicBranchPublication;
  proposal?: LogicBranchProposal;
  pendingApprovalCount: number;
  rebaseRequired: boolean;
  conflicts: LogicBranchRebaseConflict[];
  operations: LogicBranchOperationLogEntry[];
  addedAtIso: string;
  updatedAtIso: string;
  removedAtIso?: string;
  mergedAtIso?: string;
  removalBlockedReason?: string;
}

export interface LogicBranchMergeCheck {
  id: LogicBranchMergeCheckId;
  label: string;
  status: 'passed' | 'blocked';
  message: string;
  issues?: LogicIssue[];
}

export interface LogicBranchMergeReadiness {
  mergeable: boolean;
  checks: LogicBranchMergeCheck[];
}

export interface LogicBranchMergeResult {
  merged: boolean;
  resource: LogicBranchAdapterResource;
  checks: LogicBranchMergeCheck[];
  mergedMainVersion?: LogicSavedVersion;
  errors: string[];
}

export interface LogicBranchRebaseResolution {
  acceptManualResolution?: boolean;
  notes?: string;
}

function branchSafeId(value: string): string {
  return value.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-|-$/g, '') || 'logic';
}

export function logicBranchResourceRid(logicFileId: string, branchId: string): string {
  return `ri.openfoundry.logic-branch.${branchSafeId(branchId)}.${branchSafeId(logicFileId)}`;
}

function branchOperation(operation: LogicBranchAdapterOperation, actor: string, now: Date, detail: string): LogicBranchOperationLogEntry {
  return {
    operation,
    actor,
    atIso: now.toISOString(),
    detail,
  };
}

function isolatedBranchVersion(
  resource: Pick<LogicBranchAdapterResource, 'branchId' | 'branchName' | 'logicFileId' | 'branchVersion'>,
  base: LogicVersionDefinition,
  head: LogicVersionDefinition,
  author: string,
  now: Date,
): LogicSavedVersion {
  const versionNumber = resource.branchVersion.versionNumber + 1;
  return {
    ...createLogicSavedVersion(base, head, author, now, versionNumber),
    id: `logic-branch-${branchSafeId(resource.branchId)}-${branchSafeId(resource.logicFileId)}-v${versionNumber}-${now.getTime().toString(36)}`,
  };
}

function pendingApprovals(reviews: LogicBranchReview[]): number {
  return reviews.filter((review) => review.status === 'pending').length;
}

function logicBranchFunctionRid(logicFileId: string, branchName: string): string {
  return `${logicFileId}@${branchSafeId(branchName)}`;
}

export function addLogicFileToBranch({
  branchId,
  branchName,
  logicFileId,
  mainVersion,
  actor,
  now = new Date(),
}: {
  branchId: string;
  branchName: string;
  logicFileId: string;
  mainVersion: LogicSavedVersion;
  actor: string;
  now?: Date;
}): LogicBranchAdapterResource {
  const branchVersion: LogicSavedVersion = {
    ...createLogicSavedVersion(mainVersion.definition, mainVersion.definition, actor, now, mainVersion.versionNumber + 1),
    id: `logic-branch-${branchSafeId(branchId)}-${branchSafeId(logicFileId)}-v${mainVersion.versionNumber + 1}-${now.getTime().toString(36)}`,
  };
  return {
    resourceRid: logicBranchResourceRid(logicFileId, branchId),
    logicFileId,
    branchId,
    branchName,
    status: 'active',
    mainBaseVersion: mainVersion,
    mainCurrentVersion: mainVersion,
    branchVersion,
    pendingApprovalCount: 0,
    rebaseRequired: false,
    conflicts: [],
    operations: [branchOperation('add', actor, now, `Added ${logicFileId} to ${branchName}`)],
    addedAtIso: now.toISOString(),
    updatedAtIso: now.toISOString(),
  };
}

export function editLogicFileOnBranch(
  resource: LogicBranchAdapterResource,
  nextDefinition: LogicVersionDefinition,
  actor: string,
  now = new Date(),
): LogicBranchAdapterResource {
  const branchVersion = isolatedBranchVersion(resource, resource.branchVersion.definition, nextDefinition, actor, now);
  return {
    ...resource,
    status: 'active',
    branchVersion,
    publication: undefined,
    removalBlockedReason: undefined,
    rebaseRequired: resource.rebaseRequired || resource.mainBaseVersion.id !== resource.mainCurrentVersion.id,
    updatedAtIso: now.toISOString(),
    operations: [...resource.operations, branchOperation('edit', actor, now, `Saved branch draft v${branchVersion.versionNumber}`)],
  };
}

export function publishLogicVersionOnBranch(
  resource: LogicBranchAdapterResource,
  actor: string,
  now = new Date(),
): LogicBranchAdapterResource {
  const branchVersion: LogicSavedVersion = {
    ...resource.branchVersion,
    status: 'published',
    publishedAtIso: now.toISOString(),
  };
  return {
    ...resource,
    status: 'active',
    branchVersion,
    publication: {
      functionRid: logicBranchFunctionRid(resource.logicFileId, resource.branchName),
      versionId: branchVersion.id,
      versionNumber: branchVersion.versionNumber,
      tag: 'Branched pre-release',
      availableOnBranchId: resource.branchId,
      callableSurfaces: ['workshop', 'function_backed_actions', 'branch_ontology_objects'],
    },
    removalBlockedReason: undefined,
    updatedAtIso: now.toISOString(),
    operations: [...resource.operations, branchOperation('publish', actor, now, `Published ${branchVersion.id} as a branched pre-release`)],
  };
}

export function logicBranchFunctionAvailable(resource: LogicBranchAdapterResource, branchId: string): boolean {
  return resource.status === 'active' && Boolean(resource.publication) && resource.branchId === branchId;
}

export function removeLogicFileFromBranch(
  resource: LogicBranchAdapterResource,
  actor: string,
  now = new Date(),
): LogicBranchAdapterResource {
  if (resource.publication) {
    return {
      ...resource,
      removalBlockedReason: 'Published Logic functions cannot be deleted while on a branch.',
      updatedAtIso: now.toISOString(),
      operations: [...resource.operations, branchOperation('remove', actor, now, 'Removal blocked because the branch version is published')],
    };
  }
  return {
    ...resource,
    status: 'removed',
    removedAtIso: now.toISOString(),
    removalBlockedReason: undefined,
    updatedAtIso: now.toISOString(),
    operations: [...resource.operations, branchOperation('remove', actor, now, `Removed ${resource.logicFileId} from ${resource.branchName}`)],
  };
}

export function requestLogicBranchReview(
  resource: LogicBranchAdapterResource,
  reviewers: Array<Pick<LogicBranchReview, 'reviewerId' | 'reviewerName'>>,
  actor: string,
  now = new Date(),
): LogicBranchAdapterResource {
  const reviews = reviewers.map((reviewer) => ({
    ...reviewer,
    status: 'pending' as const,
  }));
  const proposal: LogicBranchProposal = {
    id: `logic-branch-proposal-${branchSafeId(resource.branchId)}-${now.getTime().toString(36)}`,
    status: reviews.length > 0 ? 'in_review' : 'draft',
    createdBy: actor,
    createdAtIso: now.toISOString(),
    reviews,
  };
  return {
    ...resource,
    proposal,
    pendingApprovalCount: pendingApprovals(reviews),
    updatedAtIso: now.toISOString(),
    operations: [...resource.operations, branchOperation('review', actor, now, `Requested ${reviews.length} review${reviews.length === 1 ? '' : 's'}`)],
  };
}

export function reviewLogicBranchProposal(
  resource: LogicBranchAdapterResource,
  review: Pick<LogicBranchReview, 'reviewerId' | 'reviewerName' | 'status' | 'comment'>,
  now = new Date(),
): LogicBranchAdapterResource {
  const existingProposal = resource.proposal ?? {
    id: `logic-branch-proposal-${branchSafeId(resource.branchId)}-${now.getTime().toString(36)}`,
    status: 'in_review' as const,
    createdBy: review.reviewerId,
    createdAtIso: now.toISOString(),
    reviews: [],
  };
  const decidedReview: LogicBranchReview = {
    reviewerId: review.reviewerId,
    reviewerName: review.reviewerName,
    status: review.status,
    comment: review.comment,
    decidedAtIso: review.status === 'pending' ? undefined : now.toISOString(),
  };
  const reviews = existingProposal.reviews.some((candidate) => candidate.reviewerId === review.reviewerId)
    ? existingProposal.reviews.map((candidate) => (candidate.reviewerId === review.reviewerId ? decidedReview : candidate))
    : [...existingProposal.reviews, decidedReview];
  const nextStatus: LogicBranchProposalStatus = reviews.some((candidate) => candidate.status === 'rejected')
    ? 'rejected'
    : pendingApprovals(reviews) === 0 && reviews.length > 0
      ? 'approved'
      : 'in_review';
  return {
    ...resource,
    proposal: {
      ...existingProposal,
      status: nextStatus,
      reviews,
    },
    pendingApprovalCount: pendingApprovals(reviews),
    updatedAtIso: now.toISOString(),
    operations: [...resource.operations, branchOperation('review', review.reviewerName, now, `${review.status} review for ${resource.logicFileId}`)],
  };
}

function changeLabel(change: Pick<LogicVersionComponentChange, 'changeType' | 'id'>): string {
  return `${change.changeType}:${change.id}`;
}

function componentConflictSet(
  component: LogicBranchRebaseConflict['component'],
  mainChanges: LogicVersionComponentChange[],
  branchChanges: LogicVersionComponentChange[],
): LogicBranchRebaseConflict[] {
  const branchById = new Map(branchChanges.map((change) => [change.id, change]));
  return mainChanges.flatMap((mainChange) => {
    const branchChange = branchById.get(mainChange.id);
    if (!branchChange) return [];
    return [{
      id: `${component}:${mainChange.id}`,
      component,
      componentId: mainChange.id,
      mainChange: changeLabel(mainChange),
      branchChange: changeLabel(branchChange),
      requiresManualResolution: true,
    }];
  });
}

function valueConflictSet(
  component: 'prompt' | 'model',
  mainChanges: LogicVersionValueChange[],
  branchChanges: LogicVersionValueChange[],
): LogicBranchRebaseConflict[] {
  const branchById = new Map(branchChanges.map((change) => [change.blockId, change]));
  return mainChanges.flatMap((mainChange) => {
    const branchChange = branchById.get(mainChange.blockId);
    if (!branchChange) return [];
    return [{
      id: `${component}:${mainChange.blockId}`,
      component,
      componentId: mainChange.blockId,
      mainChange: 'edited',
      branchChange: 'edited',
      requiresManualResolution: true,
    }];
  });
}

function detectLogicBranchRebaseConflicts(resource: LogicBranchAdapterResource, mainCurrentVersion: LogicSavedVersion): LogicBranchRebaseConflict[] {
  const mainChanges = compareLogicVersionDefinitions(resource.mainBaseVersion.definition, mainCurrentVersion.definition);
  const branchChanges = compareLogicVersionDefinitions(resource.mainBaseVersion.definition, resource.branchVersion.definition);
  return [
    ...componentConflictSet('input', mainChanges.inputs, branchChanges.inputs),
    ...componentConflictSet('block', mainChanges.blocks, branchChanges.blocks),
    ...componentConflictSet('output', mainChanges.outputs, branchChanges.outputs),
    ...valueConflictSet('prompt', mainChanges.promptChanges, branchChanges.promptChanges),
    ...valueConflictSet('model', mainChanges.modelChanges, branchChanges.modelChanges),
  ];
}

export function rebaseLogicFileOnBranch(
  resource: LogicBranchAdapterResource,
  mainCurrentVersion: LogicSavedVersion,
  actor: string,
  now = new Date(),
  resolution: LogicBranchRebaseResolution = {},
): LogicBranchAdapterResource {
  const conflicts = detectLogicBranchRebaseConflicts(resource, mainCurrentVersion);
  const blockedByConflicts = conflicts.length > 0 && !resolution.acceptManualResolution;
  return {
    ...resource,
    mainCurrentVersion,
    mainBaseVersion: blockedByConflicts ? resource.mainBaseVersion : mainCurrentVersion,
    rebaseRequired: blockedByConflicts,
    conflicts: blockedByConflicts ? conflicts : [],
    updatedAtIso: now.toISOString(),
    operations: [
      ...resource.operations,
      branchOperation('rebase', actor, now, blockedByConflicts
        ? `Rebase requires manual resolution for ${conflicts.length} conflict${conflicts.length === 1 ? '' : 's'}`
        : `Rebased onto main v${mainCurrentVersion.versionNumber}${resolution.notes ? `: ${resolution.notes}` : ''}`),
    ],
  };
}

function outputApiNameIssues(outputs: LogicOutputDefinition[]): LogicIssue[] {
  const issues: LogicIssue[] = [];
  if (!outputs.some((output) => output.final)) {
    issues.push({ severity: 'error', field: 'outputs.final', message: 'At least one final Logic function output is required.' });
  }
  const seen = new Set<string>();
  for (const output of outputs) {
    if (!output.name.trim()) {
      issues.push({ severity: 'error', field: `outputs.${output.id}.name`, message: 'Output display name is required.' });
    }
    if (!isValidLogicInputApiName(output.apiName)) {
      issues.push({ severity: 'error', field: `outputs.${output.id}.apiName`, message: 'Output API name must start with a letter and contain only letters, numbers, and underscores.' });
    }
    if (!LOGIC_OUTPUT_VALUE_TYPES.has(output.outputType)) {
      issues.push({ severity: 'error', field: `outputs.${output.id}.outputType`, message: 'Logic outputs cannot return model variables or unsupported local value types.' });
    }
    const normalized = output.apiName.toLowerCase();
    if (seen.has(normalized)) {
      issues.push({ severity: 'error', field: `outputs.${output.apiName}`, message: 'Output API names must be unique.' });
    }
    seen.add(normalized);
  }
  return issues;
}

export function validateLogicBranchPublishableState(definition: LogicVersionDefinition): LogicIssue[] {
  return [
    ...definition.inputs.flatMap((input) => validateLogicInputDefinition(input).map((issue): LogicIssue => ({
      severity: 'error',
      field: `inputs.${input.apiName || input.id}.${issue.field}`,
      message: issue.message,
    }))),
    ...definition.blocks.flatMap((definitionBlock) => {
      const llmBlock = logicLlmBlockFromDefinitionBlock(definitionBlock);
      return llmBlock ? validateLlmBlock(llmBlock, definition.inputs).filter((issue) => issue.severity === 'error') : [];
    }),
    ...outputApiNameIssues(definition.outputs),
  ];
}

export function getLogicBranchMergeReadiness(resource: LogicBranchAdapterResource): LogicBranchMergeReadiness {
  const publishableIssues = validateLogicBranchPublishableState(resource.branchVersion.definition);
  const checks: LogicBranchMergeCheck[] = [
    {
      id: 'resource_present',
      label: 'Resource is on branch',
      status: resource.status === 'active' ? 'passed' : 'blocked',
      message: resource.status === 'active' ? 'Logic file is active on this branch.' : 'Removed Logic files cannot be merged.',
    },
    {
      id: 'published_on_branch',
      label: 'Published on branch',
      status: resource.publication && resource.branchVersion.status === 'published' ? 'passed' : 'blocked',
      message: resource.publication ? `${resource.publication.functionRid} is tagged Branched pre-release.` : 'Publish the branch version before merge.',
    },
    {
      id: 'up_to_date_with_main',
      label: 'Up to date with main',
      status: !resource.rebaseRequired && resource.mainBaseVersion.id === resource.mainCurrentVersion.id ? 'passed' : 'blocked',
      message: !resource.rebaseRequired && resource.mainBaseVersion.id === resource.mainCurrentVersion.id
        ? `Rebased against main v${resource.mainCurrentVersion.versionNumber}.`
        : 'Rebase this Logic file against latest main before merge.',
    },
    {
      id: 'publishable_state',
      label: 'Publishable state',
      status: publishableIssues.length === 0 ? 'passed' : 'blocked',
      message: publishableIssues.length === 0 ? 'No publish-blocking Logic errors were found.' : `${publishableIssues.length} publish-blocking issue${publishableIssues.length === 1 ? '' : 's'} found.`,
      issues: publishableIssues,
    },
    {
      id: 'no_pending_approvals',
      label: 'No pending approvals',
      status: resource.pendingApprovalCount === 0 && resource.proposal?.status !== 'rejected' ? 'passed' : 'blocked',
      message: resource.proposal?.status === 'rejected'
        ? 'Rejected reviews must be addressed before merge.'
        : resource.pendingApprovalCount === 0
          ? 'No pending reviewer approvals remain.'
          : `${resource.pendingApprovalCount} reviewer approval${resource.pendingApprovalCount === 1 ? '' : 's'} pending.`,
    },
  ];
  return {
    mergeable: checks.every((check) => check.status === 'passed'),
    checks,
  };
}

export function mergeLogicFileBranch(
  resource: LogicBranchAdapterResource,
  actor: string,
  now = new Date(),
): LogicBranchMergeResult {
  const readiness = getLogicBranchMergeReadiness(resource);
  if (!readiness.mergeable || !resource.publication) {
    return {
      merged: false,
      resource,
      checks: readiness.checks,
      errors: readiness.checks.filter((check) => check.status === 'blocked').map((check) => check.message),
    };
  }
  const mergedMainVersion: LogicSavedVersion = {
    id: `logic-version-main-${branchSafeId(resource.logicFileId)}-v${resource.mainCurrentVersion.versionNumber + 1}-${now.getTime().toString(36)}`,
    versionNumber: resource.mainCurrentVersion.versionNumber + 1,
    author: actor,
    createdAtIso: now.toISOString(),
    status: 'published',
    definition: resource.branchVersion.definition,
    changeSummary: compareLogicVersionDefinitions(resource.mainCurrentVersion.definition, resource.branchVersion.definition),
    publishedAtIso: now.toISOString(),
  };
  const nextProposal = resource.proposal ? { ...resource.proposal, status: 'merged' as const } : resource.proposal;
  const mergedResource: LogicBranchAdapterResource = {
    ...resource,
    status: 'merged',
    proposal: nextProposal,
    mainBaseVersion: mergedMainVersion,
    mainCurrentVersion: mergedMainVersion,
    pendingApprovalCount: 0,
    rebaseRequired: false,
    conflicts: [],
    mergedAtIso: now.toISOString(),
    updatedAtIso: now.toISOString(),
    operations: [...resource.operations, branchOperation('merge', actor, now, `Merged branch pre-release into main v${mergedMainVersion.versionNumber}`)],
  };
  return {
    merged: true,
    resource: mergedResource,
    checks: readiness.checks,
    mergedMainVersion,
    errors: [],
  };
}

export type LogicUsageSurfaceStatus = 'available' | 'blocked' | 'requires_publish';
export type LogicUsageSurfaceId = 'workshop' | 'action_workflow' | 'logic_function' | 'function_on_objects' | 'automate' | 'api_curl';

export interface LogicUsageSnippet {
  language: 'json' | 'typescript' | 'bash';
  label: string;
  body: string;
}

export interface LogicUsageSurface {
  id: LogicUsageSurfaceId;
  label: string;
  description: string;
  href: string;
  status: LogicUsageSurfaceStatus;
  blockedReason?: string;
  requirements: string[];
  snippet?: LogicUsageSnippet;
}

export interface LogicUsageBundle {
  published: boolean;
  functionRid?: string;
  publishedVersionNumber?: number;
  returnsOntologyEdits: boolean;
  surfaces: LogicUsageSurface[];
  actionTypeDraft?: LogicBackedActionTypeDraft;
}

export type LogicBackedActionExecutionSurface =
  | 'workshop_action_execution'
  | 'branch_preview'
  | 'approved_automation'
  | 'logic_preview'
  | 'api';

export type LogicBackedActionApplyMode =
  | 'action_execution'
  | 'branch_preview'
  | 'approved_automation'
  | 'preview_only'
  | 'blocked';

export interface LogicBackedActionExecutionContext {
  surface: LogicBackedActionExecutionSurface;
  branchName?: string;
  actionExecutionId?: string;
  approvedAutomationProposalId?: string;
}

export interface LogicBackedActionEditPolicy {
  canApplyOntologyEdits: boolean;
  applyMode: LogicBackedActionApplyMode;
  reason: string;
}

export interface LogicBackedActionInputField {
  name: string;
  display_name?: string;
  description?: string;
  property_type: string;
  required: boolean;
  default_value?: unknown;
}

export interface LogicBackedActionTypeDraft {
  name: string;
  displayName: string;
  description: string;
  objectTypeId: string;
  operationKind: 'invoke_function';
  functionRid: string;
  publishedVersionNumber: number;
  ontologyEditOutputApiName?: string;
  inputSchema: LogicBackedActionInputField[];
  formSchema: {
    sections: Array<{
      id: string;
      title: string;
      parameter_names: string[];
    }>;
  };
  config: Record<string, unknown>;
  authorizationPolicy: Record<string, unknown>;
  confirmationRequired: boolean;
  permissionKey: string;
  workshopButton: {
    action_id: string;
    label: string;
    default_values: Record<string, unknown>;
    execution_context: LogicBackedActionExecutionContext;
  };
  branchPreview: {
    enabled: boolean;
    branchName: string;
    execution_context: LogicBackedActionExecutionContext;
  };
  guardrails: string[];
  createActionTypeBody: Record<string, unknown>;
  href: string;
}

export type LogicAutomationEditMode = 'auto_apply' | 'stage_for_review';
export type LogicAutomationStatus = 'draft' | 'active' | 'paused';
export type LogicAutomationProposalStatus = 'open' | 'applied' | 'rejected';

export interface LogicAutomationDraft {
  id: string;
  name: string;
  source: 'logic_uses_sidebar' | 'automate_app';
  status: LogicAutomationStatus;
  functionRid: string;
  publishedVersionNumber: number;
  editMode: LogicAutomationEditMode;
  ontologyEditOutputApiName: string;
  actionTypeId: string;
  objectInputApiName?: string;
  objectTypeId?: string;
  trigger: {
    type: 'object_set_new_object' | 'manual';
    objectSetRid?: string;
    eventName: string;
  };
  logicEffect: {
    type: 'logic_effect';
    functionRid: string;
    inputMappings: Record<string, string>;
    outputApiName: string;
    editMode: LogicAutomationEditMode;
  };
  workflowPayload: {
    name: string;
    trigger_type: 'event' | 'manual';
    trigger_config: Record<string, unknown>;
    steps: Array<{
      id: string;
      name: string;
      step_type: 'logic_effect' | 'submit_action' | 'approval';
      config: Record<string, unknown>;
      next_step_id: string | null;
      branches: Array<{ condition: Record<string, unknown>; next_step_id: string }>;
    }>;
  };
  proposalVisibilityHours: number;
  href: string;
}

export interface LogicAutomationEventBucket {
  label: string;
  triggered: number;
  staged: number;
  applied: number;
  failed: number;
}

export interface LogicAutomationDecisionLogEntry {
  id: string;
  atIso: string;
  actor: string;
  event: string;
  detail: string;
}

export interface LogicAutomationProposal {
  id: string;
  automationId: string;
  status: LogicAutomationProposalStatus;
  createdBy: string;
  createdAtIso: string;
  expiresAtIso: string;
  summary: string;
  reason: string;
  logicRunId: string;
  actionTypeId: string;
  targetObjectId: string;
  parameters: Record<string, unknown>;
  proposedActionPreview: {
    actionTypeId: string;
    targetObjectId: string;
    parameters: Record<string, unknown>;
    applyMode: LogicAutomationEditMode;
  };
  decisionLog: LogicAutomationDecisionLogEntry[];
}

export type LogicPermissionExecutionMode = 'user_scoped' | 'project_scoped';
export type LogicLogVisibility = 'initiating_user' | 'project_viewers';

export interface LogicExecutionModePolicy {
  mode: LogicPermissionExecutionMode;
  permissionSubject: 'initiating_user' | 'project';
  logVisibility: LogicLogVisibility;
  retentionHours: number | null;
  retainedUntilLabel: string;
  runHistoryMaxRows?: number;
}

export interface LogicRunHistoryRecord {
  id: string;
  actorId: string;
  actorName: string;
  executionMode: LogicPermissionExecutionMode;
  status: LogicRunStatus;
  invocationSurface: string;
  startedAtIso: string;
  retentionExpiresAtIso: string;
  durationMs: number;
  errorMessage?: string;
  failureCategory?: string;
  runHistoryDatasetRid?: string;
  inputs?: Record<string, unknown>;
  outputs?: Record<string, unknown>;
  intermediateParameters?: Record<string, unknown>;
  model?: string;
  branchName?: string;
  publishedVersionId?: string;
  publishedVersionNumber?: number;
  completedAtIso?: string;
  serviceContext?: LogicRunHistoryServiceContext;
  traceRefs?: LogicRunHistoryTraceReference[];
  computeUsage?: LogicComputeUsageSummary;
}

export interface LogicRunHistoryTraceReference {
  id: string;
  kind: 'debugger' | 'logs' | 'lineage' | 'metrics';
  href: string;
  visibility: LogicLogVisibility;
}

export interface LogicRunHistoryServiceContext {
  invocationSurface: string;
  permissionSubject: 'initiating_user' | 'project';
  permissionSubjectId: string;
  initiatingUserId: string;
  projectId?: string;
  logsVisibleTo: LogicLogVisibility;
}

export interface LogicRunHistoryDatasetColumn {
  name: string;
  type: string;
  permissionScoped: boolean;
}

export interface LogicRunHistoryDatasetConfig {
  projectId: string;
  datasetRid: string;
  maxRows: number;
  visibleTo: LogicLogVisibility;
  writeMode: 'append_and_prune';
  schema: LogicRunHistoryDatasetColumn[];
}

export interface LogicRunHistoryDatasetRow {
  datasetRid: string;
  runId: string;
  functionRid: string;
  status: LogicRunStatus;
  inputs: Record<string, unknown>;
  outputs: Record<string, unknown>;
  intermediateParameters: Record<string, unknown>;
  errorMessage?: string;
  durationMs: number;
  model?: string;
  branchName: string;
  publishedVersionId?: string;
  publishedVersionNumber?: number;
  actorId: string;
  actorName: string;
  serviceContext: LogicRunHistoryServiceContext;
  traceRefs: LogicRunHistoryTraceReference[];
  computeUsage?: LogicComputeUsageSummary;
  startedAtIso: string;
  completedAtIso: string;
  visibleTo: LogicLogVisibility;
}

export type LogicProjectScopedResourceKind = 'object_type' | 'action_type' | 'function' | 'media_set' | 'model' | 'dataset';

export interface LogicProjectScopedResourceRequirement {
  id: string;
  label: string;
  kind: LogicProjectScopedResourceKind;
  source: string;
  imported: boolean;
  markingAccess: boolean;
  markings: string[];
}

export interface LogicProjectScopedValidation {
  ready: boolean;
  issues: LogicIssue[];
  missingImports: LogicProjectScopedResourceRequirement[];
  missingMarkingAccess: LogicProjectScopedResourceRequirement[];
}

export type LogicSecurityAction = 'view' | 'edit' | 'manage' | 'invoke';
export type LogicSecurityResourceKind = LogicProjectScopedResourceKind | 'result_dataset';

export interface LogicFileSecurityPolicy {
  ownerIds: string[];
  managerIds: string[];
  editorIds: string[];
  viewerIds: string[];
  invokerIds: string[];
  allowedObjectTypes: string[];
  readablePropertiesByObjectType: Record<string, string[]>;
  allowedActionTypes: string[];
  allowedFunctionRids: string[];
  allowedMediaSetRids: string[];
  allowedResultDatasetRids: string[];
  projectImportedResourceIds: string[];
  markingAccessibleResourceIds: string[];
  sensitivePropertiesByObjectType?: Record<string, string[]>;
  broadObjectAccessThreshold?: number;
  promptReviewRequired?: boolean;
  redactionPolicyId?: string;
  modelAllowlist?: string[];
  exportLoggingRestricted?: boolean;
}

export interface LogicPermissionDecision {
  actorId: string;
  action: LogicSecurityAction;
  allowed: boolean;
  reason: string;
}

export interface LogicSecurityResourceExposure {
  kind: LogicSecurityResourceKind;
  id: string;
  source: string;
  properties: string[];
  llmAccessible: boolean;
  explicitlyConfigured: boolean;
  permissioned: boolean;
  importedIntoProject: boolean;
  markingAccess: boolean;
}

export interface LogicSecurityGuardrailHook {
  id: 'redaction' | 'prompt_review' | 'model_allowlist' | 'export_logging_restriction';
  label: string;
  enabled: boolean;
  detail: string;
}

export interface LogicSecurityMinimizationWarning {
  severity: 'warning';
  field: string;
  message: string;
  resourceId?: string;
  properties?: string[];
}

export interface LogicSecurityExposureInventory {
  prompts: Array<{ blockId: string; blockName: string; variableRefs: string[]; modelBinding: LogicLlmBlockConfig['modelBinding'] }>;
  objectTypes: LogicSecurityResourceExposure[];
  actions: LogicSecurityResourceExposure[];
  functions: LogicSecurityResourceExposure[];
  mediaReferences: LogicSecurityResourceExposure[];
  resultDatasets: LogicSecurityResourceExposure[];
}

export interface LogicSecurityBoundary {
  ready: boolean;
  executionMode: LogicPermissionExecutionMode;
  permissionSubject: 'initiating_user' | 'project';
  permissionSubjectId: string;
  resources: LogicSecurityResourceExposure[];
  llmAccessibleResourceIds: string[];
  issues: LogicIssue[];
  exposureInventory: LogicSecurityExposureInventory;
  minimizationWarnings: LogicSecurityMinimizationWarning[];
  guardrailHooks: LogicSecurityGuardrailHook[];
}

export type LogicMetricsWindow = '24h' | '7d' | '30d' | '90d';

export interface LogicFailureCategoryMetric {
  category: string;
  count: number;
}

export interface LogicOperationalHealthMetric {
  id: 'failure_rate' | 'p95_duration' | 'token_compute_usage' | 'tool_failures' | 'action_failures' | 'object_query_failures' | 'model_unavailability' | 'run_history_dataset_failures' | 'automation_proposal_backlog';
  label: string;
  value: number;
  unit: 'percent' | 'milliseconds' | 'compute_seconds' | 'tokens' | 'count';
  status: 'healthy' | 'warning' | 'critical';
  threshold?: number;
}

export interface LogicOperationalHealthSurface {
  id: 'logic_detail' | 'workflow_lineage' | 'data_health' | 'project_dashboard';
  label: string;
  href: string;
  visible: boolean;
  metricIds: LogicOperationalHealthMetric['id'][];
}

export interface LogicOperationalHealthSummary {
  status: 'healthy' | 'warning' | 'critical';
  metrics: LogicOperationalHealthMetric[];
  surfaces: LogicOperationalHealthSurface[];
}

export interface LogicMetricsSummary {
  window: LogicMetricsWindow;
  windowStartIso: string;
  windowEndIso: string;
  successCount: number;
  failureCount: number;
  failureCategories: LogicFailureCategoryMetric[];
  recentRuns: LogicRunHistoryRecord[];
  p95DurationMs: number | null;
  viewerPermissionRequired: boolean;
  failureRate: number;
  totalPromptTokensEstimate: number;
  totalComputeSeconds: number;
  toolFailureCount: number;
  actionFailureCount: number;
  objectQueryFailureCount: number;
  modelUnavailableCount: number;
  runHistoryDatasetFailureCount: number;
  automationProposalBacklog: number;
  operationalHealth: LogicOperationalHealthSummary;
}

export function logicExecutionModePolicy(mode: LogicPermissionExecutionMode): LogicExecutionModePolicy {
  if (mode === 'project_scoped') {
    return {
      mode,
      permissionSubject: 'project',
      logVisibility: 'project_viewers',
      retentionHours: null,
      retainedUntilLabel: 'run history dataset',
      runHistoryMaxRows: 10000,
    };
  }
  return {
    mode: 'user_scoped',
    permissionSubject: 'initiating_user',
    logVisibility: 'initiating_user',
    retentionHours: 24,
    retainedUntilLabel: '24 hours',
  };
}

export function logicProjectScopedRunHistoryDatasetRid(projectId: string) {
  const suffix = projectId.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-|-$/g, '') || 'project';
  return `ri.foundry.dataset.logic-run-history.${suffix}`;
}

export function createLogicRunHistoryDatasetConfig(
  projectId: string,
  overrides: Partial<Pick<LogicRunHistoryDatasetConfig, 'datasetRid' | 'maxRows'>> = {},
): LogicRunHistoryDatasetConfig {
  const maxRows = Math.max(1, Math.floor(overrides.maxRows ?? logicExecutionModePolicy('project_scoped').runHistoryMaxRows ?? 10000));
  return {
    projectId,
    datasetRid: overrides.datasetRid?.trim() || logicProjectScopedRunHistoryDatasetRid(projectId),
    maxRows,
    visibleTo: 'project_viewers',
    writeMode: 'append_and_prune',
    schema: [
      { name: 'run_id', type: 'string', permissionScoped: false },
      { name: 'function_rid', type: 'string', permissionScoped: false },
      { name: 'status', type: 'string', permissionScoped: false },
      { name: 'inputs', type: 'json', permissionScoped: true },
      { name: 'outputs', type: 'json', permissionScoped: true },
      { name: 'intermediate_parameters', type: 'json', permissionScoped: true },
      { name: 'error_message', type: 'string', permissionScoped: true },
      { name: 'duration_ms', type: 'integer', permissionScoped: false },
      { name: 'model', type: 'string', permissionScoped: true },
      { name: 'branch_name', type: 'string', permissionScoped: false },
      { name: 'published_version_id', type: 'string', permissionScoped: false },
      { name: 'user_service_context', type: 'json', permissionScoped: true },
      { name: 'trace_refs', type: 'json', permissionScoped: true },
      { name: 'compute_usage', type: 'json', permissionScoped: true },
    ],
  };
}

function securityResourceKey(kind: LogicSecurityResourceKind, id: string) {
  return `${kind}:${id}`;
}

function securityContains(values: string[], kind: LogicSecurityResourceKind, id: string) {
  return values.includes(id) || values.includes(securityResourceKey(kind, id));
}

function actionAllowedByRole(policy: LogicFileSecurityPolicy, actorId: string, action: LogicSecurityAction): boolean {
  const isOwner = policy.ownerIds.includes(actorId);
  const isManager = isOwner || policy.managerIds.includes(actorId);
  const isEditor = isManager || policy.editorIds.includes(actorId);
  const isViewer = isEditor || policy.viewerIds.includes(actorId);
  const isInvoker = isEditor || policy.invokerIds.includes(actorId);
  if (action === 'manage') return isManager;
  if (action === 'edit') return isEditor;
  if (action === 'view') return isViewer;
  return isInvoker;
}

export function logicFilePermissionDecision(
  policy: LogicFileSecurityPolicy,
  actorId: string,
  action: LogicSecurityAction,
): LogicPermissionDecision {
  const allowed = actionAllowedByRole(policy, actorId, action);
  const reason = allowed
    ? action === 'invoke'
      ? 'Actor has Logic function invocation permission.'
      : `Actor can ${action} this Logic file.`
    : action === 'invoke'
      ? 'Published Logic invocation requires owner, manager, editor, or invoker permission.'
      : `Actor lacks ${action} permission on this Logic file.`;
  return { actorId, action, allowed, reason };
}

function addSecurityExposure(
  resources: Map<string, LogicSecurityResourceExposure>,
  exposure: LogicSecurityResourceExposure,
) {
  if (!exposure.id.trim()) return;
  const key = securityResourceKey(exposure.kind, exposure.id);
  const existing = resources.get(key);
  if (!existing) {
    resources.set(key, exposure);
    return;
  }
  resources.set(key, {
    ...existing,
    source: Array.from(new Set([...existing.source.split(', '), exposure.source])).join(', '),
    properties: Array.from(new Set([...existing.properties, ...exposure.properties])).sort(),
    llmAccessible: existing.llmAccessible || exposure.llmAccessible,
    explicitlyConfigured: existing.explicitlyConfigured && exposure.explicitlyConfigured,
    permissioned: existing.permissioned && exposure.permissioned,
    importedIntoProject: existing.importedIntoProject && exposure.importedIntoProject,
    markingAccess: existing.markingAccess && exposure.markingAccess,
  });
}

function blockToolsFromDefinition(definition: LogicVersionDefinition, llmBlocks?: LogicLlmBlockConfig[]): LogicLlmBlockConfig[] {
  return llmBlocks ?? definition.blocks
    .map(logicLlmBlockFromDefinitionBlock)
    .filter((block): block is LogicLlmBlockConfig => Boolean(block));
}


function sensitivePropertiesFor(policy: LogicFileSecurityPolicy, objectTypeId: string): Set<string> {
  return new Set(policy.sensitivePropertiesByObjectType?.[objectTypeId] ?? []);
}

function logicSecurityGuardrailHooks(policy: LogicFileSecurityPolicy): LogicSecurityGuardrailHook[] {
  return [
    {
      id: 'redaction',
      label: 'Redaction policy',
      enabled: Boolean(policy.redactionPolicyId),
      detail: policy.redactionPolicyId ? `Apply ${policy.redactionPolicyId} before prompt/tool logging.` : 'No local redaction policy configured.',
    },
    {
      id: 'prompt_review',
      label: 'Prompt review',
      enabled: Boolean(policy.promptReviewRequired),
      detail: policy.promptReviewRequired ? 'Prompt changes require governance review before publication.' : 'Prompt review is not required by local governance.',
    },
    {
      id: 'model_allowlist',
      label: 'Model allowlist',
      enabled: Boolean(policy.modelAllowlist?.length),
      detail: policy.modelAllowlist?.length ? `Allowed models: ${policy.modelAllowlist.join(', ')}.` : 'Any permissioned model binding may be selected.',
    },
    {
      id: 'export_logging_restriction',
      label: 'Export/logging restrictions',
      enabled: Boolean(policy.exportLoggingRestricted),
      detail: policy.exportLoggingRestricted ? 'Prompt, output, and run-history exports are restricted.' : 'No local export/logging restriction configured.',
    },
  ];
}

export function buildLogicSecurityBoundary({
  definition,
  policy,
  executionMode = 'user_scoped',
  permissionSubjectId,
  llmBlocks,
  resultDatasetRid,
}: {
  definition: LogicVersionDefinition;
  policy: LogicFileSecurityPolicy;
  executionMode?: LogicPermissionExecutionMode;
  permissionSubjectId: string;
  llmBlocks?: LogicLlmBlockConfig[];
  resultDatasetRid?: string;
}): LogicSecurityBoundary {
  const resources = new Map<string, LogicSecurityResourceExposure>();
  const issues: LogicIssue[] = [];
  const minimizationWarnings: LogicSecurityMinimizationWarning[] = [];
  const promptInventory = blockToolsFromDefinition(definition, llmBlocks).map((block) => ({
    blockId: block.id,
    blockName: block.name,
    variableRefs: block.promptVariableRefs,
    modelBinding: block.modelBinding,
  }));
  const projectScoped = executionMode === 'project_scoped';
  const importReady = (kind: LogicSecurityResourceKind, id: string) => !projectScoped || securityContains(policy.projectImportedResourceIds, kind, id);
  const markingReady = (kind: LogicSecurityResourceKind, id: string) => !projectScoped || securityContains(policy.markingAccessibleResourceIds, kind, id);

  definition.inputs.forEach((input) => {
    if (['object', 'object_list', 'object_set'].includes(input.type) && input.objectTypeId) {
      const permissioned = policy.allowedObjectTypes.includes(input.objectTypeId);
      addSecurityExposure(resources, {
        kind: 'object_type',
        id: input.objectTypeId,
        source: `input ${input.apiName}`,
        properties: [],
        llmAccessible: true,
        explicitlyConfigured: true,
        permissioned,
        importedIntoProject: importReady('object_type', input.objectTypeId),
        markingAccess: markingReady('object_type', input.objectTypeId),
      });
      if (!permissioned) issues.push({ severity: 'error', field: `inputs.${input.apiName}.objectTypeId`, message: `${input.objectTypeId} is not permissioned for Logic input access.` });
    }
    if (input.type === 'media_reference' && input.mediaSetRid) {
      const permissioned = policy.allowedMediaSetRids.includes(input.mediaSetRid);
      addSecurityExposure(resources, {
        kind: 'media_set',
        id: input.mediaSetRid,
        source: `input ${input.apiName}`,
        properties: [],
        llmAccessible: true,
        explicitlyConfigured: true,
        permissioned,
        importedIntoProject: importReady('media_set', input.mediaSetRid),
        markingAccess: markingReady('media_set', input.mediaSetRid),
      });
      if (!permissioned) issues.push({ severity: 'error', field: `inputs.${input.apiName}.mediaSetRid`, message: `${input.mediaSetRid} is not permissioned for media input access.` });
    }
  });

  blockToolsFromDefinition(definition, llmBlocks).forEach((block) => {
    block.toolAccess.forEach((tool) => {
      if (tool.kind === 'query_objects') {
        const readableProperties = new Set(tool.readablePropertiesByObjectType[tool.objectTypeId] ?? []);
        const policyProperties = new Set(policy.readablePropertiesByObjectType[tool.objectTypeId] ?? []);
        const explicitlyConfigured = tool.readableObjectTypeIds.includes(tool.objectTypeId)
          && tool.selectedProperties.every((property) => readableProperties.has(property));
        const permissioned = policy.allowedObjectTypes.includes(tool.objectTypeId)
          && tool.selectedProperties.every((property) => policyProperties.has(property));
        addSecurityExposure(resources, {
          kind: 'object_type',
          id: tool.objectTypeId,
          source: `tool ${tool.name}`,
          properties: tool.selectedProperties,
          llmAccessible: true,
          explicitlyConfigured,
          permissioned,
          importedIntoProject: importReady('object_type', tool.objectTypeId),
          markingAccess: markingReady('object_type', tool.objectTypeId),
        });
        if (!explicitlyConfigured) issues.push({ severity: 'error', field: `tools.${tool.name}.selectedProperties`, message: `${tool.name} exposes object data that is not explicitly configured as readable.` });
        if (!permissioned) issues.push({ severity: 'error', field: `tools.${tool.name}.permissions`, message: `${tool.name} requests object data outside the actor/project permission boundary.` });
        const broadThreshold = policy.broadObjectAccessThreshold ?? 25;
        if (tool.maxObjects > broadThreshold) {
          minimizationWarnings.push({
            severity: 'warning',
            field: `tools.${tool.name}.maxObjects`,
            message: `${tool.name} can expose up to ${tool.maxObjects} ${tool.objectTypeId} objects to an LLM; narrow filters or lower the limit.`,
            resourceId: tool.objectTypeId,
          });
        }
        const sensitiveProperties = tool.selectedProperties.filter((property) => sensitivePropertiesFor(policy, tool.objectTypeId).has(property));
        if (sensitiveProperties.length > 0) {
          minimizationWarnings.push({
            severity: 'warning',
            field: `tools.${tool.name}.selectedProperties`,
            message: `${tool.name} exposes sensitive ${tool.objectTypeId} properties to an LLM; apply redaction or remove them from the tool context.`,
            resourceId: tool.objectTypeId,
            properties: sensitiveProperties,
          });
        }
      }
      if (tool.kind === 'apply_action') {
        const explicitlyConfigured = tool.allowedActionTypeIds.includes(tool.actionTypeId);
        const permissioned = policy.allowedActionTypes.includes(tool.actionTypeId);
        addSecurityExposure(resources, {
          kind: 'action_type',
          id: tool.actionTypeId,
          source: `tool ${tool.name}`,
          properties: [],
          llmAccessible: true,
          explicitlyConfigured,
          permissioned,
          importedIntoProject: importReady('action_type', tool.actionTypeId),
          markingAccess: markingReady('action_type', tool.actionTypeId),
        });
        if (!explicitlyConfigured || !permissioned) issues.push({ severity: 'error', field: `tools.${tool.name}.actionTypeId`, message: `${tool.actionTypeId} is not available to this Logic function.` });
      }
      if (tool.kind === 'execute_function') {
        const explicitlyConfigured = tool.allowedFunctionRids.includes(tool.functionRid);
        const permissioned = policy.allowedFunctionRids.includes(tool.functionRid);
        addSecurityExposure(resources, {
          kind: 'function',
          id: tool.functionRid,
          source: `tool ${tool.name}`,
          properties: [],
          llmAccessible: true,
          explicitlyConfigured,
          permissioned,
          importedIntoProject: importReady('function', tool.functionRid),
          markingAccess: markingReady('function', tool.functionRid),
        });
        if (!explicitlyConfigured || !permissioned) issues.push({ severity: 'error', field: `tools.${tool.name}.functionRid`, message: `${tool.functionRid} is not available to this Logic function.` });
      }
    });
  });

  if (projectScoped && resultDatasetRid) {
    const permissioned = policy.allowedResultDatasetRids.includes(resultDatasetRid);
    addSecurityExposure(resources, {
      kind: 'result_dataset',
      id: resultDatasetRid,
      source: 'run history dataset',
      properties: [],
      llmAccessible: false,
      explicitlyConfigured: true,
      permissioned,
      importedIntoProject: true,
      markingAccess: true,
    });
    if (!permissioned) issues.push({ severity: 'error', field: 'runHistoryDatasetRid', message: `${resultDatasetRid} is not permissioned for project-scoped run history.` });
  }

  for (const resource of resources.values()) {
    if (projectScoped && resource.kind !== 'result_dataset') {
      if (!resource.importedIntoProject) issues.push({ severity: 'error', field: `${resource.source}.imported`, message: `${resource.kind} ${resource.id} must be imported into the Logic project.` });
      if (!resource.markingAccess) issues.push({ severity: 'error', field: `${resource.source}.markings`, message: `${resource.kind} ${resource.id} requires marking access.` });
    }
  }

  const resourceList = Array.from(resources.values()).sort((left, right) => left.kind.localeCompare(right.kind) || left.id.localeCompare(right.id));
  const byKind = (kind: LogicSecurityResourceKind) => resourceList.filter((resource) => resource.kind === kind);
  return {
    ready: issues.length === 0,
    executionMode,
    permissionSubject: executionMode === 'project_scoped' ? 'project' : 'initiating_user',
    permissionSubjectId,
    resources: resourceList,
    llmAccessibleResourceIds: resourceList.filter((resource) => resource.llmAccessible).map((resource) => securityResourceKey(resource.kind, resource.id)).sort(),
    issues,
    exposureInventory: {
      prompts: promptInventory,
      objectTypes: byKind('object_type'),
      actions: byKind('action_type'),
      functions: byKind('function'),
      mediaReferences: byKind('media_set'),
      resultDatasets: byKind('result_dataset'),
    },
    minimizationWarnings,
    guardrailHooks: logicSecurityGuardrailHooks(policy),
  };
}

export function buildLogicRunHistoryDatasetRow(
  run: LogicRunHistoryRecord,
  config: LogicRunHistoryDatasetConfig,
  options: {
    functionRid: string;
    branchName?: string;
    publishedVersionId?: string;
    publishedVersionNumber?: number;
  },
): LogicRunHistoryDatasetRow {
  const completedAtIso = run.completedAtIso ?? new Date(Date.parse(run.startedAtIso) + run.durationMs).toISOString();
  return {
    datasetRid: config.datasetRid,
    runId: run.id,
    functionRid: options.functionRid,
    status: run.status,
    inputs: run.inputs ?? {},
    outputs: run.outputs ?? {},
    intermediateParameters: run.intermediateParameters ?? {},
    errorMessage: run.errorMessage,
    durationMs: run.durationMs,
    model: run.model,
    branchName: run.branchName ?? options.branchName ?? 'main',
    publishedVersionId: run.publishedVersionId ?? options.publishedVersionId,
    publishedVersionNumber: run.publishedVersionNumber ?? options.publishedVersionNumber,
    actorId: run.actorId,
    actorName: run.actorName,
    serviceContext: run.serviceContext ?? {
      invocationSurface: run.invocationSurface,
      permissionSubject: run.executionMode === 'project_scoped' ? 'project' : 'initiating_user',
      permissionSubjectId: run.executionMode === 'project_scoped' ? config.projectId : run.actorId,
      initiatingUserId: run.actorId,
      projectId: run.executionMode === 'project_scoped' ? config.projectId : undefined,
      logsVisibleTo: run.executionMode === 'project_scoped' ? 'project_viewers' : 'initiating_user',
    },
    traceRefs: run.traceRefs ?? [],
    computeUsage: run.computeUsage,
    startedAtIso: run.startedAtIso,
    completedAtIso,
    visibleTo: config.visibleTo,
  };
}

export function limitLogicRunHistoryDatasetRows(rows: LogicRunHistoryDatasetRow[], maxRows: number): LogicRunHistoryDatasetRow[] {
  return [...rows]
    .sort((left, right) => Date.parse(right.startedAtIso) - Date.parse(left.startedAtIso))
    .slice(0, Math.max(0, Math.floor(maxRows)));
}

function projectScopedResourceKey(kind: LogicProjectScopedResourceKind, id: string) {
  return `${kind}:${id}`;
}

function addProjectScopedResource(
  resources: Map<string, LogicProjectScopedResourceRequirement>,
  item: Omit<LogicProjectScopedResourceRequirement, 'imported' | 'markingAccess' | 'markings'> & Partial<Pick<LogicProjectScopedResourceRequirement, 'imported' | 'markingAccess' | 'markings'>>,
) {
  if (!item.id.trim()) return;
  const key = projectScopedResourceKey(item.kind, item.id);
  const existing = resources.get(key);
  resources.set(key, {
    id: item.id,
    label: existing?.label ?? item.label,
    kind: item.kind,
    source: existing ? `${existing.source}, ${item.source}` : item.source,
    imported: existing?.imported ?? item.imported ?? true,
    markingAccess: existing?.markingAccess ?? item.markingAccess ?? true,
    markings: Array.from(new Set([...(existing?.markings ?? []), ...(item.markings ?? [])])),
  });
}

export function logicProjectScopedResourceRequirements(
  inputs: LogicInputDefinition[],
  llmBlock: LogicLlmBlockConfig,
  outputs: LogicOutputDefinition[] = [],
  overrides: Record<string, Partial<Pick<LogicProjectScopedResourceRequirement, 'imported' | 'markingAccess' | 'markings'>>> = {},
): LogicProjectScopedResourceRequirement[] {
  const resources = new Map<string, LogicProjectScopedResourceRequirement>();
  inputs.forEach((input) => {
    if (['object', 'object_list', 'object_set'].includes(input.type) && input.objectTypeId) {
      addProjectScopedResource(resources, { id: input.objectTypeId, label: input.objectTypeId, kind: 'object_type', source: `input ${input.apiName}` });
    }
    if (input.type === 'media_reference' && input.mediaSetRid) {
      addProjectScopedResource(resources, { id: input.mediaSetRid, label: input.mediaSetRid, kind: 'media_set', source: `input ${input.apiName}` });
    }
    if (input.type === 'model' && input.modelVariableKind) {
      addProjectScopedResource(resources, { id: input.modelVariableKind, label: `${input.modelVariableKind} model`, kind: 'model', source: `input ${input.apiName}` });
    }
  });
  llmBlock.toolAccess.forEach((tool) => {
    if (tool.kind === 'query_objects') {
      addProjectScopedResource(resources, { id: tool.objectTypeId, label: tool.objectTypeId, kind: 'object_type', source: `tool ${tool.name}`, markings: tool.selectedProperties.includes('ssn') ? ['PII'] : [] });
    }
    if (tool.kind === 'apply_action') {
      addProjectScopedResource(resources, { id: tool.actionTypeId, label: tool.actionTypeId, kind: 'action_type', source: `tool ${tool.name}` });
    }
    if (tool.kind === 'execute_function') {
      addProjectScopedResource(resources, { id: tool.functionRid, label: tool.functionRid, kind: 'function', source: `tool ${tool.name}` });
    }
  });
  outputs.forEach((output) => {
    if (output.outputType === 'ontology_edit_bundle') {
      addProjectScopedResource(resources, { id: output.sourceId, label: output.sourceId, kind: 'action_type', source: `output ${output.apiName}` });
    }
  });
  return Array.from(resources.values()).map((resource) => {
    const override = overrides[projectScopedResourceKey(resource.kind, resource.id)] ?? overrides[resource.id] ?? {};
    return { ...resource, ...override, markings: override.markings ?? resource.markings };
  }).sort((left, right) => left.kind.localeCompare(right.kind) || left.id.localeCompare(right.id));
}

export function validateLogicProjectScopedResources(resources: LogicProjectScopedResourceRequirement[]): LogicProjectScopedValidation {
  const missingImports = resources.filter((resource) => !resource.imported);
  const missingMarkingAccess = resources.filter((resource) => resource.imported && !resource.markingAccess);
  return {
    ready: missingImports.length === 0 && missingMarkingAccess.length === 0,
    missingImports,
    missingMarkingAccess,
    issues: [
      ...missingImports.map((resource): LogicIssue => ({
        severity: 'error',
        field: `projectScopedResources.${resource.id}.imported`,
        message: `${resource.label} must be imported into the Logic project for project-scoped execution.`,
      })),
      ...missingMarkingAccess.map((resource): LogicIssue => ({
        severity: 'error',
        field: `projectScopedResources.${resource.id}.markings`,
        message: `${resource.label} requires marking access${resource.markings.length ? ` for ${resource.markings.join(', ')}` : ''}.`,
      })),
    ],
  };
}

export function logicRunVisibleToViewer(run: LogicRunHistoryRecord, viewerActorId: string): boolean {
  if (run.executionMode === 'user_scoped') return run.actorId === viewerActorId;
  return true;
}

export function filterLogicRunsForViewer(runs: LogicRunHistoryRecord[], viewerActorId: string, now = new Date()): LogicRunHistoryRecord[] {
  const nowMs = now.getTime();
  return runs
    .filter((run) => logicRunVisibleToViewer(run, viewerActorId))
    .filter((run) => Date.parse(run.retentionExpiresAtIso) > nowMs)
    .sort((left, right) => Date.parse(right.startedAtIso) - Date.parse(left.startedAtIso));
}

function logicMetricsWindowStart(window: LogicMetricsWindow, now: Date): Date {
  const start = new Date(now);
  switch (window) {
    case '24h':
      start.setTime(now.getTime() - 24 * 60 * 60 * 1000);
      return start;
    case '7d':
      start.setDate(now.getDate() - 7);
      return start;
    case '90d':
      start.setDate(now.getDate() - 90);
      return start;
    case '30d':
    default:
      start.setDate(now.getDate() - 30);
      return start;
  }
}

function inferLogicFailureCategory(run: LogicRunHistoryRecord): string {
  if (run.failureCategory) return run.failureCategory;
  const text = (run.errorMessage ?? '').toLowerCase();
  if (text.includes('permission') || text.includes('unauthorized') || text.includes('forbidden')) return 'permission_error';
  if (text.includes('validation') || text.includes('invalid input') || text.includes('schema')) return 'validation_error';
  if (text.includes('ontology') || text.includes('edit bundle') || text.includes('writeback')) return 'ontology_edit_error';
  if (text.includes('timeout') || text.includes('deadline')) return 'timeout';
  if (text.includes('rate limit') || text.includes('quota')) return 'rate_limit';
  if (text.includes('model') || text.includes('llm')) return 'model_error';
  return 'runtime_error';
}

function p95DurationMs(runs: LogicRunHistoryRecord[]): number | null {
  if (runs.length === 0) return null;
  const durations = runs.map((run) => run.durationMs).sort((left, right) => left - right);
  return durations[Math.max(0, Math.ceil(durations.length * 0.95) - 1)] ?? null;
}

function countRunsByFailureText(runs: LogicRunHistoryRecord[], patterns: RegExp[]): number {
  return runs.filter((run) => {
    const text = `${run.failureCategory ?? ''} ${run.errorMessage ?? ''}`.toLowerCase();
    return patterns.some((pattern) => pattern.test(text));
  }).length;
}

function logicHealthStatus(metrics: LogicOperationalHealthMetric[]): LogicOperationalHealthSummary['status'] {
  if (metrics.some((metric) => metric.status === 'critical')) return 'critical';
  if (metrics.some((metric) => metric.status === 'warning')) return 'warning';
  return 'healthy';
}

function healthStatus(value: number, warning: number, critical: number): LogicOperationalHealthMetric['status'] {
  if (value >= critical) return 'critical';
  if (value >= warning) return 'warning';
  return 'healthy';
}

function buildLogicOperationalHealthSummary(input: {
  failureRate: number;
  p95DurationMs: number | null;
  totalComputeSeconds: number;
  totalPromptTokensEstimate: number;
  toolFailureCount: number;
  actionFailureCount: number;
  objectQueryFailureCount: number;
  modelUnavailableCount: number;
  runHistoryDatasetFailureCount: number;
  automationProposalBacklog: number;
}): LogicOperationalHealthSummary {
  const metrics: LogicOperationalHealthMetric[] = [
    { id: 'failure_rate', label: 'Failure rate', value: input.failureRate, unit: 'percent', threshold: 10, status: healthStatus(input.failureRate, 10, 25) },
    { id: 'p95_duration', label: 'P95 duration', value: input.p95DurationMs ?? 0, unit: 'milliseconds', threshold: 10_000, status: input.p95DurationMs === null ? 'healthy' : healthStatus(input.p95DurationMs, 10_000, 30_000) },
    { id: 'token_compute_usage', label: 'Token / compute usage', value: input.totalComputeSeconds, unit: 'compute_seconds', threshold: 250, status: healthStatus(input.totalComputeSeconds, 250, 1000) },
    { id: 'tool_failures', label: 'Tool failures', value: input.toolFailureCount, unit: 'count', threshold: 1, status: healthStatus(input.toolFailureCount, 1, 5) },
    { id: 'action_failures', label: 'Action failures', value: input.actionFailureCount, unit: 'count', threshold: 1, status: healthStatus(input.actionFailureCount, 1, 3) },
    { id: 'object_query_failures', label: 'Object query failures', value: input.objectQueryFailureCount, unit: 'count', threshold: 1, status: healthStatus(input.objectQueryFailureCount, 1, 3) },
    { id: 'model_unavailability', label: 'Model unavailability', value: input.modelUnavailableCount, unit: 'count', threshold: 1, status: healthStatus(input.modelUnavailableCount, 1, 3) },
    { id: 'run_history_dataset_failures', label: 'Run-history dataset failures', value: input.runHistoryDatasetFailureCount, unit: 'count', threshold: 1, status: healthStatus(input.runHistoryDatasetFailureCount, 1, 2) },
    { id: 'automation_proposal_backlog', label: 'Automation proposal backlog', value: input.automationProposalBacklog, unit: 'count', threshold: 10, status: healthStatus(input.automationProposalBacklog, 10, 25) },
  ];
  return {
    status: logicHealthStatus(metrics),
    metrics,
    surfaces: [
      { id: 'logic_detail', label: 'Logic detail', href: '/logic?rail=Metrics', visible: true, metricIds: metrics.map((metric) => metric.id) },
      { id: 'workflow_lineage', label: 'Workflow Lineage', href: '/lineage?kind=logic', visible: true, metricIds: ['failure_rate', 'p95_duration', 'tool_failures', 'action_failures', 'object_query_failures', 'model_unavailability'] },
      { id: 'data_health', label: 'Data Health', href: '/control-panel/data-health?asset=logic', visible: true, metricIds: ['run_history_dataset_failures', 'failure_rate'] },
      { id: 'project_dashboard', label: 'Project dashboards', href: '/dashboards?template=logic-operational-health', visible: true, metricIds: ['failure_rate', 'p95_duration', 'token_compute_usage', 'automation_proposal_backlog'] },
    ],
  };
}

export function calculateLogicMetrics(
  runs: LogicRunHistoryRecord[],
  window: LogicMetricsWindow,
  now = new Date(),
  options: { automationProposalBacklog?: number } = {},
): LogicMetricsSummary {
  const windowStart = logicMetricsWindowStart(window, now);
  const windowStartMs = windowStart.getTime();
  const windowEndMs = now.getTime();
  const windowRuns = runs
    .filter((run) => {
      const startedAt = Date.parse(run.startedAtIso);
      return Number.isFinite(startedAt) && startedAt >= windowStartMs && startedAt <= windowEndMs;
    })
    .sort((left, right) => Date.parse(right.startedAtIso) - Date.parse(left.startedAtIso));
  const failureCounts = new Map<string, number>();
  let successCount = 0;
  let failureCount = 0;
  for (const run of windowRuns) {
    if (run.status === 'succeeded') {
      successCount += 1;
    } else if (run.status === 'failed') {
      failureCount += 1;
      const category = inferLogicFailureCategory(run);
      failureCounts.set(category, (failureCounts.get(category) ?? 0) + 1);
    }
  }
  const failedRuns = windowRuns.filter((run) => run.status === 'failed');
  const p95 = p95DurationMs(windowRuns);
  const totalComputeSeconds = windowRuns.reduce((sum, run) => sum + (run.computeUsage?.totalComputeSeconds ?? 0), 0);
  const totalPromptTokensEstimate = windowRuns.reduce((sum, run) => sum + (run.computeUsage?.promptTokensEstimate ?? 0), 0);
  const totalRuns = successCount + failureCount;
  const failureRate = totalRuns === 0 ? 0 : Math.round((failureCount / totalRuns) * 1000) / 10;
  const healthInput = {
    failureRate,
    p95DurationMs: p95,
    totalComputeSeconds,
    totalPromptTokensEstimate,
    toolFailureCount: countRunsByFailureText(failedRuns, [/tool/, /function execution/, /calculator/]),
    actionFailureCount: countRunsByFailureText(failedRuns, [/action/, /ontology edit/, /writeback/]),
    objectQueryFailureCount: countRunsByFailureText(failedRuns, [/object query/, /query_objects/, /ontology query/]),
    modelUnavailableCount: countRunsByFailureText(failedRuns, [/model unavailable/, /model_error/, /llm unavailable/, /capacity/]),
    runHistoryDatasetFailureCount: countRunsByFailureText(failedRuns, [/run history dataset/, /dataset write/]),
    automationProposalBacklog: Math.max(0, Math.floor(options.automationProposalBacklog ?? 0)),
  };
  const failureCategories = Array.from(failureCounts.entries())
    .map(([category, count]) => ({ category, count }))
    .sort((left, right) => right.count - left.count || left.category.localeCompare(right.category));
  return {
    window,
    windowStartIso: windowStart.toISOString(),
    windowEndIso: now.toISOString(),
    successCount,
    failureCount,
    failureCategories,
    recentRuns: windowRuns.slice(0, 10),
    viewerPermissionRequired: true,
    ...healthInput,
    operationalHealth: buildLogicOperationalHealthSummary(healthInput),
  };
}
const LOGIC_USAGE_DESCRIPTIONS: Record<LogicUsageSurfaceId, Pick<LogicUsageSurface, 'label' | 'description' | 'href'>> = {
  workshop: {
    label: 'Workshop',
    description: 'Bind this published Logic function to Workshop variables, widgets, and app flows.',
    href: '/workshop',
  },
  action_workflow: {
    label: 'Action-backed workflows',
    description: 'Invoke this function after an action gathers inputs or commits downstream work.',
    href: '/action-types',
  },
  logic_function: {
    label: 'Other Logic functions',
    description: 'Expose this publication through an Execute function tool in another Logic function.',
    href: '/logic',
  },
  function_on_objects: {
    label: 'Function-on-objects',
    description: 'Call this function in object-scoped contexts when an Ontology object input is present.',
    href: '/ontology-manager/functions',
  },
  automate: {
    label: 'Automate',
    description: 'Create an automation that invokes the published function from a schedule or object event.',
    href: '/automate',
  },
  api_curl: {
    label: 'API / curl',
    description: 'Invoke this function from API clients when the published return type is supported.',
    href: '/api/docs/logic',
  },
};

const LOGIC_USAGE_SURFACE_IDS: LogicUsageSurfaceId[] = ['workshop', 'action_workflow', 'logic_function', 'function_on_objects', 'automate', 'api_curl'];
const API_ONTOLOGY_EDIT_BLOCKED_REASON = 'Command-line and API invocation are unavailable for Logic functions that return Ontology edits.';

function sampleValueForInput(input: LogicInputDefinition): unknown {
  if (input.defaultValue !== undefined && input.defaultValue.trim() !== '') return input.defaultValue;
  switch (input.type) {
    case 'boolean':
      return true;
    case 'date':
      return '2026-05-13';
    case 'timestamp':
      return '2026-05-13T12:00:00Z';
    case 'short':
    case 'integer':
    case 'long':
      return 1;
    case 'float':
    case 'double':
      return 1.5;
    case 'array':
    case 'list':
      return ['sample'];
    case 'object_list':
    case 'object_set':
      return [{ objectType: input.objectTypeId ?? 'Object', primaryKey: 'sample' }];
    case 'object':
      return { objectType: input.objectTypeId ?? 'Object', primaryKey: 'sample' };
    case 'media_reference':
      return 'ri.media-set.main.media.sample';
    case 'model':
      return 'gpt-4.1-mini';
    case 'struct':
      return { sample: true };
    default:
      return 'sample text';
  }
}

function sampleInputs(definition: LogicVersionDefinition): Record<string, unknown> {
  return Object.fromEntries(definition.inputs.map((input) => [input.apiName, sampleValueForInput(input)]));
}

function prettyJson(value: unknown): string {
  return JSON.stringify(value, null, 2);
}

function shellSingleQuote(value: string): string {
  return value.replaceAll("'", "'\"'\"'");
}

function inputMappings(definition: LogicVersionDefinition): Record<string, string> {
  return Object.fromEntries(definition.inputs.map((input) => [input.apiName, input.apiName]));
}

function objectInputForFunctionCall(definition: LogicVersionDefinition): LogicInputDefinition | undefined {
  return definition.inputs.find((input) => input.type === 'object' || input.type === 'object_list' || input.type === 'object_set');
}

function logicFunctionApiUrl(baseUrl: string, functionRid: string) {
  return `${baseUrl.replace(/\/$/, '')}/api/v1/agent-runtime/logic/functions/${encodeURIComponent(functionRid)}/invoke`;
}

function logicBackedActionName(functionRid: string) {
  const suffix = functionRid
    .replace(/^logic\./, 'logic_')
    .replace(/[^A-Za-z0-9]+/g, '_')
    .replace(/^_+|_+$/g, '')
    .toLowerCase();
  return `${suffix || 'logic_function'}_action`;
}

function actionPropertyTypeForLogicInput(input: LogicInputDefinition): string {
  switch (input.type) {
    case 'boolean':
      return 'boolean';
    case 'date':
      return 'date';
    case 'timestamp':
      return 'timestamp';
    case 'short':
    case 'integer':
    case 'long':
      return 'integer';
    case 'float':
    case 'double':
      return 'float';
    case 'object':
      return 'object_reference';
    case 'object_list':
    case 'object_set':
      return 'object_reference_list';
    case 'array':
    case 'list':
    case 'struct':
      return 'json';
    default:
      return 'string';
  }
}

function logicBackedActionInputSchema(definition: LogicVersionDefinition): LogicBackedActionInputField[] {
  return definition.inputs.map((input) => ({
    name: input.apiName,
    display_name: input.name,
    description: input.description,
    property_type: actionPropertyTypeForLogicInput(input),
    required: input.required,
    default_value: input.defaultValue && input.defaultValue.trim() ? input.defaultValue : undefined,
  }));
}

function logicBackedActionHref(draft: Pick<LogicBackedActionTypeDraft, 'createActionTypeBody' | 'functionRid' | 'publishedVersionNumber'>) {
  const params = new URLSearchParams({
    source: 'logic',
    functionRid: draft.functionRid,
    version: String(draft.publishedVersionNumber),
    draft: JSON.stringify(draft.createActionTypeBody),
  });
  return `/action-types?${params.toString()}`;
}

export function logicBackedActionEditPolicy(context: LogicBackedActionExecutionContext): LogicBackedActionEditPolicy {
  if (context.surface === 'workshop_action_execution') {
    return {
      canApplyOntologyEdits: Boolean(context.actionExecutionId),
      applyMode: context.actionExecutionId ? 'action_execution' : 'blocked',
      reason: context.actionExecutionId
        ? 'Ontology edits are applied by the submitted action execution.'
        : 'Workshop calls must submit an action execution before applying Ontology edits.',
    };
  }
  if (context.surface === 'branch_preview') {
    return {
      canApplyOntologyEdits: Boolean(context.actionExecutionId && context.branchName),
      applyMode: context.actionExecutionId && context.branchName ? 'branch_preview' : 'blocked',
      reason: context.actionExecutionId && context.branchName
        ? `Edits are scoped to preview branch ${context.branchName}.`
        : 'Branch-aware previews require both an action execution and branch name.',
    };
  }
  if (context.surface === 'approved_automation') {
    return {
      canApplyOntologyEdits: Boolean(context.approvedAutomationProposalId),
      applyMode: context.approvedAutomationProposalId ? 'approved_automation' : 'blocked',
      reason: context.approvedAutomationProposalId
        ? 'Ontology edits are applied after an approved automation proposal.'
        : 'Automation proposals must be approved before Ontology edits are applied.',
    };
  }
  return {
    canApplyOntologyEdits: false,
    applyMode: 'preview_only',
    reason: 'Raw Logic preview and API contexts return proposed edits only.',
  };
}

export function buildLogicBackedActionTypeDraft({
  functionRid,
  publishedVersion,
  definition,
  baseUrl = 'http://localhost:8080',
  branchName = 'main',
}: {
  functionRid?: string;
  publishedVersion?: LogicSavedVersion;
  definition: LogicVersionDefinition;
  baseUrl?: string;
  branchName?: string;
}): LogicBackedActionTypeDraft | null {
  if (!functionRid || publishedVersion?.status !== 'published') return null;
  const editOutput = ontologyEditOutputForAutomation(definition);
  const objectInput = objectInputForFunctionCall(definition);
  const objectTypeId = objectInput?.objectTypeId ?? objectInput?.objectSetObjectTypeId ?? '';
  const inputSchema = logicBackedActionInputSchema(definition);
  const actionName = logicBackedActionName(functionRid);
  const sampleParameters = sampleInputs(definition);
  const operation = {
    kind: 'invoke_function',
    function_kind: 'logic',
    function_rid: functionRid,
    published_version_number: publishedVersion.versionNumber,
    url: logicFunctionApiUrl(baseUrl, functionRid),
    method: 'POST',
    parameter_mapping: inputMappings(definition),
    body_mapping: {
      inputs_from: 'parameters',
      invocation_surface: 'action_execution',
      target_object_input: objectInput?.apiName,
    },
    output_api_name: editOutput?.apiName,
    ontology_edit_application: editOutput ? 'action_execution_or_approved_automation_only' : 'none',
    branch_aware_preview: {
      enabled: true,
      branch_parameter: 'execution_context.branch_name',
    },
  };
  const config = {
    operation,
    logic_function: {
      function_rid: functionRid,
      published_version_number: publishedVersion.versionNumber,
      output_api_name: editOutput?.apiName,
    },
    workshop: {
      supported: true,
      invocation_surface: 'workshop_action_execution',
    },
    guardrails: {
      proposed_edits_only_in_logic_preview: true,
      real_edits_require_action_execution_or_approved_automation: true,
    },
  };
  const formSchema = {
    sections: [{
      id: 'logic-inputs',
      title: 'Logic inputs',
      parameter_names: inputSchema.map((field) => field.name),
    }],
  };
  const authorizationPolicy = {
    required_permission_keys: ['ontology.actions.execute', 'logic.functions.invoke'],
  };
  const createActionTypeBody = {
    name: actionName,
    display_name: `Run ${functionRid}`,
    description: editOutput
      ? `Invokes published Logic ${functionRid} v${publishedVersion.versionNumber} and applies ${editOutput.apiName} through action execution.`
      : `Invokes published Logic ${functionRid} v${publishedVersion.versionNumber}.`,
    object_type_id: objectTypeId,
    operation_kind: 'invoke_function',
    input_schema: inputSchema,
    form_schema: formSchema,
    config,
    confirmation_required: Boolean(editOutput),
    permission_key: 'logic.actions.execute',
    authorization_policy: authorizationPolicy,
  };
  const draftBase = {
    name: actionName,
    displayName: `Run ${functionRid}`,
    description: String(createActionTypeBody.description),
    objectTypeId,
    operationKind: 'invoke_function' as const,
    functionRid,
    publishedVersionNumber: publishedVersion.versionNumber,
    ontologyEditOutputApiName: editOutput?.apiName,
    inputSchema,
    formSchema,
    config,
    authorizationPolicy,
    confirmationRequired: Boolean(editOutput),
    permissionKey: 'logic.actions.execute',
    workshopButton: {
      action_id: actionName,
      label: `Run ${functionRid}`,
      default_values: sampleParameters,
      execution_context: {
        surface: 'workshop_action_execution' as const,
        actionExecutionId: `action-${actionName}`,
      },
    },
    branchPreview: {
      enabled: true,
      branchName,
      execution_context: {
        surface: 'branch_preview' as const,
        branchName,
        actionExecutionId: `preview-${actionName}`,
      },
    },
    guardrails: [
      'Logic preview returns proposed Ontology edits only.',
      'Workshop applies real edits through submitted action execution.',
      'Automate applies real edits only after an approved proposal or configured auto-apply flow.',
      'Branch-aware preview execution is scoped to the selected branch.',
    ],
    createActionTypeBody,
  };
  return {
    ...draftBase,
    href: logicBackedActionHref(draftBase),
  };
}

export function logicDefinitionReturnsOntologyEdits(definition: LogicVersionDefinition): boolean {
  const finalOutputs = definition.outputs.filter((output) => output.final);
  const outputsToInspect = finalOutputs.length > 0 ? finalOutputs : definition.outputs;
  return outputsToInspect.some((output) => output.outputType === 'ontology_edit_bundle' || output.source === 'ontology_edit_bundle');
}

function ontologyEditOutputForAutomation(definition: LogicVersionDefinition): LogicOutputDefinition | undefined {
  return definition.outputs.find((output) => output.final && (output.outputType === 'ontology_edit_bundle' || output.source === 'ontology_edit_bundle'))
    ?? definition.outputs.find((output) => output.outputType === 'ontology_edit_bundle' || output.source === 'ontology_edit_bundle');
}

export function logicDefinitionHasOntologyEditOutput(definition: LogicVersionDefinition): boolean {
  return ontologyEditOutputForAutomation(definition) !== undefined;
}

function firstApplyActionTool(definition: LogicVersionDefinition): { actionTypeId?: string; parameterMappings?: Record<string, string> } | undefined {
  for (const block of definition.blocks) {
    const toolAccess = Array.isArray(block.toolAccess) ? block.toolAccess : [];
    const tool = toolAccess.find((candidate): candidate is { kind: string; actionTypeId?: string; parameterMappings?: Record<string, string> } => {
      return typeof candidate === 'object' && candidate !== null && (candidate as { kind?: string }).kind === 'apply_action';
    });
    if (tool) return tool;
  }
  return undefined;
}

function automationHref(draft: Pick<LogicAutomationDraft, 'functionRid' | 'publishedVersionNumber' | 'editMode' | 'ontologyEditOutputApiName' | 'actionTypeId' | 'objectTypeId'>) {
  const params = new URLSearchParams({
    source: 'logic',
    functionRid: draft.functionRid,
    version: String(draft.publishedVersionNumber),
    mode: draft.editMode,
    output: draft.ontologyEditOutputApiName,
    actionTypeId: draft.actionTypeId,
  });
  if (draft.objectTypeId) params.set('objectType', draft.objectTypeId);
  return `/automate?${params.toString()}`;
}

export function buildLogicAutomationDraft({
  functionRid,
  publishedVersion,
  definition,
  mode = 'stage_for_review',
  source = 'logic_uses_sidebar',
}: {
  functionRid?: string;
  publishedVersion?: LogicSavedVersion;
  definition: LogicVersionDefinition;
  mode?: LogicAutomationEditMode;
  source?: LogicAutomationDraft['source'];
}): LogicAutomationDraft | null {
  if (!functionRid || publishedVersion?.status !== 'published') return null;
  const editOutput = ontologyEditOutputForAutomation(definition);
  if (!editOutput) return null;
  const objectInput = objectInputForFunctionCall(definition);
  const actionTool = firstApplyActionTool(definition);
  const actionTypeId = actionTool?.actionTypeId ?? editOutput.sourceId ?? 'ontology-edit-action';
  const objectTypeId = objectInput?.objectTypeId ?? objectInput?.objectSetObjectTypeId;
  const triggerType: LogicAutomationDraft['trigger']['type'] = objectInput ? 'object_set_new_object' : 'manual';
  const triggerEventName = objectTypeId ? `ontology.${objectTypeId}.object.changed` : 'manual.logic.automation';
  const logicStepId = 'invoke-logic-effect';
  const reviewStepId = mode === 'stage_for_review' ? 'stage-action-proposal' : 'apply-ontology-edits';
  const inputMap = inputMappings(definition);
  const logicEffect = {
    type: 'logic_effect' as const,
    functionRid,
    inputMappings: inputMap,
    outputApiName: editOutput.apiName,
    editMode: mode,
  };
  const reviewStep = mode === 'stage_for_review'
    ? {
        id: reviewStepId,
        name: 'Stage action proposal',
        step_type: 'approval' as const,
        config: {
          proposal_source: 'logic_effect',
          function_rid: functionRid,
          action_id: actionTypeId,
          output_api_name: editOutput.apiName,
          auto_apply_on_approval: true,
          decision_log_handoff: true,
        },
        next_step_id: null,
        branches: [],
      }
    : {
        id: reviewStepId,
        name: 'Apply Ontology edits',
        step_type: 'submit_action' as const,
        config: {
          action_id: actionTypeId,
          parameters_from: `logic.outputs.${editOutput.apiName}`,
          execution_identity: 'project',
          decision_log_handoff: true,
        },
        next_step_id: null,
        branches: [],
      };
  const draftBase = {
    id: `logic-automation-${functionRid.replace(/[^A-Za-z0-9]+/g, '-').replace(/^-|-$/g, '').toLowerCase() || 'logic'}`,
    name: `Automation for ${functionRid}`,
    source,
    status: 'draft' as const,
    functionRid,
    publishedVersionNumber: publishedVersion.versionNumber,
    editMode: mode,
    ontologyEditOutputApiName: editOutput.apiName,
    actionTypeId,
    objectInputApiName: objectInput?.apiName,
    objectTypeId,
    trigger: {
      type: triggerType,
      objectSetRid: objectTypeId ? `ri.object-set.${objectTypeId.toLowerCase()}.active` : undefined,
      eventName: triggerEventName,
    },
    logicEffect,
    workflowPayload: {
      name: `Automation for ${functionRid}`,
      trigger_type: objectInput ? 'event' as const : 'manual' as const,
      trigger_config: objectInput
        ? { event_name: triggerEventName, object_type_id: objectTypeId, object_input_api_name: objectInput.apiName }
        : {},
      steps: [
        {
          id: logicStepId,
          name: 'Invoke Logic effect',
          step_type: 'logic_effect' as const,
          config: logicEffect,
          next_step_id: reviewStepId,
          branches: [],
        },
        reviewStep,
      ],
    },
    proposalVisibilityHours: 24,
  };
  return {
    ...draftBase,
    href: automationHref(draftBase),
  };
}

export function buildLogicAutomationEventChart(draft: LogicAutomationDraft, now = new Date()): LogicAutomationEventBucket[] {
  return Array.from({ length: 7 }, (_, index) => {
    const day = new Date(now);
    day.setDate(now.getDate() - (6 - index));
    const triggered = Math.max(1, (index + 2) * (draft.editMode === 'auto_apply' ? 3 : 2));
    const failed = index % 5 === 0 ? 1 : 0;
    const applied = draft.editMode === 'auto_apply' ? triggered - failed : Math.max(0, Math.floor(triggered * 0.45) - failed);
    const staged = draft.editMode === 'stage_for_review' ? triggered - applied - failed : 0;
    return {
      label: day.toISOString().slice(5, 10),
      triggered,
      staged,
      applied,
      failed,
    };
  });
}

export function buildLogicAutomationProposal(draft: LogicAutomationDraft, now = new Date()): LogicAutomationProposal {
  const createdAtIso = now.toISOString();
  const targetObjectId = draft.objectTypeId ? `${draft.objectTypeId}:sample-4421` : 'Object:sample-4421';
  const parameters = {
    source_logic_function: draft.functionRid,
    output_api_name: draft.ontologyEditOutputApiName,
    customer: targetObjectId,
    summary: `Apply edits proposed by ${draft.functionRid}`,
  };
  return {
    id: `proposal-${draft.id}`,
    automationId: draft.id,
    status: draft.editMode === 'auto_apply' ? 'applied' : 'open',
    createdBy: 'AIP Logic effect',
    createdAtIso,
    expiresAtIso: new Date(now.getTime() + draft.proposalVisibilityHours * 60 * 60 * 1000).toISOString(),
    summary: `Review ${draft.actionTypeId} from ${draft.functionRid}`,
    reason: draft.editMode === 'auto_apply'
      ? 'Automation applied the edit bundle using project-scoped execution.'
      : 'Automation staged the Logic-generated edit bundle for human review.',
    logicRunId: `logic-run-${now.getTime().toString(36)}`,
    actionTypeId: draft.actionTypeId,
    targetObjectId,
    parameters,
    proposedActionPreview: {
      actionTypeId: draft.actionTypeId,
      targetObjectId,
      parameters,
      applyMode: draft.editMode,
    },
    decisionLog: [
      {
        id: `decision-${now.getTime().toString(36)}-created`,
        atIso: createdAtIso,
        actor: 'Automate',
        event: draft.editMode === 'auto_apply' ? 'auto_applied' : 'proposal_staged',
        detail: draft.editMode === 'auto_apply'
          ? 'Ontology edits were applied automatically by the automation.'
          : 'Ontology edits were staged as an action proposal for review.',
      },
    ],
  };
}

export function decideLogicAutomationProposal(
  proposal: LogicAutomationProposal,
  decision: 'approved' | 'rejected',
  actor: string,
  now = new Date(),
): LogicAutomationProposal {
  return {
    ...proposal,
    status: decision === 'approved' ? 'applied' : 'rejected',
    decisionLog: [
      ...proposal.decisionLog,
      {
        id: `decision-${now.getTime().toString(36)}-${decision}`,
        atIso: now.toISOString(),
        actor,
        event: decision === 'approved' ? 'approved_and_applied' : 'rejected',
        detail: decision === 'approved'
          ? 'Reviewer approved the proposal and handed it to action execution.'
          : 'Reviewer rejected the staged proposal; no Ontology edits were applied.',
      },
    ],
  };
}

function unpublishedUsageSurfaces(): LogicUsageSurface[] {
  return LOGIC_USAGE_SURFACE_IDS.map((id) => ({
    id,
    ...LOGIC_USAGE_DESCRIPTIONS[id],
    status: 'requires_publish',
    requirements: ['Publish a Logic version'],
  }));
}

export function buildLogicUsageSurfaces({
  functionRid,
  publishedVersion,
  definition,
  baseUrl = 'http://localhost:8080',
}: {
  functionRid?: string;
  publishedVersion?: LogicSavedVersion;
  definition: LogicVersionDefinition;
  baseUrl?: string;
}): LogicUsageBundle {
  if (!functionRid || publishedVersion?.status !== 'published') {
    return {
      published: false,
      functionRid,
      publishedVersionNumber: publishedVersion?.versionNumber,
      returnsOntologyEdits: logicDefinitionReturnsOntologyEdits(definition),
      surfaces: unpublishedUsageSurfaces(),
    };
  }
  const inputs = sampleInputs(definition);
  const requirements = [`published_version=v${publishedVersion.versionNumber}`, `function_rid=${functionRid}`];
  const apiUrl = `${baseUrl.replace(/\/$/, '')}/api/v1/agent-runtime/logic/functions/${encodeURIComponent(functionRid)}/invoke`;
  const returnsOntologyEdits = logicDefinitionReturnsOntologyEdits(definition);
  const automationDraft = buildLogicAutomationDraft({ functionRid, publishedVersion, definition, mode: 'stage_for_review' });
  const actionTypeDraft = buildLogicBackedActionTypeDraft({ functionRid, publishedVersion, definition, baseUrl });
  const automateBlockedReason = 'Create Automation is available when the published Logic produces an Ontology edit output.';
  const objectInput = objectInputForFunctionCall(definition);
  const surfaces: LogicUsageSurface[] = [
    {
      id: 'workshop',
      ...LOGIC_USAGE_DESCRIPTIONS.workshop,
      status: 'available',
      requirements,
      snippet: {
        language: 'json',
        label: 'Workshop function variable',
        body: prettyJson({
          variable_type: 'function_output',
          function_package_id: functionRid,
          parameters: inputs,
          result_path: 'finalAnswer',
        }),
      },
    },
    {
      id: 'action_workflow',
      ...LOGIC_USAGE_DESCRIPTIONS.action_workflow,
      href: actionTypeDraft?.href ?? LOGIC_USAGE_DESCRIPTIONS.action_workflow.href,
      status: 'available',
      requirements: actionTypeDraft?.ontologyEditOutputApiName
        ? [...requirements, `ontology_edit_output=${actionTypeDraft.ontologyEditOutputApiName}`]
        : requirements,
      snippet: {
        language: 'json',
        label: 'Function-backed action type',
        body: prettyJson(actionTypeDraft?.createActionTypeBody ?? {
          operation_kind: 'invoke_function',
          function_rid: functionRid,
          inputs,
        }),
      },
    },
    {
      id: 'logic_function',
      ...LOGIC_USAGE_DESCRIPTIONS.logic_function,
      status: 'available',
      requirements,
      snippet: {
        language: 'json',
        label: 'Execute function tool',
        body: prettyJson({
          kind: 'execute_function',
          functionKind: 'existing_logic',
          functionRid,
          parameterMappings: inputMappings(definition),
          expectedOutputType: 'json',
        }),
      },
    },
    {
      id: 'function_on_objects',
      ...LOGIC_USAGE_DESCRIPTIONS.function_on_objects,
      status: 'available',
      requirements,
      snippet: {
        language: 'typescript',
        label: 'Object-scoped invocation',
        body: objectInput
          ? `await Functions.callOnObject('${objectInput.objectTypeId ?? 'Object'}', inputs.${objectInput.apiName}, '${functionRid}', ${prettyJson(inputs)});`
          : `await Functions.call('${functionRid}', ${prettyJson(inputs)});`,
      },
    },
    {
      id: 'automate',
      ...LOGIC_USAGE_DESCRIPTIONS.automate,
      href: automationDraft?.href ?? LOGIC_USAGE_DESCRIPTIONS.automate.href,
      status: automationDraft ? 'available' : 'blocked',
      blockedReason: automationDraft ? undefined : automateBlockedReason,
      requirements: automationDraft ? [...requirements, `ontology_edit_output=${automationDraft.ontologyEditOutputApiName}`] : [...requirements, 'Ontology edit output'],
      snippet: automationDraft ? {
        language: 'json',
        label: 'Pre-populated automation',
        body: prettyJson(automationDraft.workflowPayload),
      } : undefined,
    },
    {
      id: 'api_curl',
      ...LOGIC_USAGE_DESCRIPTIONS.api_curl,
      href: apiUrl,
      status: returnsOntologyEdits ? 'blocked' : 'available',
      blockedReason: returnsOntologyEdits ? API_ONTOLOGY_EDIT_BLOCKED_REASON : undefined,
      requirements,
      snippet: returnsOntologyEdits ? undefined : {
        language: 'bash',
        label: 'curl',
        body: `curl -X POST '${apiUrl}' \\\n  -H 'authorization: Bearer $OPENFOUNDRY_TOKEN' \\\n  -H 'content-type: application/json' \\\n  -d '${shellSingleQuote(prettyJson({ inputs }))}'`,
      },
    },
  ];
  return {
    published: true,
    functionRid,
    publishedVersionNumber: publishedVersion.versionNumber,
    returnsOntologyEdits,
    surfaces,
    actionTypeDraft: actionTypeDraft ?? undefined,
  };
}

const LOGIC_OUTPUT_VALUE_TYPES = new Set<LogicValueType>([
  'array',
  'list',
  'boolean',
  'date',
  'double',
  'float',
  'integer',
  'long',
  'media_reference',
  'object',
  'object_list',
  'object_set',
  'short',
  'string',
  'struct',
  'timestamp',
  'json',
  'ontology_edit_bundle',
]);

const CREATE_VARIABLE_VALUE_TYPES = new Set<LogicValueType>([
  'array',
  'boolean',
  'date',
  'double',
  'float',
  'integer',
  'long',
  'object',
  'short',
  'string',
  'struct',
  'timestamp',
  'json',
]);

function isCollectionType(type: LogicValueType | undefined): boolean {
  return type === 'array' || type === 'list' || type === 'object_list' || type === 'object_set';
}

function isOntologyEditType(type: LogicValueType | undefined): boolean {
  return type === 'ontology_edit_bundle';
}

function literalCompatible(value: string | undefined, type: LogicValueType): boolean {
  const raw = value?.trim() ?? '';
  if (!raw) return false;
  if (type === 'string') return true;
  if (type === 'boolean') return raw === 'true' || raw === 'false';
  if (['short', 'integer', 'long'].includes(type)) return /^-?\d+$/.test(raw);
  if (['float', 'double'].includes(type)) return Number.isFinite(Number(raw));
  if (type === 'date') return /^\d{4}-\d{2}-\d{2}$/.test(raw) && !Number.isNaN(Date.parse(`${raw}T00:00:00Z`));
  if (type === 'timestamp') return raw.includes('T') && !Number.isNaN(Date.parse(raw));
  if (['json', 'struct', 'array', 'object'].includes(type)) {
    try {
      const parsed = JSON.parse(raw) as unknown;
      if (type === 'array') return Array.isArray(parsed);
      if (type === 'struct' || type === 'object') return typeof parsed === 'object' && parsed !== null && !Array.isArray(parsed);
      return true;
    } catch {
      return false;
    }
  }
  return false;
}

export function validateCreateVariableBlock(block: LogicVariableBlockConfig, inputs: LogicInputDefinition[], blockOutputTypes: Record<string, LogicValueType> = {}): LogicIssue[] {
  const issues: LogicIssue[] = [];
  const inputTypes = inputTypeByApiName(inputs);
  if (!isValidLogicInputApiName(block.apiName)) {
    issues.push({ severity: 'error', field: 'variable.apiName', message: 'Variable API name must start with a letter and contain only letters, numbers, and underscores.' });
  }
  if (!CREATE_VARIABLE_VALUE_TYPES.has(block.valueType)) {
    issues.push({ severity: 'error', field: 'variable.valueType', message: 'Create variable blocks support primitive, array, object, struct, and JSON-compatible values only.' });
  }
  if (block.source === 'literal' && !literalCompatible(block.literalValue, block.valueType)) {
    issues.push({ severity: 'error', field: 'variable.literalValue', message: `Literal value is not compatible with ${block.valueType}.` });
  }
  if (block.source === 'input') {
    const inputType = inputTypes.get(block.inputApiName ?? '');
    if (!valueTypeCompatible(inputType, block.valueType)) {
      issues.push({ severity: 'error', field: 'variable.inputApiName', message: `Input source is not compatible with ${block.valueType}.` });
    }
  }
  if (block.source === 'block_output') {
    const outputType = blockOutputTypes[block.blockOutputId ?? ''];
    if (!outputTypeCompatible(outputType, block.valueType)) {
      issues.push({ severity: 'error', field: 'variable.blockOutputId', message: `Block output source is not compatible with ${block.valueType}.` });
    }
  }
  return issues;
}

function branchOutputCompatible(expected: LogicValueType, actual: LogicValueType | undefined): boolean {
  return Boolean(actual && (outputTypeCompatible(actual, expected) || outputTypeCompatible(expected, actual)));
}

export function validateConditionalBlock(block: LogicConditionalBlockConfig): LogicIssue[] {
  const issues: LogicIssue[] = [];
  if (!block.conditionExpression.trim()) {
    issues.push({ severity: 'error', field: 'conditional.conditionExpression', message: 'Conditional expression is required.' });
  }

  const branches = block.branches?.length
    ? block.branches
    : [
        { id: 'then', outputType: block.trueOutputType },
        { id: 'else', outputType: block.falseOutputType },
      ];
  const valueBranches = branches.filter((branch) => !branch.takeNoAction && !branch.returnsOntologyEdits);
  const editBranches = branches.filter((branch) => branch.returnsOntologyEdits);
  const noActionBranches = branches.filter((branch) => branch.takeNoAction);

  if (valueBranches.length > 0) {
    const expectedType = valueBranches[0].outputType;
    if (!expectedType || valueBranches.some((branch) => !branchOutputCompatible(expectedType, branch.outputType))) {
      issues.push({ severity: 'error', field: 'conditional.outputType', message: 'Conditional branches must produce compatible output types.' });
    }
    if (editBranches.length > 0 || noActionBranches.length > 0) {
      issues.push({ severity: 'error', field: 'conditional.branches', message: 'Conditionals cannot mix value-returning branches with ontology-edit or no-action branches.' });
    }
  } else if (editBranches.length > 0 && branches.some((branch) => !branch.returnsOntologyEdits && !branch.takeNoAction)) {
    issues.push({ severity: 'error', field: 'conditional.branches', message: 'Ontology edit conditionals require every branch to run an action or explicitly take no action.' });
  }
  return issues;
}

export function validateLoopBlock(block: LogicLoopBlockConfig, inputs: LogicInputDefinition[]): LogicIssue[] {
  const issues: LogicIssue[] = [];
  const inputTypes = inputTypeByApiName(inputs);
  const listInputType = inputTypes.get(block.inputApiName);
  if (listInputType !== 'array' && listInputType !== 'list' && listInputType !== 'object_list') {
    issues.push({ severity: 'error', field: 'loop.inputApiName', message: 'Loop input must be an array, list, or object list.' });
  }
  if (listInputType === 'array' && !block.arrayToListInserted) {
    issues.push({ severity: 'warning', field: 'loop.arrayToListInserted', message: 'Array loop inputs require an Array to List conversion before iteration.' });
  }
  if (!isValidLogicInputApiName(block.elementVariableApiName)) {
    issues.push({ severity: 'error', field: 'loop.elementVariableApiName', message: 'Loop element variable must be a valid API name.' });
  }
  if (!isValidLogicInputApiName(block.indexVariableApiName)) {
    issues.push({ severity: 'error', field: 'loop.indexVariableApiName', message: 'Loop index variable must be a valid API name.' });
  }
  if (block.elementVariableApiName === block.indexVariableApiName) {
    issues.push({ severity: 'error', field: 'loop.indexVariableApiName', message: 'Loop element and index variables must have distinct API names.' });
  }
  if (block.parallel && block.containsActionTool) {
    issues.push({ severity: 'error', field: 'loop.parallel', message: 'Loops that contain action tools must run sequentially.' });
  }
  if (!block.parallel && !block.containsActionTool) {
    issues.push({ severity: 'warning', field: 'loop.parallel', message: 'Loop can run in parallel because it has no action tools.' });
  }
  if (block.outputAggregation === 'list') {
    if (!isCollectionType(block.finalOutputType)) {
      issues.push({ severity: 'error', field: 'loop.finalOutputType', message: 'List aggregation must produce an array, list, object list, or object set output.' });
    }
    if (isOntologyEditType(block.bodyOutputType)) {
      issues.push({ severity: 'error', field: 'loop.outputAggregation', message: 'Ontology edit loop bodies must aggregate as ontology edits, not lists.' });
    }
  } else if (block.outputAggregation === 'first') {
    if (!outputTypeCompatible(block.bodyOutputType, block.finalOutputType)) {
      issues.push({ severity: 'error', field: 'loop.finalOutputType', message: 'First aggregation output must match the loop body output type.' });
    }
  } else if (block.outputAggregation === 'count') {
    if (block.finalOutputType !== 'integer' && block.finalOutputType !== 'long') {
      issues.push({ severity: 'error', field: 'loop.finalOutputType', message: 'Count aggregation must produce an integer or long output.' });
    }
  } else if (block.outputAggregation === 'none') {
    if (!isOntologyEditType(block.bodyOutputType) || !isOntologyEditType(block.finalOutputType)) {
      issues.push({ severity: 'error', field: 'loop.finalOutputType', message: 'No aggregation is only valid for ontology edit bundle loop outputs.' });
    }
  }
  return issues;
}

export function validateLogicOutputDefinition(output: LogicOutputDefinition, blockOutputTypes: Record<string, LogicValueType>): LogicIssue[] {
  const issues: LogicIssue[] = [];
  if (!output.name.trim()) {
    issues.push({ severity: 'error', field: 'output.name', message: 'Output display name is required.' });
  }
  if (!isValidLogicInputApiName(output.apiName)) {
    issues.push({ severity: 'error', field: 'output.apiName', message: 'Output API name must start with a letter and contain only letters, numbers, and underscores.' });
  }
  if (!LOGIC_OUTPUT_VALUE_TYPES.has(output.outputType)) {
    issues.push({ severity: 'error', field: 'output.outputType', message: 'Logic outputs cannot return model variables or unsupported local value types.' });
  }
  if (output.source === 'ontology_edit_bundle' && output.outputType !== 'ontology_edit_bundle') {
    issues.push({ severity: 'error', field: 'output.outputType', message: 'Ontology edit bundle sources must produce ontology_edit_bundle outputs.' });
  } else if (output.source !== 'ontology_edit_bundle') {
    const sourceType = blockOutputTypes[output.sourceId];
    if (!sourceType) {
      issues.push({ severity: 'error', field: 'output.sourceId', message: 'Output source must reference an existing block or intermediary output.' });
    } else if (!outputTypeCompatible(sourceType, output.outputType)) {
      issues.push({ severity: 'error', field: 'output.sourceId', message: `Output source is not compatible with ${output.outputType}.` });
    }
  }
  if (output.workshopUsage === 'markdown_display' && output.outputType !== 'string') {
    issues.push({ severity: 'error', field: 'output.workshopUsage', message: 'Workshop Markdown display functions require a string output.' });
  }
  return issues;
}

export function validateLogicOutputs(outputs: LogicOutputDefinition[], blockOutputTypes: Record<string, LogicValueType>): LogicIssue[] {
  const issues = outputs.flatMap((output) => validateLogicOutputDefinition(output, blockOutputTypes));
  if (!outputs.some((output) => output.final)) {
    issues.push({ severity: 'error', field: 'outputs.final', message: 'At least one final Logic function output is required.' });
  }
  const seen = new Set<string>();
  for (const output of outputs) {
    const normalized = output.apiName.toLowerCase();
    if (seen.has(normalized)) {
      issues.push({ severity: 'error', field: `outputs.${output.apiName}`, message: 'Output API names must be unique.' });
    }
    seen.add(normalized);
  }
  return issues;
}

export type LogicRunStatus = 'idle' | 'running' | 'succeeded' | 'failed';
export type LogicExecutionMode = 'draft_preview' | 'published' | 'automation';

export interface LogicPreviewRunMetadata {
  runId: string;
  executionMode: LogicExecutionMode;
  startedAtIso: string;
  durationMs: number;
  inputCount: number;
  toolCallCount: number;
  computeUsage: LogicComputeUsageSummary;
  retainedUntil: 'local_session' | 'platform_policy';
  securityFiltered: boolean;
}

export interface LogicPreviewRunResult {
  id: string;
  status: LogicRunStatus;
  result: string;
  durationMs: number;
  metadata: LogicPreviewRunMetadata;
  trace: LogicDebuggerTraceMetadata;
  outputs: Record<string, unknown>;
  intermediateParameters: Record<string, unknown>;
  errors: LogicIssue[];
}

export interface LogicDebuggerBlockTrace {
  id: string;
  title: string;
  status: 'not_run' | 'ok' | 'error';
  durationMs: number;
  inputs: Record<string, unknown>;
  outputs: Record<string, unknown>;
  prompt?: LogicDebuggerTraceMetadata['renderedPrompt'];
  toolCalls: LogicDebuggerTraceMetadata['toolCalls'];
  errors: LogicIssue[];
  retention: LogicPreviewRunMetadata['retainedUntil'];
  securityFiltered: boolean;
}

const SENSITIVE_TRACE_KEY_PATTERN = /authorization|password|secret|token|api[_-]?key|credential/i;
const TRACE_STRING_LIMIT = 240;

function sanitizeTraceValue(value: unknown, key = ''): unknown {
  if (SENSITIVE_TRACE_KEY_PATTERN.test(key)) return '[redacted]';
  if (typeof value === 'string') {
    return value.length > TRACE_STRING_LIMIT ? `${value.slice(0, TRACE_STRING_LIMIT)}…` : value;
  }
  if (Array.isArray(value)) return value.map((entry) => sanitizeTraceValue(entry));
  if (typeof value === 'object' && value !== null) {
    return Object.fromEntries(Object.entries(value).map(([entryKey, entryValue]) => [entryKey, sanitizeTraceValue(entryValue, entryKey)]));
  }
  return value;
}

function numericInputValue(inputValues: Record<string, string>, apiName: string, fallback: number): number {
  const parsed = Number(inputValues[apiName]);
  return Number.isFinite(parsed) ? parsed : fallback;
}

export function executeDraftLogicPreview(
  block: LogicLlmBlockConfig,
  inputs: LogicInputDefinition[],
  inputValues: Record<string, string>,
  now = new Date(),
): LogicPreviewRunResult {
  const trace = buildLlmDebuggerTrace(block, inputs);
  const errors = trace.errors.filter((issue) => issue.severity === 'error');
  const durationMs = Math.max(80, Math.round(trace.computeUsage.totalComputeSeconds * 12 + trace.toolCalls.length * 38));
  const runId = `draft-${now.getTime().toString(36)}`;
  const complaint = inputValues.complaintText?.trim() || 'No complaint text provided.';
  const baseRisk = numericInputValue(inputValues, 'baseRisk', 0);
  const delayHours = numericInputValue(inputValues, 'delayHours', 0);
  const riskScore = Math.round((baseRisk + delayHours * 2) * 10) / 10;
  const shipmentRecommendations = riskScore >= 45
    ? ['Create service recovery case', 'Notify account owner', 'Review related shipments']
    : ['Monitor SLA status', 'Share follow-up summary'];
  const result = errors.length > 0
    ? 'Draft run failed validation before execution.'
    : `Preview result: ${complaint} Risk score ${riskScore}; recommend service recovery follow-up and review proposed Ontology edits before publishing.`;

  return {
    id: runId,
    status: errors.length > 0 ? 'failed' : 'succeeded',
    result,
    durationMs,
    metadata: {
      runId,
      executionMode: 'draft_preview',
      startedAtIso: now.toISOString(),
      durationMs,
      inputCount: inputs.length,
      toolCallCount: trace.toolCalls.length,
      computeUsage: trace.computeUsage,
      retainedUntil: 'local_session',
      securityFiltered: true,
    },
    trace,
    outputs: {
      finalAnswer: result,
      actionEditPreview: {
        actionTypeId: 'create-service-case',
        mode: 'preview',
        proposedEdits: [{ objectTypeId: 'Customer', operation: 'update', fields: { nextStep: 'service_recovery_follow_up' } }],
      },
    },
    intermediateParameters: {
      llmTextDraft: result,
      riskScore,
      shipmentRecommendations,
    },
    errors,
  };
}

export function buildDebuggerBlockTraces(
  run: LogicPreviewRunResult | undefined,
  inputValues: Record<string, string>,
  clearToolCalls = false,
): LogicDebuggerBlockTrace[] {
  if (!run) return [];
  const retention = run.metadata.executionMode === 'draft_preview' ? 'local_session' : 'platform_policy';
  const sanitizedInputs = sanitizeTraceValue(inputValues) as Record<string, unknown>;
  const filteredToolCalls = clearToolCalls ? [] : run.trace.toolCalls.map((toolCall) => sanitizeTraceValue(toolCall) as LogicDebuggerTraceMetadata['toolCalls'][number]);

  return [
    {
      id: 'input-binding',
      title: 'Input binding',
      status: 'ok',
      durationMs: 42,
      inputs: sanitizedInputs,
      outputs: { boundInputs: Object.keys(inputValues).length },
      toolCalls: [],
      errors: [],
      retention,
      securityFiltered: true,
    },
    {
      id: run.trace.blockId,
      title: 'Use LLM prompt render',
      status: run.errors.length > 0 ? 'error' : 'ok',
      durationMs: Math.max(80, run.durationMs - 84),
      inputs: { variables: run.trace.renderedPrompt.variables },
      outputs: { structuredOutputKind: run.trace.output.structuredOutputKind, tokenUsage: run.trace.tokenUsage, computeUsage: run.trace.computeUsage },
      prompt: sanitizeTraceValue(run.trace.renderedPrompt) as LogicDebuggerTraceMetadata['renderedPrompt'],
      toolCalls: filteredToolCalls,
      errors: run.errors,
      retention,
      securityFiltered: true,
    },
    {
      id: 'final-output',
      title: 'Final output mapping',
      status: run.status === 'failed' ? 'error' : 'ok',
      durationMs: 42,
      inputs: { source: 'llm.text' },
      outputs: { finalResult: sanitizeTraceValue(run.result) },
      toolCalls: [],
      errors: run.errors,
      retention,
      securityFiltered: true,
    },
  ];
}
