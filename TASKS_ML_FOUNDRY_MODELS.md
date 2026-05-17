# ML / Foundry Models — Tasks for 1:1 parity with Palantir Foundry

> **Target stack (consistent with `TASKS_COMPUTE_PIPELINES.md`):**
> Apache Spark/PySpark for batch training and batch inference, Iceberg
> for versioned data artifacts, K8s + Spark Operator and a new
> "inference runtime" (KServe-compatible or a Go-native equivalent) for live
> deployments, pgvector / Vespa for retrieval (already exist), Postgres for
> the experiment/registry metadata store, Kafka for inference logging.
> **Do not** introduce DuckDB or MLflow as-is: we will replicate the Foundry
> model (`palantir_models`, Modeling Objectives, live/batch deployments).
>
> **Current state acknowledged (do not redo):**
> - `libs/ml-kernel-go/` (~8k LoC, 47 files): already has `models/` with
>   `model`, `model_version`, `experiment`, `run`, `training_job`,
>   `feature`, `deployment`, `prediction`, `asset_lineage`, `overview`,
>   `interop`; `handlers/` with `models`, `deployments`, `predictions`,
>   `experiments`, `features`, `runs`, `training`, `asset_lineage`,
>   `overview`; `domain/drift.go` with tests.
> - [services/model-catalog-service/internal/repo/lifecycle.go](services/model-catalog-service/internal/repo/lifecycle.go) has
>   a real repository with migrations and adapter.
> - [services/model-deployment-service/](services/model-deployment-service/)
>   is **substrate-only** (only `server`, `config`, `mlkernel_proof_test`).
> - [services/ai-evaluation-service/internal/handlers/evaluations.go](services/ai-evaluation-service/internal/handlers/evaluations.go)
>   already implements the eval flow with 2 migrations.
> - [services/retrieval-context-service/](services/retrieval-context-service/)
>   has pgvector and Vespa backends.
> - [libs/vector-store/](libs/vector-store/) (~975 LoC) already exists.
> - [proto/ml/{model,serving,experiment,feature_store}.proto](proto/ml/) are
>   2-line stubs — **all gRPC contracting is still to be done**.
> - [apps/web/src/routes/ml/MlPage.tsx](apps/web/src/routes/ml/MlPage.tsx)
>   exists as a single page; full UI is missing.
>
> **What's missing for 1:1** is in the blocks below. Each task is a
> self-contained prompt with a link to the official documentation.

---

## Block A — Model Adapter SDK (parity with `palantir_models`)

### Task A1. Python package `openfoundry-models` (adapters)

**Context**: Foundry builds its entire integration around the
`palantir_models.ModelAdapter` class with `api()` and `predict()` methods. It is **the**
contract. There is no equivalent today.

**Prompt**:
> Create `sdks/python/openfoundry-models/` (module `openfoundry_models` with
> alias `ofm`) with full parity to the Foundry SDK `palantir_models`:
> 1. Base class `ofm.ModelAdapter` with methods `@classmethod api(cls) ->
>    Tuple[Dict, Dict]` and `predict(self, **kwargs) -> Dict`.
> 2. Contract types:
>    - `ofm.Pandas(columns=[(name, type), ...])`
>    - `ofm.Spark(columns=[...])`
>    - `ofm.Parameter(type, default=...)`
>    - `ofm.NDArray(shape: list[int], dtype: numpy.typing.DTypeLike)`
>    - `ofm.Object(ObjectType)` and `ofm.ObjectSet(ObjectType)` (binding to
>      `services/ontology-definition-service`)
>    - `ofm.FileSystem()` (access to files inside an Iceberg dataset)
>    - `ofm.MediaReference()` (input only)
>    - Validator: if the adapter is going to be published as a *Function*, forbid
>      `typing.Any` and require types in collections.
> 3. Supported column types: `str, int, float, bool, list, dict,
>    datetime.date, datetime.time, datetime.datetime, typing.Any,
>    MediaReference`.
> 4. Stable serializer of the API to JSON (canonical) for storing in
>    `model_versions.api_definition`.
> 5. `ofm.save(adapter_instance, path)` and `ofm.load(path)` with pickle +
>    metadata: dependencies (`requirements.txt` snapshot), Python
>    version, adapter version, API hash.
> 6. CLI `ofm publish --model-rid <rid> --artifact <path>` that uploads the
>    bundle to `model-catalog-service`.
>
> **References**:
> - Model adapter overview: https://www.palantir.com/docs/foundry/integrate-models/model-adapter-overview
> - Model adapter API definition: https://www.palantir.com/docs/foundry/integrate-models/model-adapter-api
> - Model adapter reference: https://www.palantir.com/docs/foundry/integrate-models/model-adapter-reference
> - Creating adapters: https://www.palantir.com/docs/foundry/integrate-models/model-adapter-creation
> - Reusable model adapters: https://www.palantir.com/docs/foundry/integrate-models/model-adapter-custom/index.html

### Task A2. Reusable adapter templates (sklearn / pytorch / xgboost / hf)

