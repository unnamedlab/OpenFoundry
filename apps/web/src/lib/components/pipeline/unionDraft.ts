// Pipeline Builder union draft — combines multiple upstream nodes into
// a single output. The runtime substitutes `__input_N__` with the Nth
// upstream's materialised view; the editor produces a UNION ALL chain.

export type UnionType = 'by_name' | 'by_position';

export interface UnionDraft {
  display_name: string;
  description?: string;
  union_type: UnionType;
  input_node_ids: string[];
  input_node_labels: string[];
}

export const UNION_TYPES: Array<{ id: UnionType; label: string; description: string }> = [
  {
    id: 'by_name',
    label: 'Union by name',
    description: 'Aligns columns from each input by name. Inputs must share the same column names.',
  },
  {
    id: 'by_position',
    label: 'Union by position',
    description: 'Aligns columns by their position in the input. Schemas must match column-for-column.',
  },
];

export function newUnionDraft(inputs: Array<{ id: string; label: string }>): UnionDraft {
  return {
    display_name: inputs.length > 0 ? `Union ${inputs[0].label}` : 'New union',
    description: '',
    union_type: 'by_name',
    input_node_ids: inputs.map((entry) => entry.id),
    input_node_labels: inputs.map((entry) => entry.label),
  };
}

export function composeUnionSql(draft: UnionDraft): string {
  if (draft.input_node_ids.length === 0) return 'SELECT 1 AS placeholder WHERE FALSE';
  const placeholders = draft.input_node_ids.map((_, index) => `__input_${index}__`);
  // Union by name: explicit projection per input is up to runtime to align;
  // the SQL we emit is `SELECT * FROM <input>` chained with UNION ALL. The
  // runtime is responsible for normalising column ordering before the union.
  // Union by position: same SQL, but runtime skips the column-alignment pass.
  const segments = placeholders.map((placeholder) => `SELECT * FROM ${placeholder}`);
  const operator = draft.union_type === 'by_name' ? 'UNION ALL BY NAME' : 'UNION ALL';
  return segments.join(`\n${operator}\n`);
}
