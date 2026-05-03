//! Multi-hop graph traversal backed by `LinkStore`.
//!
//! Foundry exposes object graphs with a `?graph(depth=N, link_types=[...])`
//! style query. The legacy implementation walked the Postgres
//! `link_instances` table with a recursive CTE; the migrated version performs
//! the expansion through `storage-abstraction` so the same logic works against
//! Cassandra-backed adjacency indexes.

use std::collections::{HashMap, HashSet, VecDeque};

use auth_middleware::claims::Claims;
use chrono::{DateTime, TimeZone, Utc};
use serde::Serialize;
use storage_abstraction::repositories::{
    Link, LinkStore, LinkTypeId, ObjectId, ObjectStore, Page, ReadConsistency, RepoError, TenantId,
};
use thiserror::Error;
use uuid::Uuid;

use crate::{
    AppState,
    domain::{
        access::{clearance_rank, marking_rank},
        composition,
    },
};

/// One edge returned by [`traverse`].
#[derive(Debug, Clone, Serialize)]
pub struct TraversedEdge {
    pub link_id: Uuid,
    pub link_type_id: Uuid,
    pub source_object_id: Uuid,
    pub target_object_id: Uuid,
    pub marking: String,
    pub depth: i32,
    pub created_at: DateTime<Utc>,
}

#[derive(Debug, Clone)]
pub struct TraversalParams {
    pub starting_object_id: Uuid,
    pub max_depth: i32,
    pub link_type_ids: Option<Vec<Uuid>>,
    pub marking_filter: Option<Vec<String>>,
    pub limit: i32,
}

