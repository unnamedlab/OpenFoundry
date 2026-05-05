//! FASE 4 — node-level preview engine.
//!
//! Pure-Rust forward evaluator for the canonical Pipeline Builder
//! transforms (passthrough, cast, title_case, clean_string, filter,
//! join, union). Designed to be called from
//! `pipeline-authoring-service`'s preview handler. The engine accepts
//! any node shape that implements [`PreviewNodeView`] so the service
//! can plug in its persisted `PipelineNode` directly.

use std::collections::{HashMap, HashSet, VecDeque};

use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use serde_json::Value as Json;

use crate::eval::{EvalValue, Row, eval};
use crate::parser::parse_expr;

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq)]
pub struct PreviewOutput {
    pub pipeline_id: String,
    pub node_id: String,
    pub columns: Vec<String>,
    pub rows: Vec<HashMap<String, Json>>,
    pub sample_size: usize,
    pub generated_at: DateTime<Utc>,
    pub seed: u64,
    pub source_chain: Vec<String>,
    pub fresh: bool,
}

#[derive(Debug, Clone, thiserror::Error)]
pub enum PreviewError {
    #[error("node '{0}' not found in pipeline")]
    NodeNotFound(String),
    #[error("cycle detected reaching node '{0}'")]
    Cycle(String),
    #[error("seed loader failed for node '{node_id}': {message}")]
    SeedLoader { node_id: String, message: String },
    #[error("transform '{transform}' on node '{node_id}': {message}")]
    Transform {
        node_id: String,
        transform: String,
        message: String,
    },
}

pub trait PreviewNodeView {
    fn id(&self) -> &str;
    fn transform_type(&self) -> &str;
    fn config(&self) -> &Json;
    fn depends_on(&self) -> &[String];
}

pub trait SeedLoader {
    fn load<N: PreviewNodeView>(&self, node: &N, sample_size: usize) -> Result<Vec<Row>, String>;
}

/// Default loader: deterministic synthetic rows seeded by
/// `pipeline_id` + `node_id`. Used when no real dataset is bound.
pub struct DeterministicSeedLoader {
    pub pipeline_id: String,
}

impl SeedLoader for DeterministicSeedLoader {
    fn load<N: PreviewNodeView>(
        &self,
        node: &N,
        sample_size: usize,
    ) -> Result<Vec<Row>, String> {
        let seed = compute_seed(&self.pipeline_id, node.id());
        Ok(synthesise_rows(seed, node.id(), sample_size.min(50)))
    }
}

const DEFAULT_SAMPLE_SIZE: usize = 50_000;

pub fn preview_node<N: PreviewNodeView, L: SeedLoader>(
    pipeline_id: &str,
    node_id: &str,
    nodes: &[N],
    loader: &L,
    sample_size: Option<usize>,
) -> Result<PreviewOutput, PreviewError> {
    if !nodes.iter().any(|n| n.id() == node_id) {
        return Err(PreviewError::NodeNotFound(node_id.to_string()));
    }
    let chain = topological_chain(node_id, nodes)?;
    let cap = sample_size.unwrap_or(DEFAULT_SAMPLE_SIZE).min(DEFAULT_SAMPLE_SIZE);
    let seed = compute_seed(pipeline_id, node_id);

    let mut produced: HashMap<String, Vec<Row>> = HashMap::new();
    for nid in &chain {
        let node = nodes
            .iter()
            .find(|n| n.id() == nid)
            .ok_or_else(|| PreviewError::NodeNotFound(nid.clone()))?;
        let rows = if node.depends_on().is_empty() {
            loader
                .load(node, cap)
                .map_err(|message| PreviewError::SeedLoader {
                    node_id: nid.clone(),
                    message,
                })?
        } else {
            apply_transform(node, &produced, cap)?
        };
        produced.insert(nid.clone(), rows);
    }

    let final_rows = produced
        .remove(node_id)
        .ok_or_else(|| PreviewError::NodeNotFound(node_id.to_string()))?;
    let columns = derive_columns(&final_rows);

    Ok(PreviewOutput {
        pipeline_id: pipeline_id.to_string(),
        node_id: node_id.to_string(),
        sample_size: final_rows.len(),
        columns,
        rows: final_rows,
        generated_at: Utc::now(),
        seed,
        source_chain: chain,
        fresh: true,
    })
}

