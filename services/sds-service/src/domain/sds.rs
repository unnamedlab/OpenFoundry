use std::sync::OnceLock;

use chrono::Utc;
use regex::Regex;
use uuid::Uuid;

use crate::models::sensitive_data::{
    CreateRemediationRuleRequest, IssueStatus, MarkSensitiveIssueRequest, RemediationRule,
    RunSensitiveDataScanRequest, SensitiveDataFinding, SensitiveDataIssue, SensitiveDataIssueRow,
    SensitiveDataScanJob, SensitiveDataScanJobRow, SensitiveDataScanRequest,
    SensitiveDataScanResponse, SensitiveDataScope,
};

pub fn scan(request: &SensitiveDataScanRequest) -> SensitiveDataScanResponse {
    let detectors = detector_catalog();
    let mut findings = Vec::new();
    let mut redacted_content = request.content.clone();

    for (kind, regex) in detectors {
        let mut match_count = 0;
        let mut first_value = None;
        for capture in regex.find_iter(&request.content) {
            match_count += 1;
            if first_value.is_none() {
                first_value = Some(capture.as_str().to_string());
            }
            if request.redact {
                redacted_content = redacted_content
                    .replace(capture.as_str(), &redact_value(kind, capture.as_str()));
            }
        }

        if let Some(value) = first_value {
            findings.push(SensitiveDataFinding {
                kind: kind.to_string(),
                value: value.clone(),
                redacted: redact_value(kind, &value),
                match_count,
                severity: severity_for_kind(kind).to_string(),
            });
        }
    }

    findings.sort_by(|left, right| left.kind.cmp(&right.kind));
    let total_risk_score = risk_score(&request.scope, &findings);

    SensitiveDataScanResponse {
        findings,
        redacted_content,
        risk_score: total_risk_score,
    }
}

pub async fn create_scan_job(
    db: &sqlx::PgPool,
    request: &RunSensitiveDataScanRequest,
) -> Result<SensitiveDataScanJob, String> {
    let scan_request = SensitiveDataScanRequest {
        content: request.content.clone(),
        redact: request.redact,
        scope: request.scope.clone(),
    };
    let response = scan(&scan_request);
    let remediations = recommended_remediations(&response.findings);
    let issue_count = response.findings.len() as i32;
    let job_id = Uuid::now_v7();
    let now = Utc::now();

    let findings_json =
        serde_json::to_value(&response.findings).map_err(|cause| cause.to_string())?;
    let remediations_json =
        serde_json::to_value(&remediations).map_err(|cause| cause.to_string())?;

    sqlx::query(
        "INSERT INTO sds_scan_jobs (id, target_name, scope, status, risk_score, findings, issue_count, redacted_content, remediations, requested_by, created_at, updated_at)
         VALUES ($1, $2, $3, 'completed', $4, $5::jsonb, $6, $7, $8::jsonb, $9, $10, $11)",
    )
    .bind(job_id)
    .bind(&request.target_name)
    .bind(request.scope.as_str())
    .bind(response.risk_score as i32)
    .bind(findings_json)
    .bind(issue_count)
    .bind(&response.redacted_content)
    .bind(remediations_json)
    .bind(request.requested_by)
    .bind(now)
    .bind(now)
    .execute(db)
    .await
    .map_err(|cause| format!("failed to insert scan job: {cause}"))?;

    for finding in &response.findings {
        let issue_id = Uuid::now_v7();
        let markings = default_markings(finding);
        let issue_remediations = finding_remediations(finding);
        sqlx::query(
            "INSERT INTO sds_issues (id, job_id, kind, severity, status, matched_value, redacted_value, match_count, markings, remediation_actions, created_at, updated_at)
             VALUES ($1, $2, $3, $4, 'open', $5, $6, $7, $8::jsonb, $9::jsonb, $10, $11)",
        )
        .bind(issue_id)
        .bind(job_id)
        .bind(&finding.kind)
        .bind(&finding.severity)
        .bind(&finding.value)
        .bind(&finding.redacted)
        .bind(finding.match_count as i32)
        .bind(serde_json::to_value(&markings).map_err(|cause| cause.to_string())?)
        .bind(serde_json::to_value(&issue_remediations).map_err(|cause| cause.to_string())?)
        .bind(now)
        .bind(now)
        .execute(db)
        .await
        .map_err(|cause| format!("failed to insert issue: {cause}"))?;
    }

    Ok(SensitiveDataScanJob {
        id: job_id,
        target_name: request.target_name.clone(),
        scope: request.scope.clone(),
        status: "completed".to_string(),
        risk_score: response.risk_score,
        findings: response.findings,
        issue_count,
        redacted_content: response.redacted_content,
        remediations,
        requested_by: request.requested_by,
        created_at: now,
        updated_at: now,
    })
}

