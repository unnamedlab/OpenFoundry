use auth_middleware::jwt::JwtConfig;
use base64::{Engine as _, engine::general_purpose::STANDARD};
use bergshamra::{
    DsigContext, KeysManager, VerifyResult, keys::loader::load_x509_cert_pem, verify,
};
use chrono::{DateTime, Duration, Utc};
use roxmltree::{Document, Node};
use serde_json::{Map, Value};
use std::collections::HashSet;
use url::Url;
use uuid::Uuid;

use crate::{domain::oauth, models::sso::SsoProvider};

const NS_SAML_ASSERTION: &str = "urn:oasis:names:tc:SAML:2.0:assertion";
const NS_SAML_PROTOCOL: &str = "urn:oasis:names:tc:SAML:2.0:protocol";
const SAML_STATUS_SUCCESS: &str = "urn:oasis:names:tc:SAML:2.0:status:Success";
const SAML_SUBJECT_CONFIRMATION_BEARER: &str = "urn:oasis:names:tc:SAML:2.0:cm:bearer";

#[derive(Debug, Clone)]
pub struct SamlMetadataDefaults {
    pub entity_id: Option<String>,
    pub sso_url: Option<String>,
    pub certificate: Option<String>,
}

#[derive(Debug, Clone)]
pub struct SamlIdentity {
    pub subject: String,
    pub email: String,
    pub name: String,
    pub raw_claims: Value,
}

#[derive(Debug, Clone)]
pub struct SamlServiceProviderConfig {
    pub entity_id: String,
    pub assertion_consumer_service_url: String,
    pub allowed_clock_skew_secs: i64,
}

#[derive(Debug, Clone)]
pub struct SamlValidationContext {
    pub service_provider: SamlServiceProviderConfig,
    pub request_id: Option<String>,
}

pub async fn resolve_metadata_defaults(metadata_url: &str) -> Result<SamlMetadataDefaults, String> {
    let response = reqwest::get(metadata_url)
        .await
        .map_err(|error| error.to_string())?;
    if !response.status().is_success() {
        return Err(format!(
            "metadata fetch failed with status {}",
            response.status()
        ));
    }
    let xml = response.text().await.map_err(|error| error.to_string())?;
    parse_metadata_defaults(&xml)
}

pub fn parse_metadata_defaults(xml: &str) -> Result<SamlMetadataDefaults, String> {
    let document = Document::parse(xml).map_err(|error| error.to_string())?;
    let entity_descriptor = document
        .descendants()
        .find(|node| node.is_element() && node.tag_name().name() == "EntityDescriptor");

    let entity_id = entity_descriptor
        .and_then(|node| node.attribute("entityID"))
        .and_then(trimmed);
    let sso_url = document
        .descendants()
        .find(|node| node.is_element() && node.tag_name().name() == "SingleSignOnService")
        .and_then(|node| node.attribute("Location"))
        .and_then(trimmed);
    let certificate = document
        .descendants()
        .find(|node| node.is_element() && node.tag_name().name() == "X509Certificate")
        .and_then(node_text)
        .map(|value| strip_all_whitespace(&value));

    Ok(SamlMetadataDefaults {
        entity_id,
        sso_url,
        certificate,
    })
}

pub fn build_authorization_url(
    config: &JwtConfig,
    provider: &SsoProvider,
    service_provider: &SamlServiceProviderConfig,
    redirect_to: Option<&str>,
) -> Result<String, String> {
    let destination = provider
        .saml_sso_url
        .as_deref()
        .ok_or_else(|| "provider is missing saml_sso_url".to_string())?;
    let request_id = format!("_{}", Uuid::now_v7().simple());
    let issue_instant = Utc::now().format("%Y-%m-%dT%H:%M:%SZ").to_string();

    let mut state_attributes = Map::new();
    state_attributes.insert(
        "saml_request_id".to_string(),
        Value::String(request_id.clone()),
    );
    let relay_state =
        oauth::issue_state_with_attributes(config, provider.id, redirect_to, state_attributes)?;

    let xml = format!(
        r#"<samlp:AuthnRequest xmlns:samlp="urn:oasis:names:tc:SAML:2.0:protocol" xmlns:saml="urn:oasis:names:tc:SAML:2.0:assertion" ID="{request_id}" Version="2.0" IssueInstant="{issue_instant}" ProtocolBinding="urn:oasis:names:tc:SAML:2.0:bindings:HTTP-POST" AssertionConsumerServiceURL="{acs_url}" Destination="{destination}"><saml:Issuer>{issuer}</saml:Issuer></samlp:AuthnRequest>"#,
        acs_url = xml_escape(&service_provider.assertion_consumer_service_url),
        destination = xml_escape(destination),
        issuer = xml_escape(&service_provider.entity_id),
    );

    let mut url = Url::parse(destination).map_err(|error| error.to_string())?;
    url.query_pairs_mut()
        .append_pair("SAMLRequest", &STANDARD.encode(xml.as_bytes()))
        .append_pair("RelayState", &relay_state);
    Ok(url.to_string())
}

