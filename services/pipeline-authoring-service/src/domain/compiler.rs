use std::{
    collections::{HashMap, HashSet},
    str::FromStr,
};

use cron::Schedule;

use crate::{
    domain::executor,
    models::{
        authoring::{
            CompilePipelineRequest, CompilePipelineResponse, ExecutablePlan, PipelineGraphSummary,
            PipelineValidationResponse, PrunePipelineResponse, ValidatePipelineRequest,
        },
        pipeline::{PipelineNode, PipelineScheduleConfig},
    },
};

pub fn validate_request(request: &ValidatePipelineRequest) -> PipelineValidationResponse {
    validate_definition(&request.status, &request.schedule_config, &request.nodes)
}

pub fn validate_definition(
    status: &str,
    schedule_config: &PipelineScheduleConfig,
    nodes: &[PipelineNode],
) -> PipelineValidationResponse {
    let mut errors = Vec::new();
    let mut warnings = Vec::new();

    if nodes.is_empty() {
        errors.push("pipeline must define at least one node".to_string());
    }

    let mut seen_ids = HashSet::new();
    let mut duplicate_ids = HashSet::new();
    let all_node_ids = nodes
        .iter()
        .map(|node| node.id.clone())
        .collect::<HashSet<_>>();
    let mut edge_count = 0usize;

    for node in nodes {
        if node.id.trim().is_empty() {
            errors.push("pipeline nodes must have a non-empty id".to_string());
            continue;
        }
        if !seen_ids.insert(node.id.clone()) {
            duplicate_ids.insert(node.id.clone());
        }

        edge_count += node.depends_on.len();
        for dependency in &node.depends_on {
            if dependency == &node.id {
                errors.push(format!(
                    "pipeline node '{}' cannot depend on itself",
                    node.id
                ));
            }
            if !all_node_ids.contains(dependency) {
                errors.push(format!(
                    "pipeline node '{}' depends on missing node '{}'",
                    node.id, dependency
                ));
            }
        }

        // Per-kind validation for media-typed nodes (P1.4).
        if crate::domain::media_nodes::is_media_transform_type(&node.transform_type) {
            for issue in crate::domain::media_nodes::validate_media_node(
                &node.transform_type,
                &node.config,
            ) {
                errors.push(format!("pipeline node '{}': {issue}", node.id));
            }
        }
    }

    if !duplicate_ids.is_empty() {
        let mut duplicates = duplicate_ids.into_iter().collect::<Vec<_>>();
        duplicates.sort();
        errors.push(format!(
            "pipeline contains duplicate node ids: {}",
            duplicates.join(", ")
        ));
    }

    if schedule_config.enabled {
        match schedule_config.cron.as_deref() {
            Some(expression) if !expression.trim().is_empty() => {
                if let Err(error) = Schedule::from_str(expression) {
                    errors.push(format!("pipeline schedule cron is invalid: {error}"));
                }
            }
            _ => errors.push("pipeline schedule is enabled but cron is missing".to_string()),
        }

        if status != "active" {
            warnings.push(
                "pipeline schedule is enabled but the pipeline status is not 'active'".to_string(),
            );
        }
    }

    if let Err(error) = topological_sort(nodes) {
        errors.push(error);
    }

    let root_nodes = nodes
        .iter()
        .filter(|node| node.depends_on.is_empty())
        .map(|node| node.id.clone())
        .collect::<Vec<_>>();
    let depended_on = nodes
        .iter()
        .flat_map(|node| node.depends_on.iter().cloned())
        .collect::<HashSet<_>>();
    let leaf_nodes = nodes
        .iter()
        .filter(|node| !depended_on.contains(&node.id))
        .map(|node| node.id.clone())
        .collect::<Vec<_>>();

    PipelineValidationResponse {
        valid: errors.is_empty(),
        errors,
        warnings,
        next_run_at: executor::compute_next_run_at_from_parts(status, schedule_config),
        summary: PipelineGraphSummary {
            node_count: nodes.len(),
            edge_count,
            root_nodes,
            leaf_nodes,
        },
    }
}