pub fn issue_from_row(row: SensitiveDataIssueRow) -> Result<SensitiveDataIssue, String> {
    Ok(SensitiveDataIssue {
        id: row.id,
        job_id: row.job_id,
        kind: row.kind,
        severity: row.severity,
        status: match row.status.as_str() {
            "resolved" => IssueStatus::Resolved,
            "suppressed" => IssueStatus::Suppressed,
            _ => IssueStatus::Open,
        },
        matched_value: row.matched_value,
        redacted_value: row.redacted_value,
        match_count: row.match_count as usize,
        markings: serde_json::from_value(row.markings).map_err(|cause| cause.to_string())?,
        remediation_actions: serde_json::from_value(row.remediation_actions)
            .map_err(|cause| cause.to_string())?,
        created_at: row.created_at,
        updated_at: row.updated_at,
    })
}

pub fn job_from_row(row: SensitiveDataScanJobRow) -> Result<SensitiveDataScanJob, String> {
    Ok(SensitiveDataScanJob {
        id: row.id,
        target_name: row.target_name,
        scope: match row.scope.as_str() {
            "dataset" => SensitiveDataScope::Dataset,
            "file" => SensitiveDataScope::File,
            "prompt" => SensitiveDataScope::Prompt,
            "message" => SensitiveDataScope::Message,
            _ => SensitiveDataScope::Record,
        },
        status: row.status,
        risk_score: row.risk_score as u32,
        findings: serde_json::from_value(row.findings).map_err(|cause| cause.to_string())?,
        issue_count: row.issue_count,
        redacted_content: row.redacted_content,
        remediations: serde_json::from_value(row.remediations)
            .map_err(|cause| cause.to_string())?,
        requested_by: row.requested_by,
        created_at: row.created_at,
        updated_at: row.updated_at,
    })
}

pub fn apply_markings(
    issue: &SensitiveDataIssue,
    request: &MarkSensitiveIssueRequest,
) -> Result<(serde_json::Value, serde_json::Value, &'static str), String> {
    let mut markings = issue.markings.clone();
    for marking in &request.markings {
        if !markings.iter().any(|existing| existing == marking) {
            markings.push(marking.clone());
        }
    }

    let mut remediations = issue.remediation_actions.clone();
    for action in &request.remediation_actions {
        if !remediations.iter().any(|existing| existing == action) {
            remediations.push(action.clone());
        }
    }

    let status = if request.resolve {
        "resolved"
    } else {
        issue.status.as_str()
    };

    Ok((
        serde_json::to_value(markings).map_err(|cause| cause.to_string())?,
        serde_json::to_value(remediations).map_err(|cause| cause.to_string())?,
        status,
    ))
}

pub fn rule_payload(
    request: &CreateRemediationRuleRequest,
) -> Result<(serde_json::Value, serde_json::Value), String> {
    Ok((
        serde_json::to_value(&request.match_conditions).map_err(|cause| cause.to_string())?,
        serde_json::to_value(&request.remediation_actions).map_err(|cause| cause.to_string())?,
    ))
}

pub fn rule_from_row(
    row: (
        Uuid,
        String,
        String,
        serde_json::Value,
        serde_json::Value,
        Option<Uuid>,
        chrono::DateTime<chrono::Utc>,
        chrono::DateTime<chrono::Utc>,
    ),
) -> Result<RemediationRule, String> {
    Ok(RemediationRule {
        id: row.0,
        name: row.1,
        scope: row.2,
        match_conditions: serde_json::from_value(row.3).map_err(|cause| cause.to_string())?,
        remediation_actions: serde_json::from_value(row.4).map_err(|cause| cause.to_string())?,
        updated_by: row.5,
        created_at: row.6,
        updated_at: row.7,
    })
}