pub fn parse_saml_response(
    provider: &SsoProvider,
    saml_response: &str,
    validation: &SamlValidationContext,
) -> Result<SamlIdentity, String> {
    parse_saml_response_at(provider, saml_response, validation, Utc::now())
}

fn parse_saml_response_at(
    provider: &SsoProvider,
    saml_response: &str,
    validation: &SamlValidationContext,
    now: DateTime<Utc>,
) -> Result<SamlIdentity, String> {
    let decoded = STANDARD
        .decode(saml_response)
        .map_err(|error| error.to_string())?;
    let xml = String::from_utf8(decoded).map_err(|error| error.to_string())?;
    let signed_reference_ids = verify_saml_signature(provider, &xml)?;

    let document = Document::parse(&xml).map_err(|error| error.to_string())?;
    let response = document.root_element();
    if response.tag_name().name() != "Response"
        || response.tag_name().namespace() != Some(NS_SAML_PROTOCOL)
    {
        return Err("saml response root element must be samlp:Response".to_string());
    }

    validate_status_success(response)?;
    validate_optional_destination(
        response.attribute("Destination"),
        &validation.service_provider.assertion_consumer_service_url,
    )?;
    validate_optional_in_response_to(
        response.attribute("InResponseTo"),
        validation.request_id.as_deref(),
        "response",
    )?;
    validate_issue_instant(
        response.attribute("IssueInstant"),
        now,
        validation.service_provider.allowed_clock_skew_secs,
        "response",
    )?;

    let assertions: Vec<_> = response
        .descendants()
        .filter(|node| is_element_named(*node, NS_SAML_ASSERTION, "Assertion"))
        .collect();
    let assertion = match assertions.as_slice() {
        [assertion] => *assertion,
        [] => return Err("saml response is missing Assertion".to_string()),
        _ => return Err("saml response contains multiple assertions".to_string()),
    };

    let response_id = response.attribute("ID");
    let assertion_id = assertion
        .attribute("ID")
        .or(assertion.attribute("AssertionID"));
    let response_signed = response_id.is_some_and(|id| signed_reference_ids.contains(id));
    let assertion_signed = assertion_id.is_some_and(|id| signed_reference_ids.contains(id));
    if !response_signed && !assertion_signed {
        return Err(
            "saml response signature does not cover either the Response or the Assertion"
                .to_string(),
        );
    }

    validate_expected_issuer(response, assertion, provider.saml_entity_id.as_deref())?;
    validate_issue_instant(
        assertion.attribute("IssueInstant"),
        now,
        validation.service_provider.allowed_clock_skew_secs,
        "assertion",
    )?;
    validate_conditions(
        assertion,
        now,
        validation.service_provider.allowed_clock_skew_secs,
    )?;
    validate_audience(assertion, &validation.service_provider)?;
    validate_subject_confirmation(
        assertion,
        validation.request_id.as_deref(),
        &validation.service_provider,
        now,
    )?;

    let subject = assertion
        .descendants()
        .find(|node| is_element_named(*node, NS_SAML_ASSERTION, "NameID"))
        .and_then(node_text)
        .ok_or_else(|| "saml assertion is missing NameID".to_string())?;

    let mut attributes = extract_attributes(assertion);
    attributes.insert("NameID".to_string(), Value::String(subject.clone()));

    let subject_key = provider
        .attribute_mapping
        .get("subject")
        .and_then(Value::as_str)
        .unwrap_or("NameID");
    let email_key = provider
        .attribute_mapping
        .get("email")
        .and_then(Value::as_str)
        .unwrap_or("email");
    let name_key = provider
        .attribute_mapping
        .get("name")
        .and_then(Value::as_str)
        .unwrap_or("name");

    let email = claim_first_string(&attributes, email_key)
        .ok_or_else(|| "saml response is missing email attribute".to_string())?;
    let mapped_subject = claim_first_string(&attributes, subject_key).unwrap_or(subject);
    let name = claim_first_string(&attributes, name_key).unwrap_or_else(|| email.clone());

    Ok(SamlIdentity {
        subject: mapped_subject,
        email,
        name,
        raw_claims: Value::Object(attributes),
    })
}