fn topological_chain<N: PreviewNodeView>(
    target: &str,
    nodes: &[N],
) -> Result<Vec<String>, PreviewError> {
    let by_id: HashMap<&str, &N> = nodes.iter().map(|n| (n.id(), n)).collect();
    if !by_id.contains_key(target) {
        return Err(PreviewError::NodeNotFound(target.to_string()));
    }

    let mut needed: HashSet<String> = HashSet::new();
    let mut queue: VecDeque<String> = VecDeque::new();
    queue.push_back(target.to_string());
    while let Some(id) = queue.pop_front() {
        if !needed.insert(id.clone()) {
            continue;
        }
        if let Some(node) = by_id.get(id.as_str()) {
            for dep in node.depends_on() {
                queue.push_back(dep.clone());
            }
        }
    }

    let mut indeg: HashMap<String, usize> = HashMap::new();
    let mut adj: HashMap<String, Vec<String>> = HashMap::new();
    for id in &needed {
        indeg.entry(id.clone()).or_insert(0);
        adj.entry(id.clone()).or_default();
    }
    for id in &needed {
        if let Some(node) = by_id.get(id.as_str()) {
            for dep in node.depends_on() {
                if !needed.contains(dep) {
                    continue;
                }
                *indeg.entry(id.clone()).or_insert(0) += 1;
                adj.entry(dep.clone()).or_default().push(id.clone());
            }
        }
    }

    let mut frontier: Vec<String> = indeg
        .iter()
        .filter(|&(_, &d)| d == 0)
        .map(|(k, _)| k.clone())
        .collect();
    frontier.sort();
    frontier.reverse();
    let mut order = Vec::new();
    while let Some(id) = frontier.pop() {
        order.push(id.clone());
        if let Some(children) = adj.remove(&id) {
            let mut next = Vec::new();
            for child in children {
                if let Some(d) = indeg.get_mut(&child) {
                    *d -= 1;
                    if *d == 0 {
                        next.push(child);
                    }
                }
            }
            next.sort();
            for c in next.into_iter().rev() {
                frontier.push(c);
            }
        }
    }
    if order.len() != needed.len() {
        return Err(PreviewError::Cycle(target.to_string()));
    }
    Ok(order)
}

fn apply_transform<N: PreviewNodeView>(
    node: &N,
    produced: &HashMap<String, Vec<Row>>,
    cap: usize,
) -> Result<Vec<Row>, PreviewError> {
    let kind = node.transform_type().to_ascii_lowercase();
    let upstream: Vec<&Vec<Row>> = node
        .depends_on()
        .iter()
        .filter_map(|d| produced.get(d))
        .collect();
    if upstream.is_empty() {
        return Ok(Vec::new());
    }
    let primary = upstream[0];

    match kind.as_str() {
        "passthrough" => Ok(take_rows(primary, cap)),
        "cast" | "title_case" | "clean_string" => apply_columns_transform(node, primary, cap, &kind),
        "filter" => apply_filter(node, primary, cap),
        "join" => {
            if upstream.len() < 2 {
                return Err(PreviewError::Transform {
                    node_id: node.id().to_string(),
                    transform: kind,
                    message: "join requires two upstream tables".into(),
                });
            }
            apply_join(node, primary, upstream[1], cap)
        }
        "union" => {
            let mut merged = Vec::new();
            for table in upstream {
                for row in table {
                    if merged.len() >= cap {
                        return Ok(merged);
                    }
                    merged.push(row.clone());
                }
            }
            Ok(merged)
        }
        _ => Ok(take_rows(primary, cap)),
    }
}