**Prompt**:
> Under `sdks/python/openfoundry-models/templates/` provide pre-built
> adapters:
> - `SklearnTabularAdapter` (Pandas input/output).
> - `PytorchTabularAdapter`.
> - `XgboostAdapter`.
> - `HuggingFaceTextAdapter` (tokenizer + model + generation).
> - `LangChainAdapter` (input: prompt; output: completion + tool calls).
> Each one deserializes its artifact with its native loader (`joblib`,
> `torch.load`, `xgb.Booster`, `transformers.from_pretrained`). Tests with
> small fixture models.
>
> **References**:
> - Reusable adapters: https://www.palantir.com/docs/foundry/integrate-models/model-adapter-custom/index.html
> - Add support for a modeling library: https://www.palantir.com/docs/foundry/develop-models/additional-libraries

### Task A3. Adapter for LLMs (parity with `pm.LanguageModelAdapter`)

**Prompt**:
> Implement `ofm.LanguageModelAdapter` with Foundry's canonical API for
> LLMs: `chat_completion(messages, params)`, `tool_call(messages, tools)`,
> `embeddings(texts)`. Subclasses:
> - `OpenAICompatAdapter` (any OpenAI-compatible endpoint).
> - `AnthropicAdapter`.
> - `LocalLlamaAdapter` (vLLM/transformers backend).
> The adapter is registered in `services/llm-catalog-service` (already exists)
> as a *provider* and becomes available to `agent-runtime-service`,
> AIP Logic, and `retrieval-context-service`.
>
> **References**:
> - Language model adapters: https://www.palantir.com/docs/foundry/integrate-models/language-models-adapters

---

## Block B — Model Catalog & Registry

### Task B1. Fill in `proto/ml/model.proto` and CRUD endpoints

**Context**: the proto is a stub. `model-catalog-service` already has
`repo/adapter.go`, `repo/lifecycle.go`, `models/models.go`, and `handlers.go`.

**Prompt**:
> Fill in [proto/ml/model.proto](proto/ml/model.proto) with services and
> messages for Foundry parity:
> - `ModelService`: `CreateModel`, `GetModel`, `ListModels`, `UpdateModel`,
>   `ArchiveModel`, `RestoreModel`.
> - `ModelVersionService`: `CreateModelVersion`, `GetModelVersion`,
>   `ListModelVersions`, `UploadArtifact`, `FinalizeArtifact`,
>   `PromoteModelVersion`, `DeprecateModelVersion`.
> - `ModelStageService`: `SetStage` (`DRAFT|STAGING|PRODUCTION|ARCHIVED`),
>   `ListByStage`. Foundry conceptual model: each model has `versions[]` and
>   each version is promoted by *stage*. Promotion fires hooks
>   (auto-redeploy of live deployments pinned to `PRODUCTION`).
> - Messages: `Model`, `ModelVersion {rid, semver, api_definition_json,
>   artifact_uri, adapter_library_uri, framework, training_run_rid,
>   created_at, created_by, stage, metrics: map<string,double>,
>   parameters: map<string,string>}`.
>
> Implement the corresponding HTTP handlers on top of the stores that
> already exist in [services/model-catalog-service/internal/repo/](services/model-catalog-service/internal/repo/).
> Store artifacts in `s3://models/{model_rid}/{version_rid}/artifact.tar.gz`.
>
> **References**:
> - Models — core concepts: https://www.palantir.com/docs/foundry/model-integration/models
> - Model versioning: https://www.palantir.com/docs/foundry/model-integration/model-versions
> - Model assets in Code Repositories: https://www.palantir.com/docs/foundry/integrate-models/model-asset-code-repositories

### Task B2. Stages + promotion + dependency lineage

**Prompt**:
> Implement the promotion state machine using
> [libs/state-machine](libs/state-machine):
> - `DRAFT → STAGING → PRODUCTION → ARCHIVED` with transitions
>   `submit_for_staging`, `approve_staging`, `promote_to_production`,
>   `archive`, `rollback_to_staging`.
> - Each transition requires configured approvers (parity with Modeling
>   Objectives, Block I).
> - On promotion to `PRODUCTION`, any live deployment that has
>   `pin_strategy: "production"` receives a `model.version.promoted` event
>   and re-applies its rollout.
> - Asset lineage: each `ModelVersion` records in `model_asset_lineage`
>   (model already exists) the training datasets, the training run,
>   the repo+commit, and the `pipeline_run_rid`. Wire the sink to
>   the `lineage-service`.
>
> **References**:
> - Upgrade model adapter without retraining: https://www.palantir.com/docs/foundry/integrate-models/upgrade-model-adapter
> - Model versioning: https://www.palantir.com/docs/foundry/model-integration/model-versions

### Task B3. Model archive: bundle contents

**Prompt**:
> Define the canonical layout of the artifact bundle:
> ```
> artifact.tar.gz/
>   adapter.py                 # user's ModelAdapter subclass
>   manifest.json              # api_definition, python_version, libs,
>                              # adapter_library, framework
>   requirements.txt           # exact snapshot of the env
>   weights/                   # model weight(s) (joblib/pt/safetensors)
>   assets/                    # tokenizers, vocab, scalers, etc
>   conda.yaml | env.yaml      # optional managed environment
> ```
> Implement resumable multipart upload to S3 and validate the manifest
> against the adapter on `Finalize`. Return `artifact_sha256` and reject
> duplicate versions with the same hash.
>
> **References**:
> - Model adapter packaging: https://www.palantir.com/docs/foundry/integrate-models/model-adapter-creation

---

## Block C — Experiment Tracking

### Task C1. Fill in `proto/ml/experiment.proto` and expose endpoints

**Context**: `libs/ml-kernel-go/models/experiment.go` and `run.go` exist;
`handlers/experiments.go` and `runs.go` too. gRPC + wiring are missing.

