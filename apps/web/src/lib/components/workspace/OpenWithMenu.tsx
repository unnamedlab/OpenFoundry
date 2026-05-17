import { Link } from 'react-router-dom';

import {
  resolveOpenWithTargetsForResource,
  resourceTypeFromKind,
  type ResourceOpenContext,
} from '@/lib/compass/resourceTypeRegistry';
import { Glyph } from '@/lib/components/ui/Glyph';

interface OpenWithMenuProps {
  resourceKind: string;
  resourceId: string;
  resourceRid?: string | null;
  projectRid?: string | null;
  projectId?: string | null;
  openUrl?: string | null;
  label?: string;
  compact?: boolean;
  align?: 'left' | 'right';
  onOpen?: () => void;
}

export function OpenWithMenu({
  resourceKind,
  resourceId,
  resourceRid = null,
  projectRid = null,
  projectId = null,
  openUrl = null,
  label = 'Open with',
  compact = false,
  align = 'right',
  onOpen,
}: OpenWithMenuProps) {
  const context: ResourceOpenContext = {
    id: resourceId,
    rid: resourceRid,
    kind: resourceKind,
    type: resourceTypeFromKind(resourceKind),
    project_rid: projectRid,
    project_id: projectId,
    open_url: openUrl,
  };
  const targets = resolveOpenWithTargetsForResource(resourceKind, context);
  if (targets.length === 0) return null;

  const className = [
    'of-open-with',
    compact ? 'of-open-with--compact' : '',
    align === 'left' ? 'of-open-with--left' : 'of-open-with--right',
  ].filter(Boolean).join(' ');

  return (
    <details className={className}>
      <summary
        className="of-open-with__trigger"
        aria-label={label}
        title={label}
        onClick={(event) => event.stopPropagation()}
      >
        <Glyph name="external-link" size={13} />
        {!compact && <span>{label}</span>}
        <Glyph name="chevron-down" size={12} />
      </summary>
      <div className="of-open-with__menu" onClick={(event) => event.stopPropagation()}>
        {targets.map((target) => (
          <Link
            key={target.id}
            to={target.href}
            className="of-open-with__item"
            onClick={onOpen}
          >
            <Glyph name={target.icon} size={13} />
            <span>{target.label}</span>
          </Link>
        ))}
      </div>
    </details>
  );
}
