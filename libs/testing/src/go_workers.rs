//! Helpers for launching real Go Temporal workers from Rust E2E tests.

use std::{
    fs::File,
    io,
    path::{Path, PathBuf},
    process::Stdio,
    time::{Duration, SystemTime, UNIX_EPOCH},
};

use tokio::process::{Child, Command};

/// Child process wrapper for `go run .` worker replicas.
pub struct GoWorker {
    child: Child,
    log_path: PathBuf,
}

impl GoWorker {
    /// Start a worker module under `workers-go/<worker>`.
    pub async fn spawn(
        repo_root: impl AsRef<Path>,
        worker: &str,
        temporal_frontend: &str,
        namespace: &str,
    ) -> Self {
        let nonce = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .expect("clock before epoch")
            .as_nanos();
        let metrics_addr = "127.0.0.1:0";
        let gocache = std::env::temp_dir().join(format!("openfoundry-go-build-{nonce}"));
        let log_path =
            std::env::temp_dir().join(format!("openfoundry-{worker}-worker-{nonce}.log"));
        let stdout = File::create(&log_path)
            .unwrap_or_else(|error| panic!("failed to create Go worker log: {error}"));
        let stderr = stdout
            .try_clone()
            .unwrap_or_else(|error| panic!("failed to clone Go worker log: {error}"));
        let worker_dir = repo_root.as_ref().join("workers-go").join(worker);

        let mut child = Command::new("go")
            .arg("run")
            .arg(".")
            .current_dir(&worker_dir)
            .env("TEMPORAL_ADDRESS", temporal_frontend)
            .env("TEMPORAL_HOST_PORT", temporal_frontend)
            .env("TEMPORAL_NAMESPACE", namespace)
            .env("METRICS_ADDR", metrics_addr)
            .env("OF_LOG_LEVEL", "warn")
            .env("GOCACHE", gocache)
            .stdin(Stdio::null())
            .stdout(Stdio::from(stdout))
            .stderr(Stdio::from(stderr))
            .spawn()
            .unwrap_or_else(|error| panic!("failed to start Go worker {worker}: {error}"));

        tokio::time::sleep(Duration::from_secs(2)).await;
        if let Some(status) = child
            .try_wait()
            .unwrap_or_else(|error| panic!("failed to inspect Go worker {worker}: {error}"))
        {
            let logs = std::fs::read_to_string(&log_path).unwrap_or_default();
            panic!("Go worker {worker} exited during startup with {status}\n{logs}");
        }

        Self { child, log_path }
    }

    pub fn logs(&self) -> io::Result<String> {
        std::fs::read_to_string(&self.log_path)
    }

    /// Best-effort async shutdown for tests that want to wait for exit.
    pub async fn stop(&mut self) {
        let _ = self.child.start_kill();
        let _ = tokio::time::timeout(Duration::from_secs(5), self.child.wait()).await;
    }
}

impl Drop for GoWorker {
    fn drop(&mut self) {
        let _ = self.child.start_kill();
    }
}