fn apply_columns_transform<N: PreviewNodeView>(
    node: &N,
    primary: &[Row],
    cap: usize,
    kind: &str,
) -> Result<Vec<Row>, PreviewError> {
    let columns = node
        .config()
        .get("columns")
        .and_then(Json::as_array)
        .map(|arr| {
            arr.iter()
                .filter_map(|v| v.as_str().map(String::from))
                .collect::<Vec<_>>()
        })
        .unwrap_or_default();

    let target = node
        .config()
        .get("cast_target")
        .and_then(Json::as_str)
        .unwrap_or("STRING")
        .to_string();

    let mut out = Vec::with_capacity(primary.len().min(cap));
    for row in primary.iter().take(cap) {
        let mut next = row.clone();
        for col in &columns {
            if let Some(value) = next.get(col).cloned() {
                let evaled = EvalValue::from_json(&value);
                let transformed = match kind {
                    "title_case" => match evaled {
                        EvalValue::String(s) => EvalValue::String(title_case(&s)),
                        other => other,
                    },
                    "clean_string" => match evaled {
                        EvalValue::String(s) => EvalValue::String(clean_string(&s)),
                        other => other,
                    },
                    "cast" => cast_helper(evaled, &target).map_err(|message| {
                        PreviewError::Transform {
                            node_id: node.id().to_string(),
                            transform: kind.to_string(),
                            message,
                        }
                    })?,
                    _ => evaled,
                };
                next.insert(col.clone(), transformed.to_json());
            }
        }
        out.push(next);
    }
    Ok(out)
}

fn apply_filter<N: PreviewNodeView>(
    node: &N,
    primary: &[Row],
    cap: usize,
) -> Result<Vec<Row>, PreviewError> {
    let predicate = node
        .config()
        .get("predicate")
        .and_then(Json::as_str)
        .ok_or_else(|| PreviewError::Transform {
            node_id: node.id().to_string(),
            transform: "filter".into(),
            message: "missing `predicate`".into(),
        })?;
    let parsed = parse_expr(predicate).map_err(|e| PreviewError::Transform {
        node_id: node.id().to_string(),
        transform: "filter".into(),
        message: format!("predicate parse error: {e}"),
    })?;
    let mut out = Vec::new();
    for row in primary.iter().take(cap) {
        let result = eval(&parsed, row).map_err(|e| PreviewError::Transform {
            node_id: node.id().to_string(),
            transform: "filter".into(),
            message: e.to_string(),
        })?;
        if matches!(result, EvalValue::Bool(true)) {
            out.push(row.clone());
        }
    }
    Ok(out)
}

fn apply_join<N: PreviewNodeView>(
    node: &N,
    left: &[Row],
    right: &[Row],
    cap: usize,
) -> Result<Vec<Row>, PreviewError> {
    let how = node
        .config()
        .get("how")
        .and_then(Json::as_str)
        .unwrap_or("inner")
        .to_ascii_lowercase();
    let on = node
        .config()
        .get("on")
        .and_then(Json::as_array)
        .map(|arr| {
            arr.iter()
                .filter_map(|v| v.as_str().map(String::from))
                .collect::<Vec<_>>()
        })
        .unwrap_or_default();
    if on.is_empty() {
        return Err(PreviewError::Transform {
            node_id: node.id().to_string(),
            transform: "join".into(),
            message: "missing `on` keys".into(),
        });
    }

    let mut index: HashMap<String, Vec<&Row>> = HashMap::new();
    for row in right {
        index.entry(join_key(row, &on)).or_default().push(row);
    }

    let mut out = Vec::new();
    for left_row in left {
        let key = join_key(left_row, &on);
        if let Some(matches) = index.get(&key) {
            for right_row in matches {
                if out.len() >= cap {
                    return Ok(out);
                }
                let mut merged = left_row.clone();
                for (k, v) in right_row.iter() {
                    merged.entry(k.clone()).or_insert_with(|| v.clone());
                }
                out.push(merged);
            }
        } else if how == "left" {
            if out.len() >= cap {
                return Ok(out);
            }
            out.push(left_row.clone());
        }
    }
    Ok(out)
}

fn take_rows(rows: &[Row], cap: usize) -> Vec<Row> {
    rows.iter().take(cap).cloned().collect()
}

fn derive_columns(rows: &[Row]) -> Vec<String> {
    if let Some(first) = rows.first() {
        let mut keys: Vec<String> = first.keys().cloned().collect();
        keys.sort();
        return keys;
    }
    Vec::new()
}

fn join_key(row: &Row, keys: &[String]) -> String {
    keys.iter()
        .map(|k| match row.get(k) {
            Some(v) => v.to_string(),
            None => "<null>".to_string(),
        })
        .collect::<Vec<_>>()
        .join("\u{1f}")
}