pub fn compile_request(
    request: &CompilePipelineRequest,
) -> Result<CompilePipelineResponse, PipelineValidationResponse> {
    let validation = validate_request(&request.pipeline);
    if !validation.valid {
        return Err(validation);
    }

    let reachable =
        execution_reachable_nodes(&request.pipeline.nodes, request.start_from_node.as_deref())?;
    let mut reachable_node_ids = reachable.into_iter().collect::<Vec<_>>();
    reachable_node_ids.sort();

    let all_node_ids = request
        .pipeline
        .nodes
        .iter()
        .map(|node| node.id.clone())
        .collect::<Vec<_>>();
    let mut pruned_node_ids = all_node_ids
        .into_iter()
        .filter(|node_id| {
            !reachable_node_ids
                .iter()
                .any(|reachable_id| reachable_id == node_id)
        })
        .collect::<Vec<_>>();
    pruned_node_ids.sort();

    let node_order = execution_order(&request.pipeline.nodes, request.start_from_node.as_deref())?;
    let execution_stages =
        execution_stages(&request.pipeline.nodes, request.start_from_node.as_deref())?;

    Ok(CompilePipelineResponse {
        validation,
        plan: ExecutablePlan {
            start_from_node: request.start_from_node.clone(),
            node_order,
            execution_stages,
            reachable_node_ids,
            pruned_node_ids,
            distributed_worker_count: request.distributed_worker_count.max(1),
            retry_policy: request.pipeline.retry_policy.clone(),
            mode: if request.distributed_worker_count > 1 {
                "staged_parallel".to_string()
            } else {
                "sequential".to_string()
            },
        },
    })
}

pub fn prune_request(
    request: &CompilePipelineRequest,
) -> Result<PrunePipelineResponse, PipelineValidationResponse> {
    let compiled = compile_request(request)?;
    Ok(PrunePipelineResponse {
        validation: compiled.validation,
        start_from_node: compiled.plan.start_from_node,
        reachable_node_ids: compiled.plan.reachable_node_ids,
        pruned_node_ids: compiled.plan.pruned_node_ids,
    })
}

fn execution_order(
    nodes: &[PipelineNode],
    start_from_node: Option<&str>,
) -> Result<Vec<String>, PipelineValidationResponse> {
    let order = topological_sort(nodes).map_err(validation_error)?;
    let Some(start_from_node) = start_from_node else {
        return Ok(order);
    };

    let reachable = reachable_nodes(nodes, start_from_node).map_err(validation_error)?;
    Ok(order
        .into_iter()
        .filter(|node_id| reachable.contains(node_id))
        .collect())
}

fn execution_stages(
    nodes: &[PipelineNode],
    start_from_node: Option<&str>,
) -> Result<Vec<Vec<String>>, PipelineValidationResponse> {
    let reachable = execution_reachable_nodes(nodes, start_from_node)?;

    let mut in_degree: HashMap<&str, usize> = HashMap::new();
    let mut adjacency: HashMap<&str, Vec<&str>> = HashMap::new();

    for node in nodes.iter().filter(|node| reachable.contains(&node.id)) {
        in_degree.entry(node.id.as_str()).or_insert(0);
        adjacency.entry(node.id.as_str()).or_default();
        for dependency in &node.depends_on {
            if !reachable.contains(dependency) {
                continue;
            }
            adjacency
                .entry(dependency.as_str())
                .or_default()
                .push(node.id.as_str());
            *in_degree.entry(node.id.as_str()).or_insert(0) += 1;
        }
    }

    let mut frontier: Vec<&str> = in_degree
        .iter()
        .filter(|&(_, &degree)| degree == 0)
        .map(|(&node_id, _)| node_id)
        .collect();
    frontier.sort_unstable();

    let mut stages = Vec::new();
    let mut visited = 0usize;
    while !frontier.is_empty() {
        let mut next_frontier = Vec::new();
        let mut stage = Vec::new();
        for node_id in frontier.drain(..) {
            visited += 1;
            stage.push(node_id.to_string());
            if let Some(neighbors) = adjacency.get(node_id) {
                for &neighbor in neighbors {
                    if let Some(degree) = in_degree.get_mut(neighbor) {
                        *degree -= 1;
                        if *degree == 0 {
                            next_frontier.push(neighbor);
                        }
                    }
                }
            }
        }
        next_frontier.sort_unstable();
        stages.push(stage);
        frontier = next_frontier;
    }

    if visited != reachable.len() {
        return Err(validation_error(
            "cycle detected in pipeline DAG".to_string(),
        ));
    }

    Ok(stages)
}