**Prompt**:
> Fill in [proto/ml/experiment.proto](proto/ml/experiment.proto) with
> MLflow-like parity that Foundry exposes via Modeling Objectives:
> - `ExperimentService`: `CreateExperiment`, `GetExperiment`,
>   `ListExperiments`, `DeleteExperiment`.
> - `RunService`: `StartRun {experiment_id, params, tags}`,
>   `LogParam`, `LogParams` (batch), `LogMetric {key, value, step,
>   timestamp}`, `LogMetrics` (batch), `LogArtifact {run_id, path,
>   uri}`, `SetTag`, `FinishRun {status: COMPLETED|FAILED|KILLED}`,
>   `GetRun`, `SearchRuns {filter, order_by, max_results, page_token}`.
> - Messages: `Experiment {rid, name, artifact_location, owner,
>   created_at}`, `Run {rid, experiment_rid, status, start_ms, end_ms,
>   params: map, metrics: list<MetricPoint>, tags: map, artifact_uri,
>   user, source_repo_commit, source_pipeline_run_rid}`.
>
> Create a new service `services/experiment-tracking-service/` (by cloning
> `services/template/`) that mounts the kernel handlers
> (`libs/ml-kernel-go/handlers/experiments.go`, `runs.go`,
> `training.go`). Persist in Postgres + artifacts in S3
> (`s3://experiments/{experiment_rid}/{run_rid}/`). Provide endpoint
> `GET /runs/{rid}/artifacts/*` with streaming.
>
> **References**:
> - Modeling Objectives — runs & metrics: https://www.palantir.com/docs/foundry/manage-models/modeling-objectives-overview
> - Foundry ML training & runs: https://www.palantir.com/docs/foundry/develop-models/python-models
> - Compare runs UI: https://www.palantir.com/docs/foundry/manage-models/compare-runs

### Task C2. Python client `openfoundry.experiments` (MLflow API parity)

**Prompt**:
> In `sdks/python/openfoundry-models/openfoundry/experiments.py`, expose a
> client that mimics the MLflow API to minimize friction:
> ```python
> from openfoundry import experiments as exp
> with exp.start_run(experiment="loan-defaults"):
>     exp.log_param("lr", 0.01)
>     exp.log_metric("auc", 0.92)
>     exp.log_artifact("./plot.png")
>     exp.log_model(adapter)  # publishes as ModelVersion automatically
> ```
> Internally it talks to `experiment-tracking-service`. `log_model` also
> uploads the artifact and invokes `CreateModelVersion` in `model-catalog-service`
> with the `training_run_rid` linked.
>
> **References**:
> - Train in Code Repositories: https://www.palantir.com/docs/foundry/integrate-models/model-asset-code-repositories

### Task C3. Auto-tracking from transforms

**Prompt**:
> When a Python transform (Task C1 of `TASKS_COMPUTE_PIPELINES.md`)
> is decorated with `@ofm.training_transform(experiment="...")`,
> automatically:
> - opens a run on start (`ctx.run_id`).
> - captures `params` from `@configure` and from `**hyperparams`.
> - finalizes the run with the build's status.
> - links `pipeline_run_rid` ↔ `experiment_run_rid` in `asset_lineage`.
>
> **References**:
> - Train via transforms: https://www.palantir.com/docs/foundry/integrate-models/model-asset-code-repositories

### Task C4. UI: experiment explorer, compare runs, parallel coordinates

**Prompt**:
> In [apps/web/src/routes/ml/](apps/web/src/routes/ml/) create sub-routes:
> - `/ml/experiments` → table with experiments.
> - `/ml/experiments/{id}` → runs with filters, pivotable columns of
>   params and metrics.
> - `/ml/experiments/{id}/compare?runs=...` → comparative tables +
>   `parallel-coordinates` and `metric-vs-step` charts.
> - `/ml/runs/{rid}` → details: artifacts, lineage to the training pipeline,
>   logs (driver stdout), published model.
>
> **References**:
> - Compare runs: https://www.palantir.com/docs/foundry/manage-models/compare-runs
> - Modeling project navigation: https://www.palantir.com/docs/foundry/manage-models/navigation

---

## Block D — Training (Code Repositories + Pipelines)

### Task D1. `@model_transform` decorator (parity with `@train`)

**Prompt**:
> Add to `sdks/python/foundry-transforms` (created in
> `TASKS_COMPUTE_PIPELINES.md` Block C):
> ```python
> @ofm.model_transform(
>     model_output=ofm.ModelOutput("ri.model.xyz"),
>     train_data=Input("ri.dataset.abc"),
> )
> def train_loan_default(ctx, model_output, train_data):
>     df = train_data.dataframe().toPandas()
>     pipeline = sklearn.Pipeline(...)
>     pipeline.fit(df.drop("y", axis=1), df["y"])
>     adapter = MySklearnAdapter(pipeline=pipeline)
>     ctx.log_param("max_depth", 6); ctx.log_metric("auc", 0.91)
>     model_output.publish(adapter, stage="STAGING")
> ```
> The `pipeline-runner-spark` runtime (PySpark, Task C1 of the pipelines
> file) detects the decorator, opens the run, captures the
> metrics, and at the end calls `CreateModelVersion` with the bundle.
>
> **References**:
> - Train in Code Repositories: https://www.palantir.com/docs/foundry/integrate-models/model-asset-code-repositories
> - Models in transforms: https://www.palantir.com/docs/foundry/develop-models/python-models