fn detector_catalog() -> &'static [(&'static str, Regex)] {
    static DETECTORS: OnceLock<Vec<(&'static str, Regex)>> = OnceLock::new();
    DETECTORS.get_or_init(|| {
        vec![
            (
                "email",
                Regex::new(r"(?i)\b[A-Z0-9._%+-]+@[A-Z0-9.-]+\.[A-Z]{2,}\b")
                    .expect("email regex should compile"),
            ),
            (
                "ssn",
                Regex::new(r"\b\d{3}-\d{2}-\d{4}\b").expect("ssn regex should compile"),
            ),
            (
                "credit_card",
                Regex::new(r"\b(?:\d[ -]*?){13,16}\b").expect("card regex should compile"),
            ),
            (
                "api_key",
                Regex::new(r"\bofk_[A-Za-z0-9_-]{8,}\b").expect("api key regex should compile"),
            ),
            (
                "bearer_token",
                Regex::new(r"Bearer\s+[A-Za-z0-9._=-]{16,}").expect("bearer regex should compile"),
            ),
        ]
    })
}

fn redact_value(kind: &str, value: &str) -> String {
    match kind {
        "email" => {
            let parts = value.split('@').collect::<Vec<_>>();
            if parts.len() == 2 {
                format!(
                    "{}***@{}",
                    &parts[0].chars().take(2).collect::<String>(),
                    parts[1]
                )
            } else {
                "[redacted-email]".to_string()
            }
        }
        "ssn" => "***-**-****".to_string(),
        "credit_card" => format!(
            "**** **** **** {}",
            value
                .chars()
                .rev()
                .take(4)
                .collect::<String>()
                .chars()
                .rev()
                .collect::<String>()
        ),
        "api_key" => "ofk_[redacted]".to_string(),
        "bearer_token" => "Bearer [redacted]".to_string(),
        _ => "[redacted]".to_string(),
    }
}

fn severity_for_kind(kind: &str) -> &'static str {
    match kind {
        "ssn" | "credit_card" => "critical",
        "api_key" | "bearer_token" => "high",
        "email" => "medium",
        _ => "low",
    }
}

fn score_for_kind(kind: &str) -> u32 {
    match kind {
        "ssn" => 40,
        "credit_card" => 40,
        "api_key" => 35,
        "bearer_token" => 35,
        "email" => 10,
        _ => 5,
    }
}

fn risk_score(scope: &SensitiveDataScope, findings: &[SensitiveDataFinding]) -> u32 {
    let base: u32 = findings
        .iter()
        .map(|finding| score_for_kind(&finding.kind))
        .sum();
    let multiplier = match scope {
        SensitiveDataScope::Prompt | SensitiveDataScope::Message => 1,
        SensitiveDataScope::Dataset | SensitiveDataScope::Record => 2,
        SensitiveDataScope::File => 2,
    };
    base * multiplier
}

fn default_markings(finding: &SensitiveDataFinding) -> Vec<String> {
    let mut markings = vec!["sensitive".to_string(), finding.kind.clone()];
    if finding.severity == "critical" {
        markings.push("restricted".to_string());
    }
    markings
}

fn finding_remediations(finding: &SensitiveDataFinding) -> Vec<String> {
    match finding.kind.as_str() {
        "email" => vec!["mask_pii".to_string()],
        "ssn" | "credit_card" => vec!["quarantine_record".to_string(), "mask_pii".to_string()],
        "api_key" | "bearer_token" => {
            vec!["revoke_credential".to_string(), "rotate_secret".to_string()]
        }
        _ => vec!["manual_review".to_string()],
    }
}

fn recommended_remediations(findings: &[SensitiveDataFinding]) -> Vec<String> {
    let mut actions = Vec::new();
    for finding in findings {
        for action in finding_remediations(finding) {
            if !actions.iter().any(|existing| existing == &action) {
                actions.push(action);
            }
        }
    }
    actions
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn scan_detects_and_redacts_sensitive_content() {
        let response = scan(&SensitiveDataScanRequest {
            content: "Contact jane@example.com with SSN 123-45-6789 and token ofk_abcdefghi"
                .to_string(),
            redact: true,
            scope: SensitiveDataScope::Record,
        });

        assert!(
            response
                .findings
                .iter()
                .any(|finding| finding.kind == "email")
        );
        assert!(
            response
                .findings
                .iter()
                .any(|finding| finding.kind == "ssn")
        );
        assert!(response.redacted_content.contains("***-**-****"));
        assert!(response.redacted_content.contains("ofk_[redacted]"));
        assert!(response.risk_score >= 50);
    }
}
