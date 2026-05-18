import { describe, expect, it, vi } from 'vitest';

import {
  buildOntologySubscriptionUrl,
  createWorkshopReactiveObjectVariable,
  subscribeOntologyChanges,
  type EventSourceLike,
} from './ontology-subscriptions';

class FakeEventSource implements EventSourceLike {
  onmessage: ((event: MessageEvent<string>) => void) | null = null;
  onerror: ((event: Event) => void) | null = null;
  listeners = new Map<string, (event: MessageEvent<string>) => void>();
  closed = false;

  addEventListener(type: string, listener: (event: MessageEvent<string>) => void) {
    this.listeners.set(type, listener);
  }

  close() {
    this.closed = true;
  }

  emit(payload: unknown) {
    this.onmessage?.({ data: JSON.stringify(payload) } as MessageEvent<string>);
  }
}

describe('ontology subscriptions', () => {
  it('builds resumable object/link subscription URLs', () => {
    expect(
      buildOntologySubscriptionUrl({
        tenant: 'acme',
        typeId: 'Aircraft',
        objectId: 'obj-1',
        linkType: 'owns.asset',
        sinceCursor: '42',
        baseUrl: '/api/v1',
      }),
    ).toBe('/api/v1/object-database/subscriptions?tenant=acme&type_id=Aircraft&object_id=obj-1&link_type=owns.asset&since_cursor=42');
  });

  it('delivers typed OSDK subscription payloads', () => {
    const source = new FakeEventSource();
    const onChange = vi.fn();
    const sub = subscribeOntologyChanges<{ tail: string }>(
      { tenant: 'acme', typeId: 'Aircraft', baseUrl: '/api/v1' },
      { onChange, eventSourceFactory: () => source },
    );

    source.emit({ cursor: '1', tenant: 'acme', type_id: 'Aircraft', object_id: 'obj-1', kind: 'object_upsert', payload: { tail: 'EC-1' }, occurred_ms: 1 });

    expect(onChange).toHaveBeenCalledWith(expect.objectContaining({ object_id: 'obj-1', payload: { tail: 'EC-1' } }));
    sub.close();
    expect(source.closed).toBe(true);
  });

  it('closes Workshop reactive variables when clearances are revoked', () => {
    const source = new FakeEventSource();
    const onRevoked = vi.fn();
    createWorkshopReactiveObjectVariable(
      { tenant: 'acme', baseUrl: '/api/v1' },
      vi.fn(),
      { onClearancesRevoked: onRevoked, eventSourceFactory: () => source },
    );

    source.emit({ cursor: '2', tenant: 'acme', kind: 'clearances_revoked', occurred_ms: 2 });

    expect(onRevoked).toHaveBeenCalledTimes(1);
    expect(source.closed).toBe(true);
  });
});