### Task D2. Migration of legacy `foundry_ml` (optional compat shim)

**Prompt**:
> Foundry deprecated `foundry_ml` in favor of `palantir_models` (Oct 2025).
> Provide a shim `from openfoundry_ml.legacy import Stage, Model` that
> maps the stages API to the new `ModelAdapter` to ease migration
> for users arriving with legacy notebooks. Document in the README that
> the modern path is `openfoundry_models`. **Do not** invest effort: just
> the minimum so that `Stage(sklearn_estimator).save()` doesn't break.
>
> **References**:
> - foundry_ml → palantir_models migration: https://www.palantir.com/docs/foundry/develop-models/python-models

---

## Block E — Live Deployments (REST inference)

### Task E1. Inference runtime: HTTP contract

**Context**: `model-deployment-service` is substrate-only. Foundry exposes
`POST /foundry-ml-live/api/inference/transform/{deployment_rid}/v2` with
Bearer auth.

**Prompt**:
> Complete [services/model-deployment-service](services/model-deployment-service)
> with parity to Foundry's v2 endpoint:
> 1. Fill in [proto/ml/serving.proto](proto/ml/serving.proto) with
>    `DeploymentService`: `CreateLiveDeployment`, `GetLiveDeployment`,
>    `ListLiveDeployments`, `UpdateLiveDeployment {replicas, resources,
>    autoscaler}`, `DeleteLiveDeployment`, `StartDeployment`,
>    `StopDeployment`, `RolloutDeployment {strategy: blue_green|canary|
>    rolling, traffic_split}`.
> 2. Implement `POST /openfoundry-ml-live/api/inference/transform/
>    {deployment_rid}/v2` that:
>    - Validates Bearer JWT via `libs/auth-middleware`.
>    - Checks `authz-cedar-go` for action `ml.deployment.invoke`.
>    - Forwards the JSON to the adapter pod (Task E2).
>    - Returns the response as-is + headers
>      `X-Deployment-Rid`, `X-Model-Version-Rid`, `X-Latency-Ms`.
> 3. Errors with Foundry-parity codes: 400, 422, 429 (rate limit with
>    sliding window per `(deployment_rid, user_rid)`), 500, 503.
>
> **References**:
> - Live deployment reference: https://www.palantir.com/docs/foundry/manage-models/live-deployment-reference
> - Models in the ontology: https://www.palantir.com/docs/foundry/ontology/models
> - Manage modeling project: https://www.palantir.com/docs/foundry/manage-models/models-in-the-ontology

### Task E2. Inference runtime: the adapter pod

**Prompt**:
> Build a base image `openfoundry/inference-runtime:py-3.11` that:
> 1. Receives envvars `MODEL_VERSION_RID`, `ARTIFACT_URI`, `OFM_LOG_KAFKA`.
> 2. On startup downloads the bundle (`artifact.tar.gz`) from S3, validates
>    `manifest.json`, installs `requirements.txt` (cacheable layer).
> 3. Imports the `ModelAdapter` class, calls `load(weights_dir)`,
>    serves a FastAPI with `POST /v2` that delegates to `adapter.predict(**body)`.
> 4. Supports GPU if `ModelVersion.manifest.requires_gpu=true` (the
>    GPU-variant image `:py-3.11-cuda12`).
> 5. Health probes `/healthz`, `/readyz`.
> 6. Emits OTel traces and Prometheus metrics (`inference_latency_ms`,
>    `inference_errors_total`, `model_loaded_at`).
>
> Render the `Deployment + Service + HPA + PodDisruptionBudget` and
> submit it to K8s (use the Go client that already exists in
> `services/pipeline-build-service/internal/spark/spark.go` as
> a reference pattern). Each deployment lives in a per-tenant namespace.
>
> **References**:
> - Live deployment infrastructure: https://www.palantir.com/docs/foundry/manage-models/live-deployment-reference
> - Resource management: https://www.palantir.com/docs/foundry/resource-management/compute-usage

### Task E3. Autoscaling + readiness + warmup

**Prompt**:
> Support HPA with custom metrics (Prometheus adapter):
> `min_replicas`, `max_replicas`, `target_qps_per_replica`,
> `target_latency_p95_ms`, `scale_down_stabilization_window`.
> Warmup: when a replica starts, it issues N synthetic predicts with a
> `warmup_payload` declared in `manifest.json` before marking `ready`.
>
> **References**:
> - Live deployment scaling: https://www.palantir.com/docs/foundry/manage-models/live-deployment-reference
> - Latency / cold start considerations: https://www.palantir.com/docs/foundry/manage-models/modeling-objectives-overview

### Task E4. Rollout strategies (blue-green, canary, rolling)

**Prompt**:
> Implement `RolloutDeployment`:
> - `blue_green`: stand up new version 100%, swap, drop old after N min.
> - `canary`: traffic split (10/90 → 25/75 → 50/50 → 100/0), advances
>   automatically if metrics (error rate, p95 lat) are within range
>   against the baseline; otherwise, auto-rollback.
> - `rolling`: surge/maxUnavailable as a standard K8s Deployment.
> Use an Envoy/HAProxy sidecar in the deployment's service for traffic
> split (or, preferred alternative, an Istio `VirtualService` if
> the cluster has it; support both).
>
> **References**:
> - Deploy with DevOps and Marketplace: https://www.palantir.com/docs/foundry/model-integration/marketplace-models

