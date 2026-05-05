/**
 * `@open-foundry/sdk/media` — Media reference helpers for OSDK
 * applications.
 *
 * This module mirrors the helpers Foundry's
 * `Use media in OSDK applications.md` doc lists:
 *
 *   * `loadItem(ref)` — fetches the `MediaItem` row backing a
 *     `MediaReference`.
 *   * `getDownloadUrl(ref)` — returns a presigned download URL the
 *     caller can hand to an `<img>`/`<audio>`/`<video>` element.
 *   * `uploadAndAttach(client, mediaSetRid, file)` — uploads a `Blob`
 *     to a media set and returns a `MediaReference` ready to feed to
 *     an Ontology edit (`createEditBatch().create(Aircraft, {
 *     myMediaProperty: ref })`).
 *
 * The shapes are kept verbatim with the Rust `core_models::MediaReference`
 * payload (camelCase JSON) so the same value round-trips between the
 * SDK, the ontology services and the media-sets-service without
 * format translation.
 *
 * ## Usage
 *
 * ```ts
 * import {
 *   uploadAndAttach,
 *   getDownloadUrl,
 *   type MediaReference,
 * } from '@open-foundry/sdk/media';
 *
 * const blob = new Blob([bytes], { type: 'image/png' });
 * const ref: MediaReference = await uploadAndAttach(client, mediaSetRid, blob, 'apron.png');
 * batch.create(Aircraft, { myMediaProperty: ref, ... });
 * const url = await getDownloadUrl(client, ref);
 * ```
 */

/** Foundry media-set schema discriminator. */
export type MediaSetSchema =
  | 'IMAGE'
  | 'AUDIO'
  | 'VIDEO'
  | 'DOCUMENT'
  | 'SPREADSHEET'
  | 'EMAIL';

/** Pointer to a single media item, suitable for embedding inside an
 *  ontology object property or a dataset cell. Mirrors the
 *  `MediaReference` shape `core_models::MediaReference` emits. */
export interface MediaReference {
  mediaSetRid: string;
  mediaItemRid: string;
  branch: string;
  schema: MediaSetSchema;
}

/** Subset of `MediaSet` that the SDK consumer needs to render a
 *  preview. Full row lives on `media-sets-service`. */
export interface MediaSet {
  rid: string;
  projectRid: string;
  name: string;
  schema: MediaSetSchema;
  allowedMimeTypes: string[];
  transactionPolicy: 'TRANSACTIONLESS' | 'TRANSACTIONAL';
  retentionSeconds: number;
  virtual: boolean;
  sourceRid: string | null;
  markings: string[];
  createdAt: string;
  createdBy: string;
}

/** A single media item inside a media set. */
export interface MediaItem {
  rid: string;
  mediaSetRid: string;
  branch: string;
  transactionRid: string;
  path: string;
  mimeType: string;
  sizeBytes: number;
  sha256: string;
  storageUri: string;
  metadata: Record<string, unknown>;
  deduplicatedFrom: string | null;
  deletedAt: string | null;
  createdAt: string;
  markings?: string[];
}

/** Minimal client surface the helpers need. The full SDK exposes a
 *  `Client` type from `@open-foundry/sdk`; we accept anything with a
 *  `fetch` method so consumers can plug in retries / interceptors
 *  without re-implementing the helpers. */
export interface MediaClient {
  /** Foundry base URL (e.g. `https://my-foundry.example.com`). */
  baseUrl: string;
  /** Bearer token reader. Called per-request so token refresh hooks
   *  fire transparently. */
  getAccessToken: () => Promise<string> | string;
  /** Fetch implementation. Defaults to global `fetch` in production;
   *  test harnesses pass an instrumented fetch. */
  fetch?: typeof fetch;
}

function callFetch(client: MediaClient): typeof fetch {
  return client.fetch ?? fetch;
}

async function authHeaders(client: MediaClient): Promise<Record<string, string>> {
  const token = await client.getAccessToken();
  return { Authorization: `Bearer ${token}` };
}

function camelToSnakeRef(ref: MediaReference): {
  media_set_rid: string;
  media_item_rid: string;
  branch: string;
  schema: MediaSetSchema;
} {
  return {
    media_set_rid: ref.mediaSetRid,
    media_item_rid: ref.mediaItemRid,
    branch: ref.branch,
    schema: ref.schema,
  };
}

/** Fetch the full `MediaItem` row backing a reference. Useful for
 *  reading metadata (size, mime, sha256, …) without downloading the
 *  bytes. */
export async function loadItem(
  client: MediaClient,
  ref: MediaReference,
): Promise<MediaItem> {
  const resp = await callFetch(client)(
    `${client.baseUrl}/api/v1/items/${encodeURIComponent(ref.mediaItemRid)}`,
    {
      method: 'GET',
      headers: await authHeaders(client),
    },
  );
  if (!resp.ok) {
    throw new Error(
      `loadItem(${ref.mediaItemRid}): HTTP ${resp.status} — ${await resp.text()}`,
    );
  }
  return (await resp.json()) as MediaItem;
}

