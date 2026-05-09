// Pipeline Builder join draft — types + SQL composition.
//
// A join links two upstream nodes (left + right) by one or more match
// conditions and produces a flat row with a chosen subset of columns
// from each side. The serialised draft lives in the resulting node's
// config so the editor can re-open it for further edits.

export type JoinType = 'left' | 'right' | 'inner' | 'outer' | 'cross';

export interface JoinMatch {
  left_column: string;
  right_column: string;
}

export interface JoinDraft {
  display_name: string;
  description?: string;
  join_type: JoinType;
  left_node_id: string;
  left_node_label: string;
  right_node_id: string;
  right_node_label: string;
  matches: JoinMatch[];
  left_columns: string[]; // selected from the left input
  right_columns: string[]; // selected from the right input
  right_prefix?: string; // optional alias prefix for right columns
  auto_select_left: boolean;
  auto_select_right: boolean;
}

export const JOIN_TYPES: Array<{ id: JoinType; label: string; description: string }> = [
  { id: 'left', label: 'Left join', description: 'Joins two datasets together, keeping all rows from the left table and only rows which satisfy the provided condition from the right table.' },
  { id: 'right', label: 'Right join', description: 'Keeps all rows from the right table and only rows from the left that satisfy the provided condition.' },
  { id: 'inner', label: 'Inner join', description: 'Keeps only the rows that satisfy the provided condition on both sides.' },
  { id: 'outer', label: 'Full outer join', description: 'Keeps all rows from both tables.' },
  { id: 'cross', label: 'Cross join', description: 'Cartesian product. No match condition is applied.' },
];

export function newJoinDraft(left: { id: string; label: string }, right: { id: string; label: string }): JoinDraft {
  return {
    display_name: `Join ${left.label}`,
    description: '',
    join_type: 'left',
    left_node_id: left.id,
    left_node_label: left.label,
    right_node_id: right.id,
    right_node_label: right.label,
    matches: [{ left_column: '', right_column: '' }],
    left_columns: [],
    right_columns: [],
    right_prefix: '',
    auto_select_left: true,
    auto_select_right: false,
  };
}

function quoteIdent(name: string): string {
  return `"${name.replace(/"/g, '""')}"`;
}

export function composeJoinSql(draft: JoinDraft): string {
  const joinKeyword = (() => {
    switch (draft.join_type) {
      case 'left':
        return 'LEFT JOIN';
      case 'right':
        return 'RIGHT JOIN';
      case 'inner':
        return 'INNER JOIN';
      case 'outer':
        return 'FULL OUTER JOIN';
      case 'cross':
        return 'CROSS JOIN';
    }
  })();

  const validMatches = draft.matches.filter((entry) => entry.left_column && entry.right_column);
  const onClause = draft.join_type === 'cross' || validMatches.length === 0
    ? ''
    : ` ON ${validMatches.map((entry) => `__left__.${quoteIdent(entry.left_column)} = __right__.${quoteIdent(entry.right_column)}`).join(' AND ')}`;

  const leftColumns = draft.auto_select_left
    ? `__left__.*`
    : draft.left_columns.map((name) => `__left__.${quoteIdent(name)}`).join(', ') || '__left__.*';

  const prefix = draft.right_prefix?.trim() ?? '';
  const rightColumns = draft.auto_select_right
    ? `__right__.*`
    : draft.right_columns
        .map((name) => prefix ? `__right__.${quoteIdent(name)} AS ${quoteIdent(`${prefix}${name}`)}` : `__right__.${quoteIdent(name)}`)
        .join(', ') || '';

  const projection = [leftColumns, rightColumns].filter((part) => part && part.trim().length > 0).join(', ');
  return `SELECT ${projection} FROM __left__ ${joinKeyword} __right__${onClause}`;
}