### Task E5. Externally hosted models (BYO endpoint)

**Prompt**:
> Support `ModelVersion.kind = "EXTERNAL"` whose "artifact" is a remote
> URL + secure credentials (stored in
> `services/identity-federation-service` as a sealed secret). The
> external-type deployment **does not** create pods; the `/v2` endpoint
> forwards to the external endpoint with dynamic auth resolution.
> Supports protocols: `OpenAI`, `Anthropic`, `Custom REST` with
> declarative request/response mapping (`request_template`,
> `response_path`).
>
> **References**:
> - Externally hosted models: https://www.palantir.com/docs/foundry/integrate-models/external-model-connection

---

## Block F — Batch Deployments + auto-evaluation

### Task F1. Batch deployments against datasets

**Prompt**:
> Add to `proto/ml/serving.proto` `BatchDeploymentService`:
> `CreateBatchDeployment {model_version_rid, input_dataset_rid,
> output_dataset_rid, schedule_rid?, profile_id}`,
> `RunBatchDeployment {deployment_rid, snapshot_id?}`,
> `ListBatchDeploymentRuns`. Implementation:
> - A PySpark build (Block C of pipelines) that:
>   - Reads `input_dataset_rid` (Iceberg).
>   - Loads the adapter (same bundle as live).
>   - Applies `adapter.predict(...)` chunk-wise via Spark `mapInPandas`.
>   - Writes to `output_dataset_rid` with a `SNAPSHOT` transaction.
> - If the adapter declares a single `Pandas` in/out (eval-compatible),
>   the build triggers Task F2 with the eval set on completion.
>
> **References**:
> - Batch deployment overview: https://www.palantir.com/docs/foundry/manage-models/batch-deployment
> - Models trained in Foundry: https://www.palantir.com/docs/foundry/integrate-models/model-asset-code-repositories

### Task F2. Auto-evaluation in Modeling Objectives

**Prompt**:
> When a `ModelVersion` meets compatibility (single tabular
> input/output) and the Modeling Objective has an *evaluation set* with a
> declared label column:
> 1. After `RunBatchDeployment`, launch a job that joins predictions with
>    ground truth on the primary key.
> 2. Compute metrics based on the configured `metric_set` (classification:
>    accuracy, precision, recall, f1, auc, log_loss; regression: rmse,
>    mae, r2; ranking: ndcg, map; multi-class: confusion matrix).
> 3. Save in `evaluation_runs` linked to `model_version_rid`.
> 4. The UI renders them in a sub-view of the Modeling Objective.
>
> **References**:
> - Auto-evaluation: https://www.palantir.com/docs/foundry/manage-models/modeling-objectives-overview
> - Evaluation metrics: https://www.palantir.com/docs/foundry/manage-models/evaluation-metrics

---

## Block G — Feature Store (offline + online)

### Task G1. Create `services/feature-store-service`

**Context**: `libs/ml-kernel-go/models/feature.go` and `handlers/features.go`
exist as the kernel; a dedicated service and the offline/online split are missing.

**Prompt**:
> Create `services/feature-store-service/` (by cloning `services/template/`).
> Fill in [proto/ml/feature_store.proto](proto/ml/feature_store.proto) with:
> - `FeatureViewService`: `CreateFeatureView {name, entity_keys: [],
>   features: [{name, type, transformation_sql}], source_dataset_rid,
>   ttl_seconds, online: bool, offline: bool, freshness_sla_seconds}`,
>   `GetFeatureView`, `ListFeatureViews`, `MaterializeFeatureView`,
>   `DeleteFeatureView`.
> - `EntityService`: `CreateEntity {name, join_keys}`, `ListEntities`.
> - `FeatureService`: `GetOnlineFeatures {feature_refs: ["fv:f1","fv:f2"],
>   entity_rows: [{key: value}]}` → vector response.
> - `OfflineFeatureService`: `GetHistoricalFeatures {feature_refs,
>   entity_dataframe}` → produces a dataset with point-in-time correctness.
>
> Storage:
> - **Offline**: an Iceberg table per `FeatureView` (you already have the catalog).
>   The materializer is a Spark transform (`pipeline-runner-spark`) that
>   runs `transformation_sql` and writes to Iceberg with a timestamp.
> - **Online**: Cassandra (you already have `cassandra-kernel`) or Redis as
>   a key-value cache. Push materializer: a streaming consumer that listens to
>   Iceberg commits and publishes to the online store with TTL.
>
> Sync (low-latency) endpoint for inference: P99 < 20ms.
>
> **References**:
> - Feature pipelines: https://www.palantir.com/docs/foundry/develop-models/feature-pipelines
> - Foundry feature store concepts: https://www.palantir.com/docs/foundry/develop-models/python-models

### Task G2. Point-in-time correctness for training

**Prompt**:
> `GetHistoricalFeatures(entity_dataframe)` must guarantee PIT
> correctness: given a dataframe with `(entity_key, event_timestamp)`,
> the result contains the feature value **as it existed** at
> `event_timestamp` (not the most recent). Use Iceberg `time travel`
> (`VERSION AS OF` or `TIMESTAMP AS OF`) or as-of joins (`AS OF JOIN`).
>
> **References**:
> - PIT joins (Iceberg / Spark): https://iceberg.apache.org/docs/latest/spark-queries/#time-travel
> - Feature consistency: https://www.palantir.com/docs/foundry/develop-models/feature-pipelines

