export type OntologyChangeKind =
  | 'object_upsert'
  | 'object_delete'
  | 'link_upsert'
  | 'link_delete'
  | 'clearances_revoked';

export interface OntologyChangeEvent<TPayload = unknown> {
  cursor: string;
  tenant: string;
  type_id?: string;
  object_id?: string;
  link_type?: string;
  kind: OntologyChangeKind;
  payload?: TPayload;
  occurred_ms: number;
}

export interface OntologySubscriptionOptions {
  tenant: string;
  typeId?: string;
  objectId?: string;
  linkType?: string;
  sinceCursor?: string;
  baseUrl?: string;
}

export interface OntologySubscriptionHandlers<TPayload = unknown> {
  onChange: (event: OntologyChangeEvent<TPayload>) => void;
  onError?: (error: Event | Error) => void;
  onClearancesRevoked?: () => void;
  eventSourceFactory?: (url: string) => EventSourceLike;
}

export interface EventSourceLike {
  onmessage: ((event: MessageEvent<string>) => void) | null;
  onerror: ((event: Event) => void) | null;
  addEventListener(type: string, listener: (event: MessageEvent<string>) => void): void;
  close(): void;
}

const API_BASE = import.meta.env.VITE_API_BASE_URL ?? '/api/v1';

// subscribeOntologyChanges is the OSDK-compatible typed client for OSV2.17
// subscriptions. Workshop reactive variables use the same helper so object and
// link changes share cursor/resume semantics across custom apps and OSDK code.
export function subscribeOntologyChanges<TPayload = unknown>(
  options: OntologySubscriptionOptions,
  handlers: OntologySubscriptionHandlers<TPayload>,
): { close: () => void } {
  const source = (handlers.eventSourceFactory ?? defaultEventSourceFactory)(buildOntologySubscriptionUrl(options));

  source.onmessage = (message) => {
    const event = parseChangeEvent<TPayload>(message.data);
    if (event.kind === 'clearances_revoked') {
      handlers.onClearancesRevoked?.();
      source.close();
      return;
    }
    handlers.onChange(event);
  };
  source.onerror = (event) => {
    handlers.onError?.(event);
  };
  source.addEventListener('clearances_revoked', () => {
    handlers.onClearancesRevoked?.();
    source.close();
  });

  return { close: () => source.close() };
}

// createWorkshopReactiveObjectVariable wires a Workshop variable-style callback
// to the OSV2 subscription stream and returns the same close handle as OSDK
// subscribe().
export function createWorkshopReactiveObjectVariable<TPayload = unknown>(
  options: OntologySubscriptionOptions,
  setValue: (event: OntologyChangeEvent<TPayload>) => void,
  handlers: Omit<OntologySubscriptionHandlers<TPayload>, 'onChange'> = {},
) {
  return subscribeOntologyChanges<TPayload>(options, { ...handlers, onChange: setValue });
}

export function buildOntologySubscriptionUrl(options: OntologySubscriptionOptions): string {
  const params = new URLSearchParams({ tenant: options.tenant });
  if (options.typeId) params.set('type_id', options.typeId);
  if (options.objectId) params.set('object_id', options.objectId);
  if (options.linkType) params.set('link_type', options.linkType);
  if (options.sinceCursor) params.set('since_cursor', options.sinceCursor);
  const base = options.baseUrl ?? API_BASE;
  return `${base}/object-database/subscriptions?${params.toString()}`;
}

function defaultEventSourceFactory(url: string): EventSourceLike {
  return new EventSource(url) as EventSourceLike;
}

function parseChangeEvent<TPayload>(raw: string): OntologyChangeEvent<TPayload> {
  const parsed = JSON.parse(raw) as OntologyChangeEvent<TPayload>;
  if (!parsed.cursor || !parsed.tenant || !parsed.kind) {
    throw new Error('invalid ontology subscription event');
  }
  return parsed;
}
