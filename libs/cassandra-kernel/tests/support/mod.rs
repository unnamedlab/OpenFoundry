use std::{
    env,
    sync::Arc,
    time::{Duration, Instant},
};

use cassandra_kernel::{ClusterConfig, SessionBuilder};
use testcontainers::{
    ContainerAsync, GenericImage, ImageExt,
    core::{IntoContainerPort, WaitFor},
    runners::AsyncRunner,
};

const DEFAULT_CASSANDRA_TEST_IMAGE: &str = "cassandra:5.0.2";
const CASSANDRA_TEST_IMAGE_ENV: &str = "CASSANDRA_TEST_IMAGE";
const CASSANDRA_TEST_START_TIMEOUT_SECS_ENV: &str = "CASSANDRA_TEST_START_TIMEOUT_SECS";
const CASSANDRA_TEST_CONNECT_TIMEOUT_SECS_ENV: &str = "CASSANDRA_TEST_CONNECT_TIMEOUT_SECS";
const DEFAULT_START_TIMEOUT_SECS: u64 = 240;
const DEFAULT_CONNECT_TIMEOUT_SECS: u64 = 90;
const CQL_READY_LOG: &str = "Starting listening for CQL clients";

pub struct CassandraTestCluster {
    _container: ContainerAsync<GenericImage>,
    pub session: Arc<scylla::Session>,
}

pub async fn start_cassandra() -> CassandraTestCluster {
    let image_ref = cassandra_image_ref();
    let start_timeout = duration_from_env(
        CASSANDRA_TEST_START_TIMEOUT_SECS_ENV,
        DEFAULT_START_TIMEOUT_SECS,
    );
    let connect_timeout = duration_from_env(
        CASSANDRA_TEST_CONNECT_TIMEOUT_SECS_ENV,
        DEFAULT_CONNECT_TIMEOUT_SECS,
    );

    let (image_name, image_tag) = split_image_ref(&image_ref);
    let image = GenericImage::new(image_name, image_tag)
        .with_exposed_port(9042.tcp())
        .with_wait_for(WaitFor::message_on_stdout(CQL_READY_LOG))
        .with_startup_timeout(start_timeout);

    let container = match tokio::time::timeout(start_timeout, image.start()).await {
        Ok(Ok(container)) => container,
        Ok(Err(err)) => {
            panic!(
                "{}",
                docker_start_error(&image_ref, start_timeout, &format!("{err:?}"))
            );
        }
        Err(_) => {
            panic!(
                "{}",
                docker_start_error(
                    &image_ref,
                    start_timeout,
                    "startup timed out before Docker/testcontainers returned",
                )
            );
        }
    };

    let host = container
        .get_host()
        .await
        .unwrap_or_else(|err| panic!("failed to read Cassandra container host: {err:?}"))
        .to_string();
    let port = container
        .get_host_port_ipv4(9042)
        .await
        .unwrap_or_else(|err| panic!("failed to read Cassandra mapped CQL port: {err:?}"));
    let endpoint = format!("{host}:{port}");

    let session = connect_session(&endpoint, connect_timeout)
        .await
        .unwrap_or_else(|err| {
            panic!(
                "{}",
                cql_connect_error(&image_ref, &endpoint, connect_timeout, &err)
            )
        });

    CassandraTestCluster {
        _container: container,
        session: Arc::new(session),
    }
}

async fn connect_session(endpoint: &str, timeout: Duration) -> Result<scylla::Session, String> {
    let started = Instant::now();

    loop {
        let cfg = ClusterConfig {
            contact_points: vec![endpoint.to_string()],
            local_datacenter: "datacenter1".to_string(),
            ..ClusterConfig::dev_local()
        };

        match SessionBuilder::new(cfg).build().await {
            Ok(session) => return Ok(session),
            Err(err) => {
                let last_error = err.to_string();
                if started.elapsed() >= timeout {
                    return Err(last_error);
                }
                tokio::time::sleep(Duration::from_secs(2)).await;
            }
        }
    }
}

fn cassandra_image_ref() -> String {
    match env::var(CASSANDRA_TEST_IMAGE_ENV) {
        Ok(value) if !value.trim().is_empty() => value.trim().to_string(),
        Ok(_) => panic!("{CASSANDRA_TEST_IMAGE_ENV} must not be empty"),
        Err(env::VarError::NotPresent) => DEFAULT_CASSANDRA_TEST_IMAGE.to_string(),
        Err(env::VarError::NotUnicode(_)) => {
            panic!("{CASSANDRA_TEST_IMAGE_ENV} must be valid UTF-8")
        }
    }
}

fn split_image_ref(image_ref: &str) -> (String, String) {
    if image_ref.contains('@') {
        panic!(
            "{CASSANDRA_TEST_IMAGE_ENV} must be a tagged image reference like \
             cassandra:5.0.2 or registry.internal/cassandra:5.0.2; digest refs are \
             not supported by this test helper"
        );
    }

    let last_slash = image_ref.rfind('/');
    if let Some(colon) = image_ref.rfind(':') {
        if last_slash.map_or(true, |slash| colon > slash) {
            let name = image_ref[..colon].trim();
            let tag = image_ref[colon + 1..].trim();
            if name.is_empty() || tag.is_empty() {
                panic!("{CASSANDRA_TEST_IMAGE_ENV} must include a non-empty image name and tag");
            }
            return (name.to_string(), tag.to_string());
        }
    }

    (image_ref.to_string(), "latest".to_string())
}

fn duration_from_env(name: &str, default_secs: u64) -> Duration {
    match env::var(name) {
        Ok(value) => {
            let secs = value
                .parse::<u64>()
                .unwrap_or_else(|_| panic!("{name} must be an integer number of seconds"));
            Duration::from_secs(secs)
        }
        Err(env::VarError::NotPresent) => Duration::from_secs(default_secs),
        Err(env::VarError::NotUnicode(_)) => panic!("{name} must be valid UTF-8"),
    }
}

fn docker_start_error(image_ref: &str, timeout: Duration, cause: &str) -> String {
    format!(
        "could not start the real Cassandra test container `{image_ref}` within {}s\n\n\
         cause from Docker/testcontainers:\n{cause}\n\n\
         This integration test requires Docker plus a locally available Cassandra image or \
         registry access.\n\
         Actions:\n\
         1. Check Docker: docker info\n\
         2. Pre-pull the selected image: docker pull {image_ref}\n\
         3. If Docker Hub is not reachable, load or mirror the image and run with \
         CASSANDRA_TEST_IMAGE=<registry>/cassandra:5.0.2\n\
         4. On slow machines, raise {CASSANDRA_TEST_START_TIMEOUT_SECS_ENV}.\n\n\
         Re-run:\n\
         CASSANDRA_TEST_IMAGE={image_ref} cargo test -p cassandra-kernel --test integration -- \
         --ignored --test-threads=1",
        timeout.as_secs()
    )
}

fn cql_connect_error(image_ref: &str, endpoint: &str, timeout: Duration, cause: &str) -> String {
    format!(
        "Cassandra container `{image_ref}` started, but CQL did not become reachable at \
         {endpoint} within {}s\n\n\
         last connection error:\n{cause}\n\n\
         Actions:\n\
         1. Inspect container logs in Docker for Cassandra bootstrap errors.\n\
         2. Confirm the image is compatible with the test local DC `datacenter1`.\n\
         3. On slow machines, raise {CASSANDRA_TEST_CONNECT_TIMEOUT_SECS_ENV}.",
        timeout.as_secs()
    )
}