### Task G3. Drift / staleness alerts

**Prompt**:
> Leverage `libs/ml-kernel-go/domain/drift.go` (already implemented):
> - Implement a periodic watcher that, for each `FeatureView` with
>   `freshness_sla_seconds`, checks `max(event_timestamp) >
>   now - sla`. If not, send an alert to `notification-alerting-service`.
> - Statistical drift: PSI / KS-test against a baseline. When
>   `psi > 0.2`, mark the feature as `DRIFTED` and create a
>   "data health check" visible in the FeatureView UI.
>
> **References**:
> - Data health & drift in Foundry: https://www.palantir.com/docs/foundry/data-health/overview

---

## Block H — Model Functions + Models in the Ontology

### Task H1. Auto-publish Model Functions

**Prompt**:
> When a `LiveDeployment` or `BatchDeployment` becomes `ACTIVE`,
> automatically register a **Model Function** in
> [services/ontology-actions-service/](services/ontology-actions-service/)
> (which will receive handlers via the compute Block tasks):
> - Name: `score_<model_name>_v<version>`.
> - Signature inferred from `model_version.api_definition`:
>   - `ofm.Pandas(...)` → input/output as `ObjectSet` when the
>     columns match properties of the bound object type.
>   - `ofm.Parameter(...)` → scalar parameters.
>   - `ofm.Object(...)` → receives an ObjectRid and resolves via
>     `ontology-query-service`.
> - The function body calls the deployment's `/v2` endpoint.
> - Generates a TS/Python SDK that appears in `services/sdk-generation-service`.
>
> **References**:
> - Functions on models: https://www.palantir.com/docs/foundry/functions/functions-on-models
> - Models in the ontology: https://www.palantir.com/docs/foundry/ontology/models

### Task H2. Binding Model ↔ Object Type

**Prompt**:
> In `services/ontology-definition-service`, add
> `model_bindings`:
> `POST /object-types/{ot_rid}/model-bindings {model_rid,
> property_to_input_map: {object_prop: model_input_name},
> output_to_property_map: {model_output: object_prop_or_action}}`.
> When a user invokes the Model Function on an instance, the
> binding automatically translates properties → inputs and outputs →
> property updates (via an existing action) or columns in the response.
>
> Offer three application modes:
> - **On-demand** (call from UI on the Object detail page).
> - **Scheduled batch** (cron that walks the entire ObjectSet).
> - **Streaming** (subscribes to `object.changed` and reactivates).
>
> **References**:
> - Models in the Ontology — binding: https://www.palantir.com/docs/foundry/ontology/models
> - Apply model to objects: https://www.palantir.com/docs/foundry/manage-models/models-in-the-ontology
> - Functions on objects: https://www.palantir.com/docs/foundry/functions/functions-on-objects

### Task H3. Workshop / Vertex integration

**Prompt**:
> Expose the Model Functions as blocks in
> [apps/web/src/lib/components/ontology/ActionExecutor.tsx](apps/web/src/lib/components/ontology/ActionExecutor.tsx)
> and as a Workshop widget:
> - "Score Object" widget (input: object selection, output: prediction).
> - "Batch Score Set" widget.
> - Workshop variable `${model_score}` bindable to charts.
> Same from Vertex with the object representation of the knowledge graph.
>
> **References**:
> - Workshop model widgets: https://www.palantir.com/docs/foundry/workshop/widgets-models
> - Vertex models: https://www.palantir.com/docs/foundry/vertex/models

---

## Block I — Modeling Objectives (governance)

### Task I1. Modeling Objective as governance aggregator

**Prompt**:
> Create `services/modeling-objective-service/` with:
> - `Objective {rid, name, description, owner, problem_type: classification|
>   regression|ranking|nlp|cv, evaluation_set_dataset_rid, label_column,
>   primary_metric, deployment_targets: [], reviewers: []}`.
> - `Submission`: each `ModelVersion` is "submitted" to an Objective. States:
>   `SUBMITTED, IN_REVIEW, APPROVED, REJECTED, ACTIVE, RETIRED`.
> - Reviewers receive a notification; approval marks the version as
>   `STAGING` (Block B2). A second approval promotes it to `PRODUCTION`.
> - Auto-eval (F2) runs on submit and blocks promotion if the primary
>   metric is below a configured threshold.
> - UI: "Modeling Objective" view at `/ml/objectives/{rid}` with a leaderboard
>   of versions, their metrics, owners, lineage.
>
> **References**:
> - Modeling Objectives overview: https://www.palantir.com/docs/foundry/manage-models/modeling-objectives-overview
> - Submission & review workflow: https://www.palantir.com/docs/foundry/manage-models/modeling-objectives-overview

### Task I2. Approval policies & restricted markings

**Prompt**:
> Wire `authz-cedar-go`:
> - Action `ml.objective.approve` requires `role:ml-reviewer`.
> - `ml.objective.deploy_to_production` requires dual approval if
>   `objective.risk_level >= HIGH`.
> - `ml.model.invoke` can have markings: if the model was trained
>   on a dataset with marking `PII`, only callers with clearance can
>   invoke it.
> - Audit all actions to `audit-sink` with an OpenLineage-like shape.
>
> **References**:
> - Policies & markings: https://www.palantir.com/docs/foundry/security/markings-overview

---

## Block J — Monitoring, Inference Logging & Drift

### Task J1. Inference logging to Iceberg