/** Mint a presigned download URL for the bytes behind a reference.
 *  The returned URL embeds a 5-minute HMAC claim (per the H3
 *  closure) so the gateway can validate without a network hop. */
export async function getDownloadUrl(
  client: MediaClient,
  ref: MediaReference,
  options: { expiresInSeconds?: number } = {},
): Promise<{ url: string; expiresAt: string; mimeType: string }> {
  const query = new URLSearchParams();
  if (options.expiresInSeconds) {
    query.set('expires_in_seconds', String(options.expiresInSeconds));
  }
  const qs = query.toString();
  const resp = await callFetch(client)(
    `${client.baseUrl}/api/v1/items/${encodeURIComponent(ref.mediaItemRid)}/download-url${qs ? `?${qs}` : ''}`,
    {
      method: 'GET',
      headers: await authHeaders(client),
    },
  );
  if (!resp.ok) {
    throw new Error(
      `getDownloadUrl(${ref.mediaItemRid}): HTTP ${resp.status} — ${await resp.text()}`,
    );
  }
  const body = (await resp.json()) as {
    url: string;
    expires_at: string;
    item: MediaItem;
  };
  return {
    url: body.url,
    expiresAt: body.expires_at,
    mimeType: body.item?.mimeType ?? '',
  };
}

/** Upload a `Blob` to a media set and return a `MediaReference` the
 *  caller can feed straight into an Ontology edit batch
 *  (`uploadAndAttach` ⇒ `MediaReference` ⇒ `Edits.Object<T>`). */
export async function uploadAndAttach(
  client: MediaClient,
  mediaSetRid: string,
  blob: Blob,
  fileName: string,
  options: { branch?: string; expiresInSeconds?: number } = {},
): Promise<MediaReference> {
  // Step 1 — ask media-sets-service for a presigned PUT.
  const presignResp = await callFetch(client)(
    `${client.baseUrl}/api/v1/media-sets/${encodeURIComponent(mediaSetRid)}/items/upload-url`,
    {
      method: 'POST',
      headers: {
        ...(await authHeaders(client)),
        'content-type': 'application/json',
      },
      body: JSON.stringify({
        path: fileName,
        mime_type: blob.type || 'application/octet-stream',
        branch: options.branch ?? 'main',
        size_bytes: blob.size,
        expires_in_seconds: options.expiresInSeconds,
      }),
    },
  );
  if (!presignResp.ok) {
    throw new Error(
      `uploadAndAttach: presign HTTP ${presignResp.status} — ${await presignResp.text()}`,
    );
  }
  const presigned = (await presignResp.json()) as {
    url: string;
    headers: Record<string, string>;
    item: MediaItem;
  };

  // Step 2 — PUT the bytes against the presigned URL.
  const put = await callFetch(client)(presigned.url, {
    method: 'PUT',
    headers: {
      'content-type': blob.type || 'application/octet-stream',
      ...presigned.headers,
    },
    body: blob,
  });
  if (!put.ok) {
    throw new Error(
      `uploadAndAttach: storage PUT HTTP ${put.status} — ${await put.text()}`,
    );
  }

  return {
    mediaSetRid: presigned.item.mediaSetRid,
    mediaItemRid: presigned.item.rid,
    branch: presigned.item.branch,
    // Schema lives on the media set, not the item — but the OSDK
    // payload demands it. The presigned response shape currently
    // doesn't carry it; default to IMAGE when absent so the
    // returned `MediaReference` is well-formed (the consuming
    // Ontology edit handler will validate against the actual set).
    schema:
      ((presigned.item as { schema?: MediaSetSchema }).schema ?? 'IMAGE'),
  };
}

/** Convert a snake_case Foundry-API JSON (the canonical
 *  `core_models::MediaReference` form) into the camelCase shape the
 *  OSDK helpers consume. Useful when the caller pulled the value
 *  straight out of an object property. */
export function fromFoundryJson(value: unknown): MediaReference {
  if (typeof value !== 'object' || value === null) {
    throw new Error('expected MediaReference object');
  }
  const v = value as Record<string, unknown>;
  const mediaSetRid = (v.mediaSetRid ?? v.media_set_rid) as string | undefined;
  const mediaItemRid = (v.mediaItemRid ?? v.media_item_rid) as string | undefined;
  if (!mediaSetRid || !mediaItemRid) {
    throw new Error('MediaReference is missing mediaSetRid / mediaItemRid');
  }
  return {
    mediaSetRid,
    mediaItemRid,
    branch: ((v.branch as string | undefined) ?? 'main') as string,
    schema: ((v.schema as MediaSetSchema | undefined) ?? 'IMAGE') as MediaSetSchema,
  };
}