fn verify_saml_signature(provider: &SsoProvider, xml: &str) -> Result<HashSet<String>, String> {
    let certificate = provider
        .saml_certificate
        .as_deref()
        .ok_or_else(|| "provider is missing saml_certificate".to_string())?;
    let pem = normalize_certificate_pem(certificate)?;
    let key = load_x509_cert_pem(pem.as_bytes()).map_err(|error| error.to_string())?;

    let mut keys = KeysManager::new();
    keys.add_key(key);

    let mut context = DsigContext::new(keys);
    context.trusted_keys_only = true;
    context.strict_verification = true;

    match verify(&context, xml).map_err(|error| error.to_string())? {
        VerifyResult::Valid { references, .. } => {
            let ids = references
                .into_iter()
                .filter_map(|reference| reference.uri.strip_prefix('#').map(ToString::to_string))
                .collect::<HashSet<_>>();
            if ids.is_empty() {
                Err("saml signature did not produce any same-document references".to_string())
            } else {
                Ok(ids)
            }
        }
        VerifyResult::Invalid { reason } => {
            Err(format!("saml signature verification failed: {reason}"))
        }
    }
}

fn validate_status_success(response: Node<'_, '_>) -> Result<(), String> {
    let status_code = response
        .children()
        .find(|node| is_element_named(*node, NS_SAML_PROTOCOL, "Status"))
        .and_then(|status| {
            status
                .children()
                .find(|node| is_element_named(*node, NS_SAML_PROTOCOL, "StatusCode"))
        })
        .and_then(|node| node.attribute("Value"))
        .and_then(trimmed)
        .ok_or_else(|| "saml response is missing StatusCode".to_string())?;

    if status_code == SAML_STATUS_SUCCESS {
        Ok(())
    } else {
        Err(format!(
            "saml response returned a non-success status: {status_code}"
        ))
    }
}

fn validate_expected_issuer(
    response: Node<'_, '_>,
    assertion: Node<'_, '_>,
    expected_issuer: Option<&str>,
) -> Result<(), String> {
    let Some(expected_issuer) = expected_issuer.and_then(trimmed) else {
        return Ok(());
    };
    let response_issuer = response
        .children()
        .find(|node| is_element_named(*node, NS_SAML_ASSERTION, "Issuer"))
        .and_then(node_text);
    let assertion_issuer = assertion
        .children()
        .find(|node| is_element_named(*node, NS_SAML_ASSERTION, "Issuer"))
        .and_then(node_text);

    if let Some(value) = response_issuer.as_deref() {
        if value != expected_issuer {
            return Err(format!(
                "saml response issuer mismatch: expected {expected_issuer}, got {value}"
            ));
        }
    }
    if let Some(value) = assertion_issuer.as_deref() {
        if value != expected_issuer {
            return Err(format!(
                "saml assertion issuer mismatch: expected {expected_issuer}, got {value}"
            ));
        }
    }
    if response_issuer.is_none() && assertion_issuer.is_none() {
        return Err("saml response is missing issuer".to_string());
    }

    Ok(())
}

fn validate_conditions(
    assertion: Node<'_, '_>,
    now: DateTime<Utc>,
    allowed_clock_skew_secs: i64,
) -> Result<(), String> {
    let Some(conditions) = assertion
        .children()
        .find(|node| is_element_named(*node, NS_SAML_ASSERTION, "Conditions"))
    else {
        return Err("saml assertion is missing Conditions".to_string());
    };

    let skew = clock_skew(allowed_clock_skew_secs);
    if let Some(not_before) = conditions.attribute("NotBefore") {
        let not_before = parse_saml_time(not_before, "conditions NotBefore")?;
        if now < not_before - skew {
            return Err("saml assertion is not yet valid".to_string());
        }
    }
    if let Some(not_on_or_after) = conditions.attribute("NotOnOrAfter") {
        let not_on_or_after = parse_saml_time(not_on_or_after, "conditions NotOnOrAfter")?;
        if now >= not_on_or_after + skew {
            return Err("saml assertion has expired".to_string());
        }
    }

    Ok(())
}