**Prompt**:
> The inference-runtime (E2) emits a Kafka event `inference.events.v1`
> for each request: `{deployment_rid, model_version_rid, request_id,
> user_rid, ts, inputs_json, outputs_json, latency_ms, status,
> error_message?}`. Create `services/inference-sink/` (clone of `ai-sink`)
> that consumes and projects to an Iceberg table `inference_log` partitioned
> by (deployment_rid, day). High volume: use batching and zstd compression.
>
> **References**:
> - Inference monitoring: https://www.palantir.com/docs/foundry/manage-models/monitor-models
> - Model performance: https://www.palantir.com/docs/foundry/manage-models/live-deployment-reference

### Task J2. Model performance dashboards

**Prompt**:
> Build a built-in dashboard at `/ml/models/{rid}/monitoring`:
> - Requests/min volume, error rate %, latency p50/p95/p99 (from
>   `inference_log`).
> - Input distribution per feature (PSI vs train baseline).
> - Output distribution (predictions histogram).
> - If there is ground-truth feedback (an action that materializes the true
>   label), compute *retroactive metrics* with a sliding window.
> - Alerts: if error rate > X% for Y min, fire a notification.
>
> **References**:
> - Monitor models: https://www.palantir.com/docs/foundry/manage-models/monitor-models
> - Model dashboards: https://www.palantir.com/docs/foundry/manage-models/monitor-models

### Task J3. Shadow / champion-challenger

**Prompt**:
> "Shadow" mode for a `LiveDeployment`: the request goes to the champion
> (which serves the response to the client) and in parallel is forwarded to
> the challenger; its predictions are logged alongside the `request_id` to
> allow later comparison (without affecting client latency).
> Admin endpoint to promote challenger → champion when metrics justify it.
>
> **References**:
> - Shadow & challenger patterns: https://www.palantir.com/docs/foundry/manage-models/modeling-objectives-overview

---

## Block K — Marketplace, Externals, and Reproducibility

### Task K1. Package model + dependencies as a Marketplace product

**Prompt**:
> When the user "publishes" a Modeling Objective to Marketplace:
> 1. Bundle = `ModelVersion artifact + ModelAdapter wheel + managed
>    environment image ref + ontology bindings JSON + deployment configs`.
> 2. Sign the bundle with the tenant's key.
> 3. Publish to `federation-product-exchange-service` (already has 8
>    migrations) as `product_type: ml_model`.
> On the destination tenant, "install product" provisions: the model in the
> catalog, the ontology bindings, and optionally a live deployment.
>
> **References**:
> - Deploy with DevOps and Marketplace: https://www.palantir.com/docs/foundry/model-integration/marketplace-models

### Task K2. Managed environments compatible across surfaces

**Prompt**:
> Reuse `library-environment-service` (Task H3 of the pipelines file):
> each `ModelVersion` references an `environment_id` that is used
> both in the inference-runtime pod (E2) and in the training
> transform and in Code Workspaces to reproduce development
> (Maestro parity).
>
> **References**:
> - Managed environments / Maestro: https://www.palantir.com/docs/foundry/code-workspaces/managed-environments

### Task K3. Reproducibility report

**Prompt**:
> For each `ModelVersion` generate a *reproducibility report* PDF/HTML:
> repo commit, hash of the training datasets (Iceberg snapshot),
> `environment_id`, metrics, parameters, RIDs of
> `pipeline_run` and `experiment_run`. Immutable, stored alongside the
> bundle.
>
> **References**:
> - Model lineage & reproducibility: https://www.palantir.com/docs/foundry/integrate-models/model-asset-code-repositories

---

## Block L — UI

### Task L1. Complete `/ml` page

**Context**: today only [apps/web/src/routes/ml/MlPage.tsx](apps/web/src/routes/ml/MlPage.tsx)
exists as a single page.

**Prompt**:
> Create the structure:
> - `/ml/models` — catalog (table, filters by owner/stage/framework).
> - `/ml/models/{rid}` — detail with tabs: Overview, Versions,
>   Deployments, Lineage, Monitoring, Bindings.
> - `/ml/models/{rid}/versions/{vid}` — version detail: API
>   definition viewer, artifacts, training run link.
> - `/ml/experiments`, `/ml/experiments/{id}` (Block C4).
> - `/ml/objectives`, `/ml/objectives/{rid}` (Block I).
> - `/ml/feature-store`, `/ml/feature-store/{view}` (Block G).
> - `/ml/deployments` — global view of live + batch.
> - `/ml/registry/promotions` — pending promotions queue.
>
> Use the table and drawer components already present in
> [apps/web/src/lib/components/](apps/web/src/lib/components/).
> Reuse the [aip-evals](apps/web/src/routes/aip-evals/) page as
> a pattern for the eval result viewer (much of its work —columns,
> per-test-case debug view, traces— is reusable for auto-evals of
> Task F2).
>
> **References**:
> - Modeling project navigation: https://www.palantir.com/docs/foundry/manage-models/navigation
> - Monitor models UI: https://www.palantir.com/docs/foundry/manage-models/monitor-models

---

## Block M — Parity validation

### Task M1. E2E smoke test "ML parity"