fn execution_reachable_nodes(
    nodes: &[PipelineNode],
    start_from_node: Option<&str>,
) -> Result<HashSet<String>, PipelineValidationResponse> {
    match start_from_node {
        Some(start_from_node) => reachable_nodes(nodes, start_from_node).map_err(validation_error),
        None => Ok(nodes.iter().map(|node| node.id.clone()).collect()),
    }
}

fn reachable_nodes(
    nodes: &[PipelineNode],
    start_from_node: &str,
) -> Result<HashSet<String>, String> {
    if !nodes.iter().any(|node| node.id == start_from_node) {
        return Err(format!("start node '{start_from_node}' not found"));
    }

    let mut adjacency: HashMap<&str, Vec<&str>> = HashMap::new();
    for node in nodes {
        adjacency.entry(node.id.as_str()).or_default();
        for dependency in &node.depends_on {
            adjacency
                .entry(dependency.as_str())
                .or_default()
                .push(node.id.as_str());
        }
    }

    let mut reachable = HashSet::new();
    let mut stack = vec![start_from_node.to_string()];
    while let Some(node_id) = stack.pop() {
        if !reachable.insert(node_id.clone()) {
            continue;
        }
        if let Some(neighbors) = adjacency.get(node_id.as_str()) {
            for neighbor in neighbors {
                stack.push((*neighbor).to_string());
            }
        }
    }

    Ok(reachable)
}

fn topological_sort(nodes: &[PipelineNode]) -> Result<Vec<String>, String> {
    let mut in_degree: HashMap<&str, usize> = HashMap::new();
    let mut adjacency: HashMap<&str, Vec<&str>> = HashMap::new();

    for node in nodes {
        in_degree.entry(node.id.as_str()).or_insert(0);
        adjacency.entry(node.id.as_str()).or_default();
        for dependency in &node.depends_on {
            adjacency
                .entry(dependency.as_str())
                .or_default()
                .push(node.id.as_str());
            *in_degree.entry(node.id.as_str()).or_insert(0) += 1;
        }
    }

    let mut queue: Vec<&str> = in_degree
        .iter()
        .filter(|&(_, &degree)| degree == 0)
        .map(|(&node_id, _)| node_id)
        .collect();
    let mut order = Vec::new();

    while let Some(node_id) = queue.pop() {
        order.push(node_id.to_string());
        if let Some(neighbors) = adjacency.get(node_id) {
            for &neighbor in neighbors {
                if let Some(degree) = in_degree.get_mut(neighbor) {
                    *degree -= 1;
                    if *degree == 0 {
                        queue.push(neighbor);
                    }
                }
            }
        }
    }

    if order.len() != nodes.len() {
        return Err("cycle detected in pipeline DAG".to_string());
    }

    Ok(order)
}

fn validation_error(error: String) -> PipelineValidationResponse {
    PipelineValidationResponse {
        valid: false,
        errors: vec![error],
        warnings: Vec::new(),
        next_run_at: None,
        summary: PipelineGraphSummary {
            node_count: 0,
            edge_count: 0,
            root_nodes: Vec::new(),
            leaf_nodes: Vec::new(),
        },
    }
}