fn compute_seed(pipeline_id: &str, node_id: &str) -> u64 {
    use std::collections::hash_map::DefaultHasher;
    use std::hash::{Hash, Hasher};
    let mut hasher = DefaultHasher::new();
    (pipeline_id, node_id).hash(&mut hasher);
    hasher.finish()
}

fn synthesise_rows(seed: u64, node_id: &str, cap: usize) -> Vec<Row> {
    let mut state = if seed == 0 { 0xdead_beef } else { seed };
    let mut out = Vec::with_capacity(cap);
    for i in 0..cap {
        state ^= state << 13;
        state ^= state >> 7;
        state ^= state << 17;
        let mut row = HashMap::new();
        row.insert("id".to_string(), Json::from(i as i64));
        row.insert("source_node".to_string(), Json::String(node_id.to_string()));
        row.insert("synthetic".to_string(), Json::Bool(true));
        row.insert("value".to_string(), Json::from((state % 1000) as i64));
        out.push(row);
    }
    out
}

fn title_case(s: &str) -> String {
    let mut out = String::with_capacity(s.len());
    let mut prev_alpha = false;
    for ch in s.chars() {
        if ch.is_alphabetic() {
            if prev_alpha {
                out.extend(ch.to_lowercase());
            } else {
                out.extend(ch.to_uppercase());
            }
            prev_alpha = true;
        } else {
            out.push(ch);
            prev_alpha = false;
        }
    }
    out
}

fn clean_string(s: &str) -> String {
    s.split_whitespace().collect::<Vec<_>>().join(" ")
}

fn cast_helper(v: EvalValue, target: &str) -> Result<EvalValue, String> {
    let upper = target.trim().to_ascii_uppercase();
    match (upper.as_str(), v.clone()) {
        ("STRING", EvalValue::String(_)) => Ok(v),
        ("STRING", EvalValue::Integer(i)) => Ok(EvalValue::String(i.to_string())),
        ("STRING", EvalValue::Double(d)) => Ok(EvalValue::String(d.to_string())),
        ("STRING", EvalValue::Bool(b)) => Ok(EvalValue::String(b.to_string())),
        ("INTEGER" | "LONG", EvalValue::Integer(_)) => Ok(v),
        ("INTEGER" | "LONG", EvalValue::Double(d)) => Ok(EvalValue::Integer(d as i64)),
        ("INTEGER" | "LONG", EvalValue::String(s)) => s
            .parse::<i64>()
            .map(EvalValue::Integer)
            .map_err(|_| format!("cannot cast '{s}' to {upper}")),
        ("DOUBLE" | "DECIMAL", EvalValue::Integer(i)) => Ok(EvalValue::Double(i as f64)),
        ("DOUBLE" | "DECIMAL", EvalValue::Double(_)) => Ok(v),
        ("DOUBLE" | "DECIMAL", EvalValue::String(s)) => s
            .parse::<f64>()
            .map(EvalValue::Double)
            .map_err(|_| format!("cannot cast '{s}' to {upper}")),
        ("BOOLEAN", EvalValue::Bool(_)) => Ok(v),
        (_, EvalValue::Null) => Ok(EvalValue::Null),
        (target, value) => Err(format!(
            "cannot cast {:?} to {target}",
            value.type_hint()
        )),
    }
}

/// Convenience implementation of [`PreviewNodeView`] for tests +
/// callers that already have a JSON `Value` per node.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct JsonPipelineNode {
    pub id: String,
    pub transform_type: String,
    pub config: Json,
    pub depends_on: Vec<String>,
}

impl PreviewNodeView for JsonPipelineNode {
    fn id(&self) -> &str {
        &self.id
    }
    fn transform_type(&self) -> &str {
        &self.transform_type
    }
    fn config(&self) -> &Json {
        &self.config
    }
    fn depends_on(&self) -> &[String] {
        &self.depends_on
    }
}

impl JsonPipelineNode {
    pub fn new(
        id: impl Into<String>,
        transform_type: impl Into<String>,
        config: Json,
        depends_on: &[&str],
    ) -> Self {
        Self {
            id: id.into(),
            transform_type: transform_type.into(),
            config,
            depends_on: depends_on.iter().map(|s| s.to_string()).collect(),
        }
    }
}