**Prompt**:
> In `tests/parity/ml/`, a Go + Python suite against a local cluster:
> 1. Create a Modeling Objective with problem_type=classification and an
>    eval dataset with label.
> 2. Code Repository with `@ofm.model_transform` sklearn → trains, logs
>    params/metrics, publishes ModelVersion v1 stage=DRAFT.
> 3. Auto-eval runs, computes AUC vs eval set.
> 4. Submit to Objective → review approve → STAGING → second approve →
>    PRODUCTION.
> 5. Create LiveDeployment of v1 → curl the `/v2` endpoint with
>    tabular JSON → verify response.
> 6. Create a Model Function auto-binding to ObjectType `Loan`; invoke from
>    a simulated Workshop; verify that the `predicted_default` property
>    is set via action.
> 7. Create BatchDeployment → runs weekly → produces a predictions
>    dataset.
> 8. Create a FeatureView with 3 features → materialize offline (Iceberg) →
>    `GetOnlineFeatures` returns in < 50ms.
> 9. Inject synthetic drift into features → verify that
>    `notification-alerting-service` receives an alert.
> 10. Promote v2 with canary 25% → 50% → 100% without auto-rollback.
>
> Run as part of `make test-integration`.
>
> **References**:
> - End-to-end tutorial: https://www.palantir.com/docs/foundry/manage-models/modeling-objectives-overview
> - Models tutorial: https://www.palantir.com/docs/foundry/model-integration/tutorial-train-jupyter-notebook

---

## Block N — Modeling Objectives gated release (added 2026-05-17)

Modeling Objectives are Palantir's "mission control" for a modeling
problem: a single resource that owns the problem statement, candidate
submissions, evaluation results, reviewer workflow, and the release →
deployment handoff. The earlier blocks above (I1, I2) treat them as a
governance wrapper around catalog + deployments; this block defines the
gated release workflow that makes the objective genuinely the production
gate.

### Task N1. Objective resource and submissions

**Prompt**:
> Add `modeling_objective` rows that own:
> - problem statement (markdown), target metrics list, evaluation
>   dataset RID, baseline model RID (optional).
> - submission queue: model version + adapter version + evaluation result
>   + submitter + timestamp + status (pending, reviewed, approved,
>   rejected, retired).
> - reviewer policy: who can review (group RIDs), required approvals,
>   blocking criteria (metric thresholds).
>
> Implement `POST /objectives`, `POST /objectives/{rid}/submissions`,
> `GET /objectives/{rid}` and a per-submission detail endpoint that
> resolves the candidate's evaluation summary, lineage, and audit trail.
> Submissions must reuse the existing model catalog / evaluation tables
> (no parallel copy).
>
> **References**:
> - Modeling Objectives overview: https://www.palantir.com/docs/foundry/manage-models/modeling-objectives-overview

### Task N2. Reviewer workflow + gated release

**Prompt**:
> Build the reviewer queue UI in `apps/web/src/routes/ml/`:
> - inbox of pending submissions per objective with metric diff vs
>   baseline.
> - per-submission decision panel: approve, request changes, reject,
>   with mandatory rationale.
> - block "release" until the configured number of approvals is
>   reached and all blocking criteria pass.
>
> Implement `POST /objectives/{rid}/submissions/{sid}/decision` that
> records the decision in audit (`audit-compliance-service`) and
> transitions submission status. A "release" transition is a separate
> endpoint that requires `approved` status + reviewer quorum and emits
> a `model_objective_release` event consumed by the deployment service.

### Task N3. Release → deployment handoff

**Prompt**:
> When a submission is released, the deployment service picks up the
> `model_objective_release` event and either:
> - promotes the released model version to the configured live
>   deployment slot (canary/full per the objective's deployment
>   policy), or
> - creates a batch deployment job for the next scheduled inference
>   window.
>
> Releases are immutable: rolling back to a prior release re-runs the
> handoff with the previous model version and records the rollback in
> audit. The objective's "current release" pointer must be visible
> from the Workshop ML module so consumers can see which model the
> Ontology bindings currently resolve to.

### Task N4. Objective in lineage and Compass

**Prompt**:
> Surface objectives as Compass resources (stable RID, search index,
> breadcrumbs, sharing). Append objective nodes to the lineage graph
> between the input datasets and the deployed model so a user inspecting
> a model serving endpoint can trace back to the objective and its
> release history.
>
> **Acceptance**:
> 1. Create an objective, submit a model from a notebook training run,
>    evaluate, request review.
> 2. Approve with two reviewers (one rejection path tested separately).
> 3. Release; verify the deployment slot updates and the lineage graph
>    shows the objective node.
> 4. Roll back; verify the previous version is restored and audit
>    captures both transitions.

---

## Recommended execution order

1. **A1, A2, B1, B3** — Adapter SDK + functional Catalog/registry with bundle.
2. **C1, C2** — Experiment tracking (service + Python client).
3. **D1** — `@model_transform` closes the train → register loop.
4. **B2** — Promotion + stages + lineage.
5. **E1, E2, E3** — Base live deployments (single replica → autoscale).
6. **F1, F2** — Batch deployments + auto-eval (needs E1+).
7. **H1, H2, H3** — Model Functions + ontology binding + Workshop.
8. **I1, I2, N1, N2, N3, N4** — Modeling Objectives (closes governance + gated release).
9. **G1, G2, G3** — Feature Store offline+online+drift.
10. **J1, J2, J3** — Monitoring + shadow.
11. **E4, E5** — Advanced rollouts + externals.
12. **A3, K1, K2, K3** — LLMs + Marketplace + reproducibility.
13. **L1, M1** — Complete UI + parity suite.

Each block delivers independent, testable value.
