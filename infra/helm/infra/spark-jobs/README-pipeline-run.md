# `_pipeline-run-template.yaml` — pipeline-run SparkApplication template

`pipeline-build-service` (Rust) loads
[`templates/_pipeline-run-template.yaml`](./templates/_pipeline-run-template.yaml)
as a string, performs `${...}` substitution, and `POST`s the result to
the Kubernetes API as a `sparkoperator.k8s.io/v1beta2/SparkApplication`.
This is the **runtime** equivalent of the maintenance jobs in this
chart — one CR per pipeline run, replacing the Temporal `PipelineRun`
workflow + `ExecutePipeline` activity pair documented in
[`docs/architecture/refactor/pipeline-worker-inventory.md`](../../../../docs/architecture/refactor/pipeline-worker-inventory.md).

The file name starts with `_` so Helm treats it as a partial and skips
it during `helm install` / `helm template` — the chart still installs
cleanly while shipping the template alongside the maintenance jobs.
This README lives at the chart root rather than under `templates/` for
the same reason: any non-`_`-prefixed file inside `templates/` is
parsed by Helm.

## Why `${var}` and not Helm `{{ }}`

Helm renders templates at install time. Pipeline runs are created
at request time by `pipeline-build-service`, long after the chart is
installed. We need a substitution syntax that:

1. Survives `helm install` untouched (Helm will not interpret `${...}`).
2. Is trivially renderable from Rust without pulling in a Tera /
   Handlebars dependency — a single `String::replace` per variable.
3. Is recognisable as "not Helm" to anyone reading the file.

`${var}` (POSIX-shell-style) satisfies all three.

## Placeholder reference

| Placeholder                  | Type    | Example                                    | Notes                                                                                   |
|------------------------------|---------|--------------------------------------------|-----------------------------------------------------------------------------------------|
| `${pipeline_id}`             | string  | `ri.pipeline.main.7c1a`                    | Truncate so the full `pipeline-run-${pipeline_id}-${run_id}` is ≤ 50 chars.             |
| `${run_id}`                  | string  | `01HF9P3M5R`                               | ULID / short UUID suffix; 10–12 chars recommended.                                      |
| `${namespace}`               | string  | `openfoundry-spark`                        | Must be the namespace the Spark Operator watches. Default in prod: `openfoundry-spark`. |
| `${spark_application_type}`  | enum    | `Scala` \| `Python`                        | `Scala` for `sql`/`spark` nodes, `Python` for `pyspark` nodes.                          |
| `${pipeline_runner_image}`   | string  | `ghcr.io/unnamedlab/pipeline-runner:0.1.0` | Built in **Tarea 3.3**. Until 3.3 lands the Pod will fail to pull.                      |
| `${main_class}`              | string  | `com.openfoundry.pipeline.PipelineRunner`  | Set for `Scala`; the Rust generator omits the line for `Python`.                        |
| `${main_application_file}`   | string  | `local:///opt/spark/jars/pipeline-runner.jar` <br> or `local:///opt/spark/work-dir/pipeline_runner.py` | JAR for `Scala`, `.py` entrypoint for `Python`. Both are baked into the image. |
| `${input_dataset_rid}`       | string  | `ri.dataset.main.4abc`                     | Iceberg table reference resolved by `pipeline-runner` against Lakekeeper.               |
| `${output_dataset_rid}`      | string  | `ri.dataset.main.9def`                     | Same as above; output table is created or appended to.                                  |
| `${driver_cores}`            | int     | `1`                                        | Default `1`.                                                                            |
| `${driver_memory}`           | string  | `1g`                                       | Default `1g`. Use Spark size strings (`512m`, `2g`, …).                                 |
| `${executor_cores}`          | int     | `1`                                        | Default `1`.                                                                            |
| `${executor_instances}`      | int     | `2`                                        | Default `2`.                                                                            |
| `${executor_memory}`         | string  | `2g`                                       | Default `2g`.                                                                           |