#[derive(Debug, Error)]
pub enum TraversalError {
    #[error(transparent)]
    Repo(#[from] RepoError),
    #[error(transparent)]
    Sql(#[from] sqlx::Error),
}

/// Returns the set of allowed markings for a caller. If the caller passed an
/// explicit `marking_filter` we honour it; otherwise we derive it from the
/// caller's clearance using the same hierarchy as
/// `domain::access::ensure_object_access` (public ⊂ confidential ⊂ pii).
fn resolve_marking_filter(claims: &Claims, marking_filter: Option<Vec<String>>) -> Vec<String> {
    if let Some(filter) = marking_filter.filter(|f| !f.is_empty()) {
        return filter;
    }
    if claims.has_role("admin") {
        return vec![
            "public".to_string(),
            "confidential".to_string(),
            "pii".to_string(),
        ];
    }
    let granted = clearance_rank(claims);
    let mut allowed = vec!["public".to_string()];
    if granted >= 1 {
        allowed.push("confidential".to_string());
    }
    if granted >= 2 {
        allowed.push("pii".to_string());
    }
    allowed
}

fn tenant_from_claims(claims: &Claims) -> TenantId {
    TenantId(
        claims
            .org_id
            .map(|id| id.to_string())
            .unwrap_or_else(|| "default".to_string()),
    )
}

fn to_uuid(kind: &str, value: &str) -> Result<Uuid, RepoError> {
    Uuid::parse_str(value).map_err(|error| {
        RepoError::InvalidArgument(format!("invalid {kind} uuid '{value}': {error}"))
    })
}

fn created_at_from_ms(created_at_ms: i64) -> DateTime<Utc> {
    Utc.timestamp_millis_opt(created_at_ms)
        .single()
        .unwrap_or(DateTime::<Utc>::UNIX_EPOCH)
}

fn edge_marking_from_rank(rank: u8) -> &'static str {
    match rank {
        2 => "pii",
        1 => "confidential",
        _ => "public",
    }
}

async fn resolve_object_markings(
    objects: &dyn ObjectStore,
    tenant: &TenantId,
    object_id: &ObjectId,
    cache: &mut HashMap<ObjectId, Vec<String>>,
) -> Result<Vec<String>, RepoError> {
    if let Some(markings) = cache.get(object_id) {
        return Ok(markings.clone());
    }

    let markings = objects
        .get(tenant, object_id, ReadConsistency::Eventual)
        .await?
        .map(|object| {
            let resolved: Vec<String> = object
                .markings
                .into_iter()
                .map(|marking| marking.0)
                .collect();
            if resolved.is_empty() {
                vec!["public".to_string()]
            } else {
                resolved
            }
        })
        .unwrap_or_else(|| vec!["public".to_string()]);

    cache.insert(object_id.clone(), markings.clone());
    Ok(markings)
}

async fn derive_edge_marking(
    objects: &dyn ObjectStore,
    tenant: &TenantId,
    source: &ObjectId,
    target: &ObjectId,
    cache: &mut HashMap<ObjectId, Vec<String>>,
) -> Result<String, RepoError> {
    let mut strongest = 0u8;
    for markings in [
        resolve_object_markings(objects, tenant, source, cache).await?,
        resolve_object_markings(objects, tenant, target, cache).await?,
    ] {
        for marking in markings {
            strongest = strongest.max(marking_rank(&marking).unwrap_or(0));
        }
    }
    Ok(edge_marking_from_rank(strongest).to_string())
}

pub(crate) async fn collect_links(
    links: &dyn LinkStore,
    tenant: &TenantId,
    node: &ObjectId,
    link_types: &[LinkTypeId],
    budget: usize,
) -> Result<Vec<Link>, RepoError> {
    let mut collected = Vec::new();

    for link_type in link_types {
        let mut outgoing_page = Page {
            size: budget.clamp(1, 5_000) as u32,
            token: None,
        };
        loop {
            let result = links
                .list_outgoing(
                    tenant,
                    link_type,
                    node,
                    outgoing_page.clone(),
                    ReadConsistency::Eventual,
                )
                .await?;
            collected.extend(result.items);
            if collected.len() >= budget || result.next_token.is_none() {
                break;
            }
            outgoing_page.token = result.next_token;
        }

        if collected.len() >= budget {
            break;
        }

        let mut incoming_page = Page {
            size: (budget - collected.len()).clamp(1, 5_000) as u32,
            token: None,
        };
        loop {
            let result = links
                .list_incoming(
                    tenant,
                    link_type,
                    node,
                    incoming_page.clone(),
                    ReadConsistency::Eventual,
                )
                .await?;
            collected.extend(result.items);
            if collected.len() >= budget || result.next_token.is_none() {
                break;
            }
            incoming_page.token = result.next_token;
        }

        if collected.len() >= budget {
            break;
        }
    }

    Ok(collected)
}

async fn resolve_link_types(
    state: &AppState,
    requested: Option<Vec<Uuid>>,
) -> Result<Vec<LinkTypeId>, sqlx::Error> {
    if let Some(ids) = requested.filter(|ids| !ids.is_empty()) {
        return Ok(ids
            .into_iter()
            .map(|id| LinkTypeId(id.to_string()))
            .collect());
    }

    let rows = sqlx::query_scalar::<_, Uuid>("SELECT id FROM link_types ORDER BY created_at DESC")
        .fetch_all(&state.db)
        .await?;
    Ok(rows
        .into_iter()
        .map(|id| LinkTypeId(id.to_string()))
        .collect())
}

async fn traverse_with_types(
    objects: &dyn ObjectStore,
    links: &dyn LinkStore,
    claims: &Claims,
    params: TraversalParams,
    link_types: Vec<LinkTypeId>,
) -> Result<Vec<TraversedEdge>, TraversalError> {
    let max_depth = params.max_depth.clamp(1, 5);
    let limit = params.limit.clamp(1, 5_000) as usize;
    if link_types.is_empty() {
        return Ok(Vec::new());
    }

    let tenant = tenant_from_claims(claims);
    let allowed_markings: HashSet<String> = resolve_marking_filter(claims, params.marking_filter)
        .into_iter()
        .collect();
    let start = ObjectId(params.starting_object_id.to_string());

    let mut edges = Vec::new();
    let mut queue = VecDeque::from([(start.clone(), 0i32)]);
    let mut seen_nodes = HashMap::from([(start, 0i32)]);
    let mut seen_edges = HashSet::new();
    let mut object_marking_cache = HashMap::new();

    while let Some((node, depth)) = queue.pop_front() {
        if depth >= max_depth || edges.len() >= limit {
            continue;
        }

        let adjacent =
            collect_links(links, &tenant, &node, &link_types, limit - edges.len()).await?;
        for link in adjacent {
            let link_id = composition::stable_link_id(&link.link_type, &link.from, &link.to);
            let edge_depth = depth + 1;
            let marking = derive_edge_marking(
                objects,
                &tenant,
                &link.from,
                &link.to,
                &mut object_marking_cache,
            )
            .await?;
            if !allowed_markings.contains(&marking) {
                continue;
            }

            let neighbour = if link.from == node {
                link.to.clone()
            } else {
                link.from.clone()
            };

            if seen_edges.insert(link_id) {
                edges.push(TraversedEdge {
                    link_id,
                    link_type_id: to_uuid("link_type_id", &link.link_type.0)?,
                    source_object_id: to_uuid("source_object_id", &link.from.0)?,
                    target_object_id: to_uuid("target_object_id", &link.to.0)?,
                    marking,
                    depth: edge_depth,
                    created_at: created_at_from_ms(link.created_at_ms),
                });
                if edges.len() >= limit {
                    break;
                }
            }

            if edge_depth < max_depth
                && seen_nodes
                    .get(&neighbour)
                    .is_none_or(|known_depth| edge_depth < *known_depth)
            {
                seen_nodes.insert(neighbour.clone(), edge_depth);
                queue.push_back((neighbour, edge_depth));
            }
        }
    }

    Ok(edges)
}

/// Multi-hop traversal backed by the configured `LinkStore`.
pub async fn traverse(
    state: &AppState,
    claims: &Claims,
    params: TraversalParams,
) -> Result<Vec<TraversedEdge>, TraversalError> {
    let link_types = resolve_link_types(state, params.link_type_ids.clone()).await?;
    traverse_with_types(
        state.stores.objects.as_ref(),
        state.stores.links.as_ref(),
        claims,
        params,
        link_types,
    )
    .await
}

#[cfg(test)]
mod tests {
    use super::*;
    use auth_middleware::claims::Claims;
    use serde_json::json;
    use storage_abstraction::repositories::{
        Link, MarkingId, Object, OwnerId, PutOutcome, TypeId,
        noop::{InMemoryLinkStore, InMemoryObjectStore},
    };