fn validate_audience(
    assertion: Node<'_, '_>,
    service_provider: &SamlServiceProviderConfig,
) -> Result<(), String> {
    let audiences = assertion
        .descendants()
        .filter(|node| is_element_named(*node, NS_SAML_ASSERTION, "Audience"))
        .filter_map(node_text)
        .collect::<Vec<_>>();
    if audiences.is_empty() {
        return Err("saml assertion is missing audience restriction".to_string());
    }

    let audience_allowed = audiences.iter().any(|audience| {
        audience == &service_provider.entity_id
            || audience == &service_provider.assertion_consumer_service_url
    });
    if audience_allowed {
        Ok(())
    } else {
        Err(format!(
            "saml assertion audience does not match service provider {}",
            service_provider.entity_id
        ))
    }
}

fn validate_subject_confirmation(
    assertion: Node<'_, '_>,
    request_id: Option<&str>,
    service_provider: &SamlServiceProviderConfig,
    now: DateTime<Utc>,
) -> Result<(), String> {
    let confirmation_data = assertion
        .descendants()
        .find(|node| {
            is_element_named(*node, NS_SAML_ASSERTION, "SubjectConfirmation")
                && node.attribute("Method") == Some(SAML_SUBJECT_CONFIRMATION_BEARER)
        })
        .and_then(|confirmation| {
            confirmation
                .children()
                .find(|node| is_element_named(*node, NS_SAML_ASSERTION, "SubjectConfirmationData"))
        })
        .ok_or_else(|| "saml assertion is missing bearer SubjectConfirmationData".to_string())?;

    validate_required_attribute_match(
        confirmation_data.attribute("Recipient"),
        &service_provider.assertion_consumer_service_url,
        "subject confirmation recipient",
    )?;
    validate_optional_in_response_to(
        confirmation_data.attribute("InResponseTo"),
        request_id,
        "subject confirmation",
    )?;

    let not_on_or_after = confirmation_data
        .attribute("NotOnOrAfter")
        .ok_or_else(|| "saml subject confirmation is missing NotOnOrAfter".to_string())?;
    let expires_at = parse_saml_time(not_on_or_after, "subject confirmation NotOnOrAfter")?;
    if now >= expires_at + clock_skew(service_provider.allowed_clock_skew_secs) {
        return Err("saml subject confirmation has expired".to_string());
    }

    Ok(())
}

fn validate_issue_instant(
    issue_instant: Option<&str>,
    now: DateTime<Utc>,
    allowed_clock_skew_secs: i64,
    label: &str,
) -> Result<(), String> {
    let Some(issue_instant) = issue_instant else {
        return Err(format!("saml {label} is missing IssueInstant"));
    };
    let issue_instant = parse_saml_time(issue_instant, &format!("{label} IssueInstant"))?;
    if issue_instant > now + clock_skew(allowed_clock_skew_secs) {
        return Err(format!("saml {label} IssueInstant is in the future"));
    }
    Ok(())
}

fn validate_optional_destination(
    destination: Option<&str>,
    expected_destination: &str,
) -> Result<(), String> {
    if let Some(destination) = destination.and_then(trimmed) {
        if destination != expected_destination {
            return Err(format!(
                "saml response destination mismatch: expected {expected_destination}, got {destination}"
            ));
        }
    }
    Ok(())
}

fn validate_optional_in_response_to(
    actual: Option<&str>,
    expected: Option<&str>,
    label: &str,
) -> Result<(), String> {
    let Some(expected) = expected.and_then(trimmed) else {
        return Ok(());
    };
    let actual = actual
        .and_then(trimmed)
        .ok_or_else(|| format!("saml {label} is missing InResponseTo"))?;
    if actual == expected {
        Ok(())
    } else {
        Err(format!(
            "saml {label} InResponseTo mismatch: expected {expected}, got {actual}"
        ))
    }
}

fn validate_required_attribute_match(
    actual: Option<&str>,
    expected: &str,
    label: &str,
) -> Result<(), String> {
    let actual = actual
        .and_then(trimmed)
        .ok_or_else(|| format!("saml {label} is missing"))?;
    if actual == expected {
        Ok(())
    } else {
        Err(format!(
            "saml {label} mismatch: expected {expected}, got {actual}"
        ))
    }
}