### Fields that are NOT placeholders (deliberate)

| Field                                  | Why hard-coded                                                                                                                      |
|----------------------------------------|-------------------------------------------------------------------------------------------------------------------------------------|
| `spec.driver.serviceAccount`           | Always `spark-jobs-non-audit` — pipeline runs MUST NOT have audit-write permission (S5.1.c WORM, see [`../README.md`](../README.md)). |
| `spec.sparkConf` Iceberg / S3a entries | Catalog URI and Ceph RGW endpoint are infra-wide constants; promoting them to env vars only invites drift.                          |
| `spec.sparkVersion`                    | Pinned to match the `apache/spark` base image used by all jobs in this chart.                                                       |
| `spec.restartPolicy` / `timeToLiveSeconds` | Mirror the Temporal `ExecutePipeline` retry policy + 30 min start-to-close timeout one-to-one.                                  |

## Render example

The following `envsubst` invocation reproduces what
`pipeline-build-service` does at runtime, and is suitable for offline
validation with `kubectl --dry-run=client`:

```sh
export pipeline_id=demo-pipeline
export run_id=01HF9P3M5R
export namespace=openfoundry-spark
export spark_application_type=Scala
export pipeline_runner_image=ghcr.io/unnamedlab/pipeline-runner:0.1.0
export main_class=com.openfoundry.pipeline.PipelineRunner
export main_application_file=local:///opt/spark/jars/pipeline-runner.jar
export input_dataset_rid=ri.dataset.main.input
export output_dataset_rid=ri.dataset.main.output
export driver_cores=1
export driver_memory=1g
export executor_cores=1
export executor_instances=2
export executor_memory=2g

envsubst < infra/helm/infra/spark-jobs/templates/_pipeline-run-template.yaml \
  > /tmp/pipeline-run-rendered.yaml
```

Offline validation (no cluster required) — parse the YAML and assert
the structure:

```sh
python3 -c "
import yaml
d = yaml.safe_load(open('/tmp/pipeline-run-rendered.yaml'))
assert d['apiVersion'] == 'sparkoperator.k8s.io/v1beta2'
assert d['kind'] == 'SparkApplication'
assert d['spec']['driver']['serviceAccount'] == 'spark-jobs-non-audit'
print('OK', d['metadata']['name'])
"
```

Cluster validation (requires a kube-context with the Spark Operator
CRDs installed — `kubectl --dry-run=client` resolves the
`SparkApplication` kind via the cluster's discovery cache):

```sh
kubectl apply --dry-run=client -f /tmp/pipeline-run-rendered.yaml
# expected:
# sparkapplication.sparkoperator.k8s.io/pipeline-run-demo-pipeline-01HF9P3M5R created (dry run)
```

Note: `kubectl --dry-run=client` against an `SparkApplication` CR
still requires API discovery to resolve the custom kind — there is no
fully offline `kubectl` validation for a CRD-defined resource. The
Python YAML parse above is the offline equivalent.

## Failure modes

- The image referenced by `${pipeline_runner_image}` is built in
  **Tarea 3.3**. Until then, the Pod will fail with `ImagePullBackOff`
  and the operator will surface the failure on the `SparkApplication`
  status.
- The Spark Operator imposes a 63-char limit on derived Pod names. The
  Rust generator MUST truncate `${pipeline_id}`/`${run_id}` so that
  `pipeline-run-${pipeline_id}-${run_id}` is ≤ 50 chars before POST.
- `${spark_application_type: Python}` requires the generator to omit
  the `mainClass:` line entirely (rather than substituting an empty
  string) — `mainClass: ""` is rejected by the operator's CRD schema.
- Pipeline runs that hit `of_audit.*` will be denied at the K8s RBAC
  layer by `spark-jobs-non-audit` (and a `pipeline-build-service`
  preflight check should reject the request before submission).