    fn test_claims(clearance: &str) -> Claims {
        Claims {
            sub: Uuid::nil(),
            iat: 0,
            exp: i64::MAX,
            iss: None,
            aud: None,
            jti: Uuid::nil(),
            email: "test@example.com".to_string(),
            name: "Test".to_string(),
            roles: Vec::new(),
            permissions: Vec::new(),
            org_id: Some(Uuid::nil()),
            attributes: json!({ "classification_clearance": clearance }),
            auth_methods: Vec::new(),
            token_use: None,
            api_key_id: None,
            session_kind: None,
            session_scope: None,
        }
    }

    #[tokio::test]
    async fn traverses_both_directions_through_link_store() {
        let objects = InMemoryObjectStore::default();
        let links = InMemoryLinkStore::default();
        let tenant = TenantId(Uuid::nil().to_string());
        let start_id = Uuid::now_v7();
        let other_public_id = Uuid::now_v7();
        let confidential_id = Uuid::now_v7();

        for (id, marking) in [
            (start_id, "public"),
            (other_public_id, "public"),
            (confidential_id, "confidential"),
        ] {
            let outcome = objects
                .put(
                    Object {
                        tenant: tenant.clone(),
                        id: ObjectId(id.to_string()),
                        type_id: TypeId(Uuid::now_v7().to_string()),
                        version: 1,
                        payload: json!({}),
                        organization_id: None,
                        created_at_ms: Some(1),
                        updated_at_ms: 1,
                        owner: Some(OwnerId(Uuid::nil().to_string())),
                        markings: vec![MarkingId(marking.to_string())],
                    },
                    None,
                )
                .await
                .unwrap();
            assert!(matches!(outcome, PutOutcome::Inserted));
        }

        let link_type = LinkTypeId(Uuid::now_v7().to_string());
        links
            .put(Link {
                tenant: tenant.clone(),
                link_type: link_type.clone(),
                from: ObjectId(start_id.to_string()),
                to: ObjectId(other_public_id.to_string()),
                payload: Some(json!({})),
                created_at_ms: 10,
            })
            .await
            .unwrap();
        links
            .put(Link {
                tenant: tenant.clone(),
                link_type: link_type.clone(),
                from: ObjectId(confidential_id.to_string()),
                to: ObjectId(other_public_id.to_string()),
                payload: Some(json!({})),
                created_at_ms: 20,
            })
            .await
            .unwrap();

        let edges = traverse_with_types(
            &objects,
            &links,
            &test_claims("pii"),
            TraversalParams {
                starting_object_id: start_id,
                max_depth: 3,
                link_type_ids: None,
                marking_filter: None,
                limit: 10,
            },
            vec![link_type],
        )
        .await
        .unwrap();

        assert_eq!(edges.len(), 2);
        assert!(edges.iter().any(|edge| edge.depth == 1));
        assert!(edges.iter().any(|edge| edge.depth == 2));
    }

    #[tokio::test]
    async fn filters_edges_by_derived_marking() {
        let objects = InMemoryObjectStore::default();
        let links = InMemoryLinkStore::default();
        let tenant = TenantId(Uuid::nil().to_string());
        let start = Uuid::now_v7();
        let target = Uuid::now_v7();
        let link_type = LinkTypeId(Uuid::now_v7().to_string());

        for (id, marking) in [(start, "public"), (target, "pii")] {
            let outcome = objects
                .put(
                    Object {
                        tenant: tenant.clone(),
                        id: ObjectId(id.to_string()),
                        type_id: TypeId(Uuid::now_v7().to_string()),
                        version: 1,
                        payload: json!({}),
                        organization_id: None,
                        created_at_ms: Some(1),
                        updated_at_ms: 1,
                        owner: Some(OwnerId(Uuid::nil().to_string())),
                        markings: vec![MarkingId(marking.to_string())],
                    },
                    None,
                )
                .await
                .unwrap();
            assert!(matches!(outcome, PutOutcome::Inserted));
        }

        links
            .put(Link {
                tenant,
                link_type: link_type.clone(),
                from: ObjectId(start.to_string()),
                to: ObjectId(target.to_string()),
                payload: Some(json!({})),
                created_at_ms: 10,
            })
            .await
            .unwrap();

        let edges = traverse_with_types(
            &objects,
            &links,
            &test_claims("public"),
            TraversalParams {
                starting_object_id: start,
                max_depth: 2,
                link_type_ids: None,
                marking_filter: None,
                limit: 10,
            },
            vec![link_type],
        )
        .await
        .unwrap();

        assert!(edges.is_empty());
    }
}