fn extract_attributes(assertion: Node<'_, '_>) -> Map<String, Value> {
    let mut attributes = Map::new();

    for attribute in assertion
        .descendants()
        .filter(|node| is_element_named(*node, NS_SAML_ASSERTION, "Attribute"))
    {
        let Some(name) = attribute.attribute("Name").and_then(trimmed) else {
            continue;
        };
        let values = attribute
            .children()
            .filter(|node| is_element_named(*node, NS_SAML_ASSERTION, "AttributeValue"))
            .filter_map(node_text)
            .map(Value::String)
            .collect::<Vec<_>>();

        if values.is_empty() {
            continue;
        }
        if values.len() == 1 {
            if let Some(value) = values.into_iter().next() {
                attributes.insert(name.to_string(), value);
            }
        } else {
            attributes.insert(name.to_string(), Value::Array(values));
        }
    }

    attributes
}

fn claim_first_string(attributes: &Map<String, Value>, key: &str) -> Option<String> {
    match attributes.get(key) {
        Some(Value::String(value)) if !value.is_empty() => Some(value.clone()),
        Some(Value::Array(values)) => values.iter().find_map(|value| match value {
            Value::String(value) if !value.is_empty() => Some(value.clone()),
            _ => None,
        }),
        _ => None,
    }
}

fn normalize_certificate_pem(raw: &str) -> Result<String, String> {
    let value = raw.trim();
    if value.is_empty() {
        return Err("provider is missing saml_certificate".to_string());
    }
    if value.contains("-----BEGIN CERTIFICATE-----") {
        return Ok(format!("{}\n", value.trim()));
    }

    let base64 = strip_all_whitespace(value);
    if base64.is_empty() {
        return Err("provider is missing saml_certificate".to_string());
    }

    let mut pem = String::from("-----BEGIN CERTIFICATE-----\n");
    for chunk in base64.as_bytes().chunks(64) {
        let chunk = std::str::from_utf8(chunk).map_err(|error| error.to_string())?;
        pem.push_str(chunk);
        pem.push('\n');
    }
    pem.push_str("-----END CERTIFICATE-----\n");
    Ok(pem)
}

fn parse_saml_time(value: &str, label: &str) -> Result<DateTime<Utc>, String> {
    DateTime::parse_from_rfc3339(value)
        .map(|time| time.with_timezone(&Utc))
        .map_err(|error| format!("invalid {label}: {error}"))
}

fn clock_skew(seconds: i64) -> Duration {
    Duration::seconds(seconds.max(0))
}

fn trimmed(value: &str) -> Option<String> {
    let trimmed = value.trim();
    if trimmed.is_empty() {
        None
    } else {
        Some(trimmed.to_string())
    }
}

fn node_text(node: Node<'_, '_>) -> Option<String> {
    let text = node
        .descendants()
        .filter(|descendant| descendant.is_text())
        .filter_map(|descendant| descendant.text())
        .collect::<String>();
    trimmed(&text)
}

fn is_element_named(node: Node<'_, '_>, namespace: &str, local_name: &str) -> bool {
    node.is_element()
        && node.tag_name().namespace() == Some(namespace)
        && node.tag_name().name() == local_name
}

fn xml_escape(value: &str) -> String {
    value
        .replace('&', "&amp;")
        .replace('<', "&lt;")
        .replace('>', "&gt;")
        .replace('"', "&quot;")
        .replace('\'', "&apos;")
}

fn strip_all_whitespace(value: &str) -> String {
    value
        .chars()
        .filter(|character| !character.is_whitespace())
        .collect()
}

#[cfg(test)]
mod tests {
    use chrono::TimeZone;
    use serde_json::json;

    use super::*;

    fn fixture_provider() -> SsoProvider {
        SsoProvider {
            id: Uuid::now_v7(),
            slug: "saml".to_string(),
            name: "SAML".to_string(),
            provider_type: "saml".to_string(),
            enabled: true,
            client_id: None,
            client_secret: None,
            issuer_url: None,
            authorization_url: None,
            token_url: None,
            userinfo_url: None,
            scopes: vec![],
            saml_metadata_url: None,
            saml_entity_id: Some("http://idp.example.com/metadata.php".to_string()),
            saml_sso_url: Some("https://idp.example.com/sso".to_string()),
            saml_certificate: Some(include_str!("../testdata/saml/signing_cert.pem").to_string()),
            attribute_mapping: json!({
                "subject": "uid",
                "email": "mail",
                "name": "displayName"
            }),
            created_at: Utc::now(),
            updated_at: Utc::now(),
        }
    }

    fn fixture_validation_context() -> SamlValidationContext {
        SamlValidationContext {
            service_provider: SamlServiceProviderConfig {
                entity_id: "http://sp.example.com/demo1/metadata.php".to_string(),
                assertion_consumer_service_url: "http://sp.example.com/demo1/index.php?acs"
                    .to_string(),
                allowed_clock_skew_secs: 120,
            },
            request_id: Some("ONELOGIN_4fee3b046395c4e751011e97f8900b5273d56685".to_string()),
        }
    }

    fn valid_fixture_now() -> DateTime<Utc> {
        Utc.with_ymd_and_hms(2024, 1, 18, 6, 20, 0)
            .single()
            .expect("valid fixture time")
    }

    #[test]
    fn metadata_parser_extracts_defaults() {
        let metadata = parse_metadata_defaults(
            r#"<EntityDescriptor xmlns="urn:oasis:names:tc:SAML:2.0:metadata" entityID="https://idp.example.com/metadata"><IDPSSODescriptor><SingleSignOnService Location="https://idp.example.com/sso"/><KeyDescriptor><KeyInfo xmlns="http://www.w3.org/2000/09/xmldsig#"><X509Data><X509Certificate>ABC123</X509Certificate></X509Data></KeyInfo></KeyDescriptor></IDPSSODescriptor></EntityDescriptor>"#,
        )
        .expect("metadata should parse");

        assert_eq!(
            metadata.entity_id.as_deref(),
            Some("https://idp.example.com/metadata")
        );
        assert_eq!(
            metadata.sso_url.as_deref(),
            Some("https://idp.example.com/sso")
        );
        assert_eq!(metadata.certificate.as_deref(), Some("ABC123"));
    }

    #[test]
    fn saml_response_parser_accepts_signed_response() {
        let provider = fixture_provider();
        let response = include_str!("../testdata/saml/response_signed.xml");
        let identity = parse_saml_response_at(
            &provider,
            &STANDARD.encode(response.as_bytes()),
            &fixture_validation_context(),
            valid_fixture_now(),
        )
        .expect("response-signed assertion should validate");

        assert_eq!(identity.subject, "test");
        assert_eq!(identity.email, "test@example.com");
        assert_eq!(identity.name, "test@example.com");
        assert_eq!(
            identity.raw_claims.get("eduPersonAffiliation"),
            Some(&Value::Array(vec![
                Value::String("users".to_string()),
                Value::String("examplerole1".to_string())
            ]))
        );
    }

    #[test]
    fn saml_response_parser_accepts_signed_assertion() {
        let provider = fixture_provider();
        let response = include_str!("../testdata/saml/response_signed_assertion.xml");
        let identity = parse_saml_response_at(
            &provider,
            &STANDARD.encode(response.as_bytes()),
            &fixture_validation_context(),
            valid_fixture_now(),
        )
        .expect("assertion-signed response should validate");

        assert_eq!(identity.subject, "test");
        assert_eq!(identity.email, "test@example.com");
    }

    #[test]
    fn saml_response_parser_rejects_tampering() {
        let provider = fixture_provider();
        let tampered = include_str!("../testdata/saml/response_signed.xml")
            .replace("test@example.com", "attacker@example.com");

        let error = parse_saml_response_at(
            &provider,
            &STANDARD.encode(tampered.as_bytes()),
            &fixture_validation_context(),
            valid_fixture_now(),
        )
        .expect_err("tampered response must fail signature validation");

        assert!(error.contains("signature verification failed"));
    }

    #[test]
    fn saml_response_parser_rejects_wrong_audience() {
        let provider = fixture_provider();
        let response = include_str!("../testdata/saml/response_signed.xml");
        let mut validation = fixture_validation_context();
        validation.service_provider.entity_id = "https://wrong.example.com/metadata".to_string();

        let error = parse_saml_response_at(
            &provider,
            &STANDARD.encode(response.as_bytes()),
            &validation,
            valid_fixture_now(),
        )
        .expect_err("wrong audience must be rejected");

        assert!(error.contains("audience"));
    }

    #[test]
    fn saml_response_parser_rejects_expired_assertion() {
        let provider = fixture_provider();
        let response = include_str!("../testdata/saml/response_signed.xml");
        let expired_now = Utc
            .with_ymd_and_hms(2024, 1, 18, 6, 25, 0)
            .single()
            .expect("valid expiration time");

        let error = parse_saml_response_at(
            &provider,
            &STANDARD.encode(response.as_bytes()),
            &fixture_validation_context(),
            expired_now,
        )
        .expect_err("expired response must be rejected");

        assert!(error.contains("expired"));
    }
}
