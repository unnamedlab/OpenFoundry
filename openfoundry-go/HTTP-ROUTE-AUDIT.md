# Rust vs Go HTTP route parity audit

Date: 2026-05-07

Generated with:

```sh
python3 tools/http_route_audit.py --write openfoundry-go/HTTP-ROUTE-AUDIT.md
```

State values:

- `implemented`: route exists in Rust and Go and the Go handler is not detected as a placeholder.
- `empty envelope`: Go route exists but returns a placeholder empty/list envelope.
- `501`: Go route exists but the handler advertises Not Implemented or equivalent pending behavior.
- `missing`: Rust route was not found in Go. A blank Rust handler (`—`) means the Go route was not found in the Rust route table (usually health/metrics aliases or newer Go-only surface).

This script is regex-based and optimized for the Axum/chi route declaration styles used in this repository; validate unusual dynamic route construction manually.

## pipeline-build-service

Rust routes: 24. Go routes: 52.
State counts: implemented: 52.

| Route | Method | Rust handler | Go handler | State |
| --- | --- | --- | --- | --- |
| `/api/v1/builds` | GET | — | `handler.ListBuilds`<br><sub>openfoundry-go/services/pipeline-build-service/internal/server/server.go:89</sub> | implemented |
| `/api/v1/builds` | POST | — | `handler.CreateBuild`<br><sub>openfoundry-go/services/pipeline-build-service/internal/server/server.go:90</sub> | implemented |
| `/api/v1/builds/{id}` | GET | — | `handler.GetBuild`<br><sub>openfoundry-go/services/pipeline-build-service/internal/server/server.go:91</sub> | implemented |
| `/api/v1/builds/{id}/abort` | POST | — | `handler.AbortBuild`<br><sub>openfoundry-go/services/pipeline-build-service/internal/server/server.go:92</sub> | implemented |
| `/api/v1/builds/{id}/jobs` | GET | — | `handler.ListJobs`<br><sub>openfoundry-go/services/pipeline-build-service/internal/server/server.go:93</sub> | implemented |
| `/api/v1/data-integration/builds` | GET | `handlers::builds::list_builds`<br><sub>services/pipeline-build-service/src/main.rs:135</sub> | `handler.ListDataIntegrationBuilds`<br><sub>openfoundry-go/services/pipeline-build-service/internal/server/server.go:125</sub> | implemented |
| `/api/v1/data-integration/builds/_summary` | GET | `handlers::builds::queue_summary`<br><sub>services/pipeline-build-service/src/main.rs:136</sub> | `handler.DataIntegrationQueueSummary`<br><sub>openfoundry-go/services/pipeline-build-service/internal/server/server.go:126</sub> | implemented |
| `/api/v1/data-integration/builds/{run_id}/abort` | POST | `handlers::builds::abort_build`<br><sub>services/pipeline-build-service/src/main.rs:137</sub> | `handler.AbortDataIntegrationBuild`<br><sub>openfoundry-go/services/pipeline-build-service/internal/server/server.go:127</sub> | implemented |
| `/api/v1/data-integration/pipelines/_scheduler/run-due` | POST | `handlers::execute::run_due_scheduled_pipelines`<br><sub>services/pipeline-build-service/src/main.rs:141</sub> | `handler.RunDueScheduledPipelines`<br><sub>openfoundry-go/services/pipeline-build-service/internal/server/server.go:128</sub> | implemented |
| `/api/v1/data-integration/pipelines/{pipeline_rid}/dry-run-resolve` | POST | `handlers::dry_run::dry_run_resolve`<br><sub>services/pipeline-build-service/src/main.rs:147</sub> | `handler.DryRunResolve`<br><sub>openfoundry-go/services/pipeline-build-service/internal/server/server.go:129</sub> | implemented |
| `/api/v1/data-integration/pipelines/{id}/runs` | GET | `handlers::runs::list_runs`<br><sub>services/pipeline-build-service/src/main.rs:123</sub> | `handler.ListPipelineRuns`<br><sub>openfoundry-go/services/pipeline-build-service/internal/server/server.go:121</sub> | implemented |
| `/api/v1/data-integration/pipelines/{id}/runs` | POST | `handlers::execute::trigger_run`<br><sub>services/pipeline-build-service/src/main.rs:123</sub> | `handler.TriggerPipelineRun`<br><sub>openfoundry-go/services/pipeline-build-service/internal/server/server.go:122</sub> | implemented |
| `/api/v1/data-integration/pipelines/{id}/runs/{run_id}` | GET | `handlers::runs::get_run`<br><sub>services/pipeline-build-service/src/main.rs:127</sub> | `handler.GetPipelineRun`<br><sub>openfoundry-go/services/pipeline-build-service/internal/server/server.go:123</sub> | implemented |
| `/api/v1/data-integration/pipelines/{id}/runs/{run_id}/retry` | POST | `handlers::execute::retry_run`<br><sub>services/pipeline-build-service/src/main.rs:131</sub> | `handler.RetryPipelineRun`<br><sub>openfoundry-go/services/pipeline-build-service/internal/server/server.go:124</sub> | implemented |
| `/api/v1/data-integration/pipelines/{id}/runs/{run_id}/spec` | GET | — | `handler.GetSpecForRun`<br><sub>openfoundry-go/services/pipeline-build-service/internal/server/server.go:134</sub> | implemented |
| `/api/v1/data-integration/spark-runs` | GET | — | `handler.ListSparkRuns`<br><sub>openfoundry-go/services/pipeline-build-service/internal/server/server.go:131</sub> | implemented |
| `/api/v1/data-integration/spark-runs` | POST | — | `handler.SubmitSparkRun`<br><sub>openfoundry-go/services/pipeline-build-service/internal/server/server.go:132</sub> | implemented |
| `/api/v1/data-integration/spark-runs/{id}` | GET | — | `handler.GetSparkRun`<br><sub>openfoundry-go/services/pipeline-build-service/internal/server/server.go:133</sub> | implemented |
| `/api/v1/dry-run/resolve` | POST | — | `handler.DryRunResolve`<br><sub>openfoundry-go/services/pipeline-build-service/internal/server/server.go:101</sub> | implemented |
| `/api/v1/dry-run/validate` | POST | — | `handler.DryRunValidate`<br><sub>openfoundry-go/services/pipeline-build-service/internal/server/server.go:102</sub> | implemented |
| `/api/v1/execute` | POST | — | `handler.ExecutePipeline`<br><sub>openfoundry-go/services/pipeline-build-service/internal/server/server.go:103</sub> | implemented |
| `/api/v1/jobs/{id}` | GET | — | `handler.GetJob`<br><sub>openfoundry-go/services/pipeline-build-service/internal/server/server.go:96</sub> | implemented |
| `/api/v1/jobs/{id}/logs` | GET | — | `handler.ListJobLogs`<br><sub>openfoundry-go/services/pipeline-build-service/internal/server/server.go:97</sub> | implemented |
| `/api/v1/jobs/{id}/logs/stream` | GET | — | `handler.StreamJobLogs`<br><sub>openfoundry-go/services/pipeline-build-service/internal/server/server.go:98</sub> | implemented |
| `/api/v1/pipeline/builds/run` | POST | `handlers::spark_runs::submit_pipeline_run`<br><sub>services/pipeline-build-service/src/main.rs:205</sub> | `handler.SubmitPipelineBuildRun`<br><sub>openfoundry-go/services/pipeline-build-service/internal/server/server.go:81</sub> | implemented |
| `/api/v1/pipeline/builds/{run_id}/status` | GET | `handlers::spark_runs::get_pipeline_run_status`<br><sub>services/pipeline-build-service/src/main.rs:209</sub> | `handler.GetPipelineBuildRunStatus`<br><sub>openfoundry-go/services/pipeline-build-service/internal/server/server.go:82</sub> | implemented |
| `/api/v1/pipelines` | GET | — | `handler.ListPipelines`<br><sub>openfoundry-go/services/pipeline-build-service/internal/server/server.go:106</sub> | implemented |
| `/api/v1/pipelines` | POST | — | `handler.CreatePipeline`<br><sub>openfoundry-go/services/pipeline-build-service/internal/server/server.go:107</sub> | implemented |
| `/api/v1/pipelines/{id}` | DELETE | — | `handler.DeletePipeline`<br><sub>openfoundry-go/services/pipeline-build-service/internal/server/server.go:111</sub> | implemented |
| `/api/v1/pipelines/{id}` | GET | — | `handler.GetPipeline`<br><sub>openfoundry-go/services/pipeline-build-service/internal/server/server.go:108</sub> | implemented |
| `/api/v1/pipelines/{id}` | PATCH | — | `handler.UpdatePipeline`<br><sub>openfoundry-go/services/pipeline-build-service/internal/server/server.go:109</sub> | implemented |
| `/api/v1/pipelines/{id}` | PUT | — | `handler.UpdatePipeline`<br><sub>openfoundry-go/services/pipeline-build-service/internal/server/server.go:110</sub> | implemented |
| `/api/v1/pipelines/{id}/runs` | GET | — | `handler.ListPipelineRuns`<br><sub>openfoundry-go/services/pipeline-build-service/internal/server/server.go:113</sub> | implemented |
| `/api/v1/pipelines/{id}/runs` | POST | — | `handler.TriggerPipelineRun`<br><sub>openfoundry-go/services/pipeline-build-service/internal/server/server.go:114</sub> | implemented |
| `/api/v1/pipelines/{id}/runs/{run_id}` | GET | — | `handler.GetPipelineRun`<br><sub>openfoundry-go/services/pipeline-build-service/internal/server/server.go:115</sub> | implemented |
| `/api/v1/pipelines/{id}/runs/{run_id}/cancel` | POST | — | `handler.CancelPipelineRun`<br><sub>openfoundry-go/services/pipeline-build-service/internal/server/server.go:117</sub> | implemented |
| `/api/v1/pipelines/{id}/runs/{run_id}/retry` | POST | — | `handler.RetryPipelineRun`<br><sub>openfoundry-go/services/pipeline-build-service/internal/server/server.go:116</sub> | implemented |
| `/health` | GET | — | `func(w`<br><sub>openfoundry-go/services/pipeline-build-service/internal/server/server.go:68</sub> | implemented |
| `/healthz` | GET | `||`<br><sub>services/pipeline-build-service/src/main.rs:218</sub> | `func(w`<br><sub>openfoundry-go/services/pipeline-build-service/internal/server/server.go:64</sub> | implemented |
| `/metrics` | GET | — | `m.Handler(`<br><sub>openfoundry-go/services/pipeline-build-service/internal/server/server.go:73</sub> | implemented |
| `/v1/builds` | GET | `handlers::builds_v1::list_builds_v1`<br><sub>services/pipeline-build-service/src/main.rs:157</sub> | `handler.ListBuildsV1`<br><sub>openfoundry-go/services/pipeline-build-service/internal/server/server.go:140</sub> | implemented |
| `/v1/builds` | POST | `handlers::builds_v1::create_build`<br><sub>services/pipeline-build-service/src/main.rs:157</sub> | `handler.CreateBuildV1`<br><sub>openfoundry-go/services/pipeline-build-service/internal/server/server.go:141</sub> | implemented |
| `/v1/builds/{rid}` | GET | `handlers::builds_v1::get_build`<br><sub>services/pipeline-build-service/src/main.rs:161</sub> | `handler.GetBuildV1`<br><sub>openfoundry-go/services/pipeline-build-service/internal/server/server.go:142</sub> | implemented |
| `/v1/builds/{rid}:abort` | POST | `handlers::builds_v1::abort_build_v1`<br><sub>services/pipeline-build-service/src/main.rs:162</sub> | `handler.AbortBuildV1`<br><sub>openfoundry-go/services/pipeline-build-service/internal/server/server.go:143</sub> | implemented |
| `/v1/datasets/{rid}/builds` | GET | `handlers::builds_v1::list_dataset_builds`<br><sub>services/pipeline-build-service/src/main.rs:166</sub> | `handler.ListDatasetBuildsV1`<br><sub>openfoundry-go/services/pipeline-build-service/internal/server/server.go:144</sub> | implemented |
| `/v1/job-specs/{kind}` | POST | `handlers::builds_v1::create_job_spec`<br><sub>services/pipeline-build-service/src/main.rs:178</sub> | `handler.CreateJobSpecV1`<br><sub>openfoundry-go/services/pipeline-build-service/internal/server/server.go:147</sub> | implemented |
| `/v1/jobs/{rid}/input-resolutions` | GET | `handlers::builds_v1::get_job_input_resolutions`<br><sub>services/pipeline-build-service/src/main.rs:174</sub> | `handler.GetJobInputResolutionsV1`<br><sub>openfoundry-go/services/pipeline-build-service/internal/server/server.go:146</sub> | implemented |
| `/v1/jobs/{rid}/logs` | GET | `handlers::job_logs::list_logs`<br><sub>services/pipeline-build-service/src/main.rs:183</sub> | `handler.ListJobLogsV1`<br><sub>openfoundry-go/services/pipeline-build-service/internal/server/server.go:148</sub> | implemented |
| `/v1/jobs/{rid}/logs` | POST | `handlers::job_logs::emit_log`<br><sub>services/pipeline-build-service/src/main.rs:183</sub> | `handler.EmitJobLogV1`<br><sub>openfoundry-go/services/pipeline-build-service/internal/server/server.go:149</sub> | implemented |
| `/v1/jobs/{rid}/logs/stream` | GET | `handlers::job_logs::stream_logs`<br><sub>services/pipeline-build-service/src/main.rs:187</sub> | `handler.StreamJobLogsV1`<br><sub>openfoundry-go/services/pipeline-build-service/internal/server/server.go:150</sub> | implemented |
| `/v1/jobs/{rid}/logs/ws` | GET | `handlers::job_logs::ws_logs`<br><sub>services/pipeline-build-service/src/main.rs:191</sub> | `handler.WSJobLogsV1`<br><sub>openfoundry-go/services/pipeline-build-service/internal/server/server.go:151</sub> | implemented |
| `/v1/jobs/{rid}/outputs` | GET | `handlers::builds_v1::get_job_outputs`<br><sub>services/pipeline-build-service/src/main.rs:170</sub> | `handler.GetJobOutputsV1`<br><sub>openfoundry-go/services/pipeline-build-service/internal/server/server.go:145</sub> | implemented |

## notebook-runtime-service

Rust routes: 0. Go routes: 51.
State counts: implemented: 28.

| Route | Method | Rust handler | Go handler | State |
| --- | --- | --- | --- | --- |
| `/api/v1/notebooks` | GET | — | `state.ListNotebooks`<br><sub>openfoundry-go/services/notebook-runtime-service/internal/server/server.go:66</sub> | implemented |
| `/api/v1/notebooks` | POST | — | `state.CreateNotebook`<br><sub>openfoundry-go/services/notebook-runtime-service/internal/server/server.go:67</sub> | implemented |
| `/api/v1/notebooks/{notebook_id}` | DELETE | — | `state.DeleteNotebook`<br><sub>openfoundry-go/services/notebook-runtime-service/internal/server/server.go:71</sub> | implemented |
| `/api/v1/notebooks/{notebook_id}` | GET | — | `state.GetNotebook`<br><sub>openfoundry-go/services/notebook-runtime-service/internal/server/server.go:68</sub> | implemented |
| `/api/v1/notebooks/{notebook_id}` | PATCH | — | `state.UpdateNotebook`<br><sub>openfoundry-go/services/notebook-runtime-service/internal/server/server.go:70</sub> | implemented |
| `/api/v1/notebooks/{notebook_id}` | PUT | — | `state.UpdateNotebook`<br><sub>openfoundry-go/services/notebook-runtime-service/internal/server/server.go:69</sub> | implemented |
| `/api/v1/notebooks/{notebook_id}/cells` | POST | — | `state.AddCell`<br><sub>openfoundry-go/services/notebook-runtime-service/internal/server/server.go:74</sub> | implemented |
| `/api/v1/notebooks/{notebook_id}/cells/execute-all` | POST | — | `state.ExecuteAllCells`<br><sub>openfoundry-go/services/notebook-runtime-service/internal/server/server.go:85</sub> | implemented |
| `/api/v1/notebooks/{notebook_id}/cells/{cell_id}` | DELETE | — | `state.DeleteCell`<br><sub>openfoundry-go/services/notebook-runtime-service/internal/server/server.go:76</sub> | implemented |
| `/api/v1/notebooks/{notebook_id}/cells/{cell_id}` | PATCH | — | `state.UpdateCell`<br><sub>openfoundry-go/services/notebook-runtime-service/internal/server/server.go:75</sub> | implemented |
| `/api/v1/notebooks/{notebook_id}/cells/{cell_id}/execute` | POST | — | `state.ExecuteCell`<br><sub>openfoundry-go/services/notebook-runtime-service/internal/server/server.go:84</sub> | implemented |
| `/api/v1/notebooks/{notebook_id}/sessions` | GET | — | `state.ListSessions`<br><sub>openfoundry-go/services/notebook-runtime-service/internal/server/server.go:79</sub> | implemented |
| `/api/v1/notebooks/{notebook_id}/sessions` | POST | — | `state.CreateSession`<br><sub>openfoundry-go/services/notebook-runtime-service/internal/server/server.go:80</sub> | implemented |
| `/api/v1/notebooks/{notebook_id}/sessions/{session_id}/stop` | POST | — | `state.StopSession`<br><sub>openfoundry-go/services/notebook-runtime-service/internal/server/server.go:81</sub> | implemented |
| `/api/v1/notebooks/{notebook_id}/workspace` | DELETE | — | `state.DeleteWorkspaceFile`<br><sub>openfoundry-go/services/notebook-runtime-service/internal/server/server.go:90</sub> | implemented |
| `/api/v1/notebooks/{notebook_id}/workspace` | GET | — | `state.ListWorkspaceFiles`<br><sub>openfoundry-go/services/notebook-runtime-service/internal/server/server.go:88</sub> | implemented |
| `/api/v1/notebooks/{notebook_id}/workspace` | PUT | — | `state.UpsertWorkspaceFile`<br><sub>openfoundry-go/services/notebook-runtime-service/internal/server/server.go:89</sub> | implemented |
| `/api/v1/notepad/documents` | GET | — | `state.ListDocuments`<br><sub>openfoundry-go/services/notebook-runtime-service/internal/server/server.go:93</sub> | implemented |
| `/api/v1/notepad/documents` | POST | — | `state.CreateDocument`<br><sub>openfoundry-go/services/notebook-runtime-service/internal/server/server.go:94</sub> | implemented |
| `/api/v1/notepad/documents/{document_id}` | DELETE | — | `state.DeleteDocument`<br><sub>openfoundry-go/services/notebook-runtime-service/internal/server/server.go:97</sub> | implemented |
| `/api/v1/notepad/documents/{document_id}` | GET | — | `state.GetDocument`<br><sub>openfoundry-go/services/notebook-runtime-service/internal/server/server.go:95</sub> | implemented |
| `/api/v1/notepad/documents/{document_id}` | PATCH | — | `state.UpdateDocument`<br><sub>openfoundry-go/services/notebook-runtime-service/internal/server/server.go:96</sub> | implemented |
| `/api/v1/notepad/documents/{document_id}/export` | POST | — | `state.ExportDocument`<br><sub>openfoundry-go/services/notebook-runtime-service/internal/server/server.go:100</sub> | implemented |
| `/api/v1/notepad/documents/{document_id}/presence` | GET | — | `state.ListPresence`<br><sub>openfoundry-go/services/notebook-runtime-service/internal/server/server.go:98</sub> | implemented |
| `/api/v1/notepad/documents/{document_id}/presence` | POST | — | `state.UpsertPresence`<br><sub>openfoundry-go/services/notebook-runtime-service/internal/server/server.go:99</sub> | implemented |
| `/health` | GET | — | `func(w`<br><sub>openfoundry-go/services/notebook-runtime-service/internal/server/server.go:53</sub> | implemented |
| `/healthz` | GET | — | `func(w`<br><sub>openfoundry-go/services/notebook-runtime-service/internal/server/server.go:49</sub> | implemented |
| `/metrics` | GET | — | `m.Handler(`<br><sub>openfoundry-go/services/notebook-runtime-service/internal/server/server.go:58</sub> | implemented |

## ontology-query-service

Rust routes: 3. Go routes: 4.
State counts: implemented: 4, missing: 1.

| Route | Method | Rust handler | Go handler | State |
| --- | --- | --- | --- | --- |
| `/api/v1/ontology/objects/{tenant}/by-type/{type_id}` | GET | `handlers::list_objects_by_type`<br><sub>services/ontology-query-service/src/lib.rs:42</sub> | `h.ListObjectsByType`<br><sub>openfoundry-go/services/ontology-query-service/internal/server/server.go:38</sub> | implemented |
| `/api/v1/ontology/objects/{tenant}/{object_id}` | GET | `handlers::get_object`<br><sub>services/ontology-query-service/src/lib.rs:41</sub> | `h.GetObject`<br><sub>openfoundry-go/services/ontology-query-service/internal/server/server.go:37</sub> | implemented |
| `/health` | GET | `||`<br><sub>services/ontology-query-service/src/main.rs:79</sub> | — | missing |
| `/healthz` | GET | — | `func(w`<br><sub>openfoundry-go/services/ontology-query-service/internal/server/server.go:28</sub> | implemented |
| `/metrics` | GET | — | `m.Handler(`<br><sub>openfoundry-go/services/ontology-query-service/internal/server/server.go:32</sub> | implemented |

## connector-management-service

Rust routes: 47. Go routes: 60.
State counts: 501: 5, empty envelope: 4, implemented: 50, missing: 40.

| Route | Method | Rust handler | Go handler | State |
| --- | --- | --- | --- | --- |
| `/api/v1/auth/bootstrap-status` | GET | — | `h.DevAuthBootstrapStatus`<br><sub>openfoundry-go/services/connector-management-service/internal/server/server.go:112</sub> | 501 |
| `/api/v1/auth/login` | POST | — | `h.DevAuthLogin`<br><sub>openfoundry-go/services/connector-management-service/internal/server/server.go:110</sub> | 501 |
| `/api/v1/auth/refresh` | POST | — | `h.DevAuthRefresh`<br><sub>openfoundry-go/services/connector-management-service/internal/server/server.go:111</sub> | 501 |
| `/api/v1/connections` | GET | — | `h.ListConnections`<br><sub>openfoundry-go/services/connector-management-service/internal/server/server.go:100</sub> | implemented |
| `/api/v1/connections` | POST | — | `h.CreateConnection`<br><sub>openfoundry-go/services/connector-management-service/internal/server/server.go:101</sub> | implemented |
| `/api/v1/connections/{id}` | DELETE | — | `h.DeleteConnection`<br><sub>openfoundry-go/services/connector-management-service/internal/server/server.go:104</sub> | implemented |
| `/api/v1/connections/{id}` | GET | — | `h.GetConnection`<br><sub>openfoundry-go/services/connector-management-service/internal/server/server.go:102</sub> | implemented |
| `/api/v1/connections/{id}` | PATCH | — | `h.UpdateConnection`<br><sub>openfoundry-go/services/connector-management-service/internal/server/server.go:103</sub> | implemented |
| `/api/v1/connections/{id}/test` | POST | — | `h.TestConnection`<br><sub>openfoundry-go/services/connector-management-service/internal/server/server.go:105</sub> | implemented |
| `/api/v1/data-connection/catalog` | GET | — | `h.GetConnectorCatalog`<br><sub>openfoundry-go/services/connector-management-service/internal/server/server.go:59</sub> | implemented |
| `/api/v1/data-connection/catalog/contracts` | GET | — | `h.GetConnectorContracts`<br><sub>openfoundry-go/services/connector-management-service/internal/server/server.go:60</sub> | implemented |
| `/api/v1/data-connection/media-set-syncs/{id}` | GET | — | `h.GetMediaSetSync`<br><sub>openfoundry-go/services/connector-management-service/internal/server/server.go:85</sub> | implemented |
| `/api/v1/data-connection/media-set-syncs/{id}` | PATCH | — | `h.UpdateMediaSetSync`<br><sub>openfoundry-go/services/connector-management-service/internal/server/server.go:86</sub> | implemented |
| `/api/v1/data-connection/media-set-syncs/{id}/run` | POST | — | `h.RunMediaSetSync`<br><sub>openfoundry-go/services/connector-management-service/internal/server/server.go:87</sub> | implemented |
| `/api/v1/data-connection/sources` | GET | — | `h.ListConnections`<br><sub>openfoundry-go/services/connector-management-service/internal/server/server.go:64</sub> | implemented |
| `/api/v1/data-connection/sources` | POST | — | `h.CreateConnection`<br><sub>openfoundry-go/services/connector-management-service/internal/server/server.go:65</sub> | implemented |
| `/api/v1/data-connection/sources/{id}` | DELETE | — | `h.DeleteConnection`<br><sub>openfoundry-go/services/connector-management-service/internal/server/server.go:67</sub> | implemented |
| `/api/v1/data-connection/sources/{id}` | GET | — | `h.GetConnection`<br><sub>openfoundry-go/services/connector-management-service/internal/server/server.go:66</sub> | implemented |
| `/api/v1/data-connection/sources/{id}/capabilities` | GET | — | `h.GetConnectionCapabilities`<br><sub>openfoundry-go/services/connector-management-service/internal/server/server.go:69</sub> | 501 |
| `/api/v1/data-connection/sources/{id}/credentials` | GET | — | `h.ListCredentials`<br><sub>openfoundry-go/services/connector-management-service/internal/server/server.go:70</sub> | implemented |
| `/api/v1/data-connection/sources/{id}/credentials` | POST | — | `h.SetCredential`<br><sub>openfoundry-go/services/connector-management-service/internal/server/server.go:71</sub> | implemented |
| `/api/v1/data-connection/sources/{id}/egress-policies` | GET | — | `h.ListSourcePolicies`<br><sub>openfoundry-go/services/connector-management-service/internal/server/server.go:72</sub> | implemented |
| `/api/v1/data-connection/sources/{id}/egress-policies` | POST | — | `h.AttachPolicy`<br><sub>openfoundry-go/services/connector-management-service/internal/server/server.go:73</sub> | implemented |
| `/api/v1/data-connection/sources/{source_id}/egress-policies/{policy_id}` | DELETE | — | `h.DetachPolicy`<br><sub>openfoundry-go/services/connector-management-service/internal/server/server.go:74</sub> | implemented |
| `/api/v1/data-connection/sources/{id}/media-set-syncs` | GET | — | `h.ListMediaSetSyncs`<br><sub>openfoundry-go/services/connector-management-service/internal/server/server.go:83</sub> | implemented |
| `/api/v1/data-connection/sources/{id}/media-set-syncs` | POST | — | `h.CreateMediaSetSync`<br><sub>openfoundry-go/services/connector-management-service/internal/server/server.go:84</sub> | implemented |
| `/api/v1/data-connection/sources/{id}/registrations` | GET | — | `h.ListRegistrations`<br><sub>openfoundry-go/services/connector-management-service/internal/server/server.go:89</sub> | implemented |
| `/api/v1/data-connection/sources/{id}/registrations/auto` | POST | — | `h.AutoRegister`<br><sub>openfoundry-go/services/connector-management-service/internal/server/server.go:93</sub> | empty envelope |
| `/api/v1/data-connection/sources/{id}/registrations/auto` | PUT | — | `h.UpdateAutoRegistration`<br><sub>openfoundry-go/services/connector-management-service/internal/server/server.go:94</sub> | implemented |
| `/api/v1/data-connection/sources/{id}/registrations/auto/status` | GET | — | `h.AutoRegisterStatus`<br><sub>openfoundry-go/services/connector-management-service/internal/server/server.go:95</sub> | implemented |
| `/api/v1/data-connection/sources/{id}/registrations/bulk` | POST | — | `h.BulkRegister`<br><sub>openfoundry-go/services/connector-management-service/internal/server/server.go:91</sub> | empty envelope |
| `/api/v1/data-connection/sources/{id}/registrations/bulk/preview` | POST | — | `h.BulkRegisterPreview`<br><sub>openfoundry-go/services/connector-management-service/internal/server/server.go:92</sub> | empty envelope |
| `/api/v1/data-connection/sources/{id}/registrations/discover` | POST | — | `h.DiscoverRegistrations`<br><sub>openfoundry-go/services/connector-management-service/internal/server/server.go:90</sub> | implemented |
| `/api/v1/data-connection/sources/{source_id}/registrations/{registration_id}` | DELETE | — | `h.DeleteRegistration`<br><sub>openfoundry-go/services/connector-management-service/internal/server/server.go:96</sub> | implemented |
| `/api/v1/data-connection/sources/{source_id}/registrations/{registration_id}/query` | POST | — | `h.QueryRegistration`<br><sub>openfoundry-go/services/connector-management-service/internal/server/server.go:97</sub> | implemented |
| `/api/v1/data-connection/sources/{source_id}/registrations/{registration_id}/query/arrow` | POST | — | `h.QueryRegistrationArrow`<br><sub>openfoundry-go/services/connector-management-service/internal/server/server.go:98</sub> | implemented |
| `/api/v1/data-connection/sources/{id}/syncs` | GET | — | `h.ListSyncJobs`<br><sub>openfoundry-go/services/connector-management-service/internal/server/server.go:76</sub> | implemented |
| `/api/v1/data-connection/sources/{id}/test-connection` | POST | — | `h.TestConnection`<br><sub>openfoundry-go/services/connector-management-service/internal/server/server.go:68</sub> | implemented |
| `/api/v1/data-connection/streaming-sources` | GET | — | `h.ListStreamingSources`<br><sub>openfoundry-go/services/connector-management-service/internal/server/server.go:61</sub> | implemented |
| `/api/v1/data-connection/syncs` | POST | — | `h.CreateSyncJob`<br><sub>openfoundry-go/services/connector-management-service/internal/server/server.go:77</sub> | implemented |
| `/api/v1/data-connection/syncs/{id}` | GET | — | `h.GetSyncJob`<br><sub>openfoundry-go/services/connector-management-service/internal/server/server.go:78</sub> | implemented |
| `/api/v1/data-connection/syncs/{id}` | PATCH | — | `h.UpdateSyncJob`<br><sub>openfoundry-go/services/connector-management-service/internal/server/server.go:79</sub> | implemented |
| `/api/v1/data-connection/syncs/{id}/run` | POST | — | `h.RunSyncJob`<br><sub>openfoundry-go/services/connector-management-service/internal/server/server.go:80</sub> | implemented |
| `/api/v1/data-connection/syncs/{id}/runs` | GET | — | `h.ListRuns`<br><sub>openfoundry-go/services/connector-management-service/internal/server/server.go:81</sub> | implemented |
| `/api/v1/users/me` | GET | — | `h.DevAuthMe`<br><sub>openfoundry-go/services/connector-management-service/internal/server/server.go:113</sub> | 501 |
| `/api/v1/virtual-table/sources/{source_rid}/enable` | POST | — | `h.EnableVirtualTableSource`<br><sub>openfoundry-go/services/connector-management-service/internal/server/server.go:116</sub> | implemented |
| `/api/v1/virtual-table/sources/{source_rid}/virtual-tables` | POST | — | `h.CreateVirtualTable`<br><sub>openfoundry-go/services/connector-management-service/internal/server/server.go:117</sub> | implemented |
| `/api/v1/virtual-tables` | GET | — | `h.ListVirtualTables`<br><sub>openfoundry-go/services/connector-management-service/internal/server/server.go:118</sub> | implemented |
| `/api/v1/virtual-tables/{rid}` | GET | — | `h.GetVirtualTable`<br><sub>openfoundry-go/services/connector-management-service/internal/server/server.go:119</sub> | implemented |
| `/api/v1/webhooks/{id}/invoke` | POST | — | `h.InvokeWebhook`<br><sub>openfoundry-go/services/connector-management-service/internal/server/server.go:107</sub> | implemented |
| `/auth/bootstrap-status` | GET | `handlers::dev_auth::bootstrap_status`<br><sub>services/connector-management-service/src/main.rs:281</sub> | — | missing |
| `/auth/login` | POST | `handlers::dev_auth::login`<br><sub>services/connector-management-service/src/main.rs:279</sub> | — | missing |
| `/auth/refresh` | POST | `handlers::dev_auth::refresh`<br><sub>services/connector-management-service/src/main.rs:280</sub> | — | missing |
| `/connections` | GET | `handlers::connections::list_connections`<br><sub>services/connector-management-service/src/main.rs:244</sub> | — | missing |
| `/connections` | POST | `handlers::connections::create_connection`<br><sub>services/connector-management-service/src/main.rs:244</sub> | — | missing |
| `/connections/{id}` | DELETE | `handlers::connections::delete_connection`<br><sub>services/connector-management-service/src/main.rs:249</sub> | — | missing |
| `/connections/{id}` | GET | `handlers::connections::get_connection`<br><sub>services/connector-management-service/src/main.rs:249</sub> | — | missing |
| `/connections/{id}/test` | POST | `handlers::connections::test_connection`<br><sub>services/connector-management-service/src/main.rs:254</sub> | — | missing |
| `/data-connection/catalog` | GET | `handlers::catalog::get_connector_catalog`<br><sub>services/connector-management-service/src/main.rs:113</sub> | — | missing |
| `/data-connection/catalog/contracts` | GET | `handlers::catalog::get_connector_contracts`<br><sub>services/connector-management-service/src/main.rs:114</sub> | — | missing |
| `/data-connection/sources` | GET | `handlers::connections::list_connections`<br><sub>services/connector-management-service/src/main.rs:123</sub> | — | missing |
| `/data-connection/sources` | POST | `handlers::connections::create_connection`<br><sub>services/connector-management-service/src/main.rs:123</sub> | — | missing |
| `/data-connection/sources/{id}` | DELETE | `handlers::connections::delete_connection`<br><sub>services/connector-management-service/src/main.rs:128</sub> | — | missing |
| `/data-connection/sources/{id}` | GET | `handlers::connections::get_connection`<br><sub>services/connector-management-service/src/main.rs:128</sub> | — | missing |
| `/data-connection/sources/{id}/capabilities` | GET | `handlers::catalog::get_connection_capabilities`<br><sub>services/connector-management-service/src/main.rs:137</sub> | — | missing |
| `/data-connection/sources/{id}/credentials` | GET | `handlers::data_connection::list_credentials`<br><sub>services/connector-management-service/src/main.rs:141</sub> | — | missing |
| `/data-connection/sources/{id}/credentials` | POST | `handlers::data_connection::set_credential`<br><sub>services/connector-management-service/src/main.rs:141</sub> | — | missing |
| `/data-connection/sources/{id}/egress-policies` | GET | `handlers::data_connection::list_source_policies`<br><sub>services/connector-management-service/src/main.rs:146</sub> | — | missing |
| `/data-connection/sources/{id}/egress-policies` | POST | `handlers::data_connection::attach_policy`<br><sub>services/connector-management-service/src/main.rs:146</sub> | — | missing |
| `/data-connection/sources/{source_id}/egress-policies/{policy_id}` | DELETE | `handlers::data_connection::detach_policy`<br><sub>services/connector-management-service/src/main.rs:151</sub> | — | missing |
| `/data-connection/sources/{id}/media-set-syncs` | GET | `handlers::media_set_syncs::list_media_set_syncs`<br><sub>services/connector-management-service/src/main.rs:166</sub> | — | missing |
| `/data-connection/sources/{id}/media-set-syncs` | POST | `handlers::media_set_syncs::create_media_set_sync`<br><sub>services/connector-management-service/src/main.rs:166</sub> | — | missing |
| `/data-connection/sources/{id}/registrations` | GET | `handlers::registrations::list_registrations`<br><sub>services/connector-management-service/src/main.rs:175</sub> | — | missing |
| `/data-connection/sources/{id}/registrations/auto` | POST | `handlers::registrations::auto_register`<br><sub>services/connector-management-service/src/main.rs:191</sub> | — | missing |
| `/data-connection/sources/{id}/registrations/auto` | PUT | `handlers::registrations::update_auto_registration`<br><sub>services/connector-management-service/src/main.rs:195</sub> | — | missing |
| `/data-connection/sources/{id}/registrations/auto/status` | GET | `handlers::registrations::auto_register_status`<br><sub>services/connector-management-service/src/main.rs:199</sub> | — | missing |
| `/data-connection/sources/{id}/registrations/bulk` | POST | `handlers::registrations::bulk_register`<br><sub>services/connector-management-service/src/main.rs:183</sub> | — | missing |
| `/data-connection/sources/{id}/registrations/bulk/preview` | POST | `handlers::registrations::bulk_register_preview`<br><sub>services/connector-management-service/src/main.rs:187</sub> | — | missing |
| `/data-connection/sources/{id}/registrations/discover` | POST | `handlers::registrations::discover`<br><sub>services/connector-management-service/src/main.rs:179</sub> | — | missing |
| `/data-connection/sources/{source_id}/registrations/{registration_id}` | DELETE | `handlers::registrations::delete_registration`<br><sub>services/connector-management-service/src/main.rs:203</sub> | — | missing |
| `/data-connection/sources/{source_id}/registrations/{registration_id}/query` | POST | `handlers::registrations::query_registration`<br><sub>services/connector-management-service/src/main.rs:207</sub> | — | missing |
| `/data-connection/sources/{source_id}/registrations/{registration_id}/query/arrow` | POST | `handlers::registrations::query_registration_arrow`<br><sub>services/connector-management-service/src/main.rs:211</sub> | — | missing |
| `/data-connection/sources/{id}/syncs` | GET | `handlers::data_connection::list_syncs`<br><sub>services/connector-management-service/src/main.rs:155</sub> | — | missing |
| `/data-connection/sources/{id}/test-connection` | POST | `handlers::connections::test_connection`<br><sub>services/connector-management-service/src/main.rs:133</sub> | — | missing |
| `/data-connection/streaming-sources` | GET | `handlers::streaming_syncs::list_streaming_sources`<br><sub>services/connector-management-service/src/main.rs:119</sub> | — | missing |
| `/data-connection/syncs` | POST | `handlers::data_connection::create_sync`<br><sub>services/connector-management-service/src/main.rs:159</sub> | — | missing |
| `/data-connection/syncs/{id}/run` | POST | `handlers::data_connection::run_sync`<br><sub>services/connector-management-service/src/main.rs:160</sub> | — | missing |
| `/data-connection/syncs/{id}/runs` | GET | `handlers::data_connection::list_runs`<br><sub>services/connector-management-service/src/main.rs:161</sub> | — | missing |
| `/health` | GET | `||`<br><sub>services/connector-management-service/src/main.rs:294</sub> | `func(w`<br><sub>openfoundry-go/services/connector-management-service/internal/server/server.go:34</sub> | implemented |
| `/healthz` | GET | — | `func(w`<br><sub>openfoundry-go/services/connector-management-service/internal/server/server.go:30</sub> | implemented |
| `/iceberg/v1/config` | GET | `handlers::iceberg_catalog::get_config`<br><sub>services/connector-management-service/src/main.rs:223</sub> | `h.IcebergGetConfig`<br><sub>openfoundry-go/services/connector-management-service/internal/server/server.go:124</sub> | implemented |
| `/iceberg/v1/namespaces` | GET | `handlers::iceberg_catalog::list_namespaces`<br><sub>services/connector-management-service/src/main.rs:224</sub> | `h.IcebergListNamespaces`<br><sub>openfoundry-go/services/connector-management-service/internal/server/server.go:125</sub> | implemented |
| `/iceberg/v1/namespaces/{namespace}` | GET | `handlers::iceberg_catalog::get_namespace`<br><sub>services/connector-management-service/src/main.rs:228</sub> | `h.IcebergGetNamespace`<br><sub>openfoundry-go/services/connector-management-service/internal/server/server.go:126</sub> | implemented |
| `/iceberg/v1/namespaces/{namespace}/tables` | GET | `handlers::iceberg_catalog::list_tables`<br><sub>services/connector-management-service/src/main.rs:232</sub> | `h.IcebergListTables`<br><sub>openfoundry-go/services/connector-management-service/internal/server/server.go:127</sub> | implemented |
| `/iceberg/v1/namespaces/{namespace}/tables/{table}` | GET | `handlers::iceberg_catalog::load_table`<br><sub>services/connector-management-service/src/main.rs:236</sub> | `h.IcebergLoadTable`<br><sub>openfoundry-go/services/connector-management-service/internal/server/server.go:128</sub> | empty envelope |
| `/metrics` | GET | `metrics_handler`<br><sub>services/connector-management-service/src/main.rs:295</sub> | `m.Handler(`<br><sub>openfoundry-go/services/connector-management-service/internal/server/server.go:53</sub> | implemented |
| `/readyz` | GET | — | `func(w`<br><sub>openfoundry-go/services/connector-management-service/internal/server/server.go:38</sub> | implemented |
| `/users/me` | GET | `handlers::dev_auth::me`<br><sub>services/connector-management-service/src/main.rs:285</sub> | — | missing |
| `/webhooks/{id}/invoke` | POST | `handlers::webhooks::invoke_webhook`<br><sub>services/connector-management-service/src/main.rs:264</sub> | — | missing |

## dataset-versioning-service

Rust routes: 78. Go routes: 87.
State counts: implemented: 87.

| Route | Method | Rust handler | Go handler | State |
| --- | --- | --- | --- | --- |
| `/api/v1/datasets` | GET | — | `h.ListDatasets`<br><sub>openfoundry-go/services/dataset-versioning-service/internal/server/server.go:42</sub> | implemented |
| `/api/v1/datasets` | POST | — | `h.CreateDataset`<br><sub>openfoundry-go/services/dataset-versioning-service/internal/server/server.go:43</sub> | implemented |
| `/api/v1/datasets/{id}` | DELETE | — | `h.DeleteDataset`<br><sub>openfoundry-go/services/dataset-versioning-service/internal/server/server.go:46</sub> | implemented |
| `/api/v1/datasets/{id}` | GET | — | `h.GetDataset`<br><sub>openfoundry-go/services/dataset-versioning-service/internal/server/server.go:44</sub> | implemented |
| `/api/v1/datasets/{id}` | PATCH | — | `h.UpdateDataset`<br><sub>openfoundry-go/services/dataset-versioning-service/internal/server/server.go:45</sub> | implemented |
| `/api/v1/datasets/{id}/branches` | GET | — | `h.ListBranches`<br><sub>openfoundry-go/services/dataset-versioning-service/internal/server/server.go:52</sub> | implemented |
| `/api/v1/datasets/{id}/branches` | POST | — | `h.CreateBranch`<br><sub>openfoundry-go/services/dataset-versioning-service/internal/server/server.go:53</sub> | implemented |
| `/api/v1/datasets/{id}/branches/{branch}` | GET | — | `h.GetBranch`<br><sub>openfoundry-go/services/dataset-versioning-service/internal/server/server.go:54</sub> | implemented |
| `/api/v1/datasets/{id}/files` | GET | — | `h.ListFiles`<br><sub>openfoundry-go/services/dataset-versioning-service/internal/server/server.go:56</sub> | implemented |
| `/api/v1/datasets/{id}/files/{file_id}/download` | GET | — | `h.DownloadFile`<br><sub>openfoundry-go/services/dataset-versioning-service/internal/server/server.go:57</sub> | implemented |
| `/api/v1/datasets/{rid}/health` | GET | `handlers::health::get_dataset_health`<br><sub>services/dataset-versioning-service/src/dataset_quality/mod.rs:66</sub> | `h.GetDatasetHealth`<br><sub>openfoundry-go/services/dataset-versioning-service/internal/server/server.go:66</sub> | implemented |
| `/api/v1/datasets/{id}/lint` | GET | `handlers::lint::get_dataset_lint`<br><sub>services/dataset-versioning-service/src/dataset_quality/mod.rs:56</sub> | `h.GetDatasetLint`<br><sub>openfoundry-go/services/dataset-versioning-service/internal/server/server.go:65</sub> | implemented |
| `/api/v1/datasets/{id}/quality` | GET | `handlers::quality::get_dataset_quality`<br><sub>services/dataset-versioning-service/src/dataset_quality/mod.rs:38</sub> | `h.GetDatasetQuality`<br><sub>openfoundry-go/services/dataset-versioning-service/internal/server/server.go:60</sub> | implemented |
| `/api/v1/datasets/{id}/quality/profile` | POST | `handlers::quality::refresh_dataset_quality`<br><sub>services/dataset-versioning-service/src/dataset_quality/mod.rs:42</sub> | `h.RefreshDatasetQuality`<br><sub>openfoundry-go/services/dataset-versioning-service/internal/server/server.go:61</sub> | implemented |
| `/api/v1/datasets/{id}/quality/rules` | POST | `handlers::quality::create_quality_rule`<br><sub>services/dataset-versioning-service/src/dataset_quality/mod.rs:46</sub> | `h.CreateQualityRule`<br><sub>openfoundry-go/services/dataset-versioning-service/internal/server/server.go:62</sub> | implemented |
| `/api/v1/datasets/{id}/quality/rules/{rule_id}` | DELETE | `handlers::quality::delete_quality_rule`<br><sub>services/dataset-versioning-service/src/dataset_quality/mod.rs:50</sub> | `h.DeleteQualityRule`<br><sub>openfoundry-go/services/dataset-versioning-service/internal/server/server.go:64</sub> | implemented |
| `/api/v1/datasets/{id}/quality/rules/{rule_id}` | PATCH | `handlers::quality::update_quality_rule`<br><sub>services/dataset-versioning-service/src/dataset_quality/mod.rs:50</sub> | `h.UpdateQualityRule`<br><sub>openfoundry-go/services/dataset-versioning-service/internal/server/server.go:63</sub> | implemented |
| `/api/v1/datasets/{id}/transactions/{txn}/files` | POST | — | `h.CreateFileUploadURL`<br><sub>openfoundry-go/services/dataset-versioning-service/internal/server/server.go:58</sub> | implemented |
| `/api/v1/datasets/{id}/versions` | GET | — | `h.ListVersions`<br><sub>openfoundry-go/services/dataset-versioning-service/internal/server/server.go:48</sub> | implemented |
| `/api/v1/datasets/{id}/versions` | POST | — | `h.CreateVersion`<br><sub>openfoundry-go/services/dataset-versioning-service/internal/server/server.go:49</sub> | implemented |
| `/api/v1/datasets/{id}/versions/{version}` | GET | — | `h.GetVersion`<br><sub>openfoundry-go/services/dataset-versioning-service/internal/server/server.go:50</sub> | implemented |
| `/health` | GET | `handlers::health::healthz`<br><sub>services/dataset-versioning-service/src/lib.rs:226</sub> | `func(w`<br><sub>openfoundry-go/services/dataset-versioning-service/internal/server/server.go:33</sub> | implemented |
| `/healthz` | GET | `handlers::health::healthz`<br><sub>services/dataset-versioning-service/src/lib.rs:225</sub> | `func(w`<br><sub>openfoundry-go/services/dataset-versioning-service/internal/server/server.go:29</sub> | implemented |
| `/internal/datasets/{rid}/metadata` | GET | `handlers::internal::get_dataset_metadata`<br><sub>services/dataset-versioning-service/src/data_asset_catalog/mod.rs:137</sub> | `h.GetDatasetMetadata`<br><sub>openfoundry-go/services/dataset-versioning-service/internal/server/server.go:71</sub> | implemented |
| `/metrics` | GET | `handlers::health::metrics`<br><sub>services/dataset-versioning-service/src/lib.rs:227</sub> | `m.Handler(`<br><sub>openfoundry-go/services/dataset-versioning-service/internal/server/server.go:37</sub> | implemented |
| `/v1/_internal/local-fs/*` | GET | — | `h.LocalPresignProxy`<br><sub>openfoundry-go/services/dataset-versioning-service/internal/server/server.go:87</sub> | implemented |
| `/v1/_internal/local-fs/{*key}` | GET | `handlers::files::local_presign_proxy`<br><sub>services/dataset-versioning-service/src/lib.rs:228</sub> | `h.LocalPresignProxy`<br><sub>openfoundry-go/services/dataset-versioning-service/internal/server/server.go:88</sub> | implemented |
| `/v1/catalog/facets` | GET | `handlers::catalog::get_catalog_facets`<br><sub>services/dataset-versioning-service/src/data_asset_catalog/mod.rs:55</sub> | `h.GetCatalogFacets`<br><sub>openfoundry-go/services/dataset-versioning-service/internal/server/server.go:90</sub> | implemented |
| `/v1/datasets` | GET | `handlers::crud::list_datasets`<br><sub>services/dataset-versioning-service/src/data_asset_catalog/mod.rs:60</sub> | `h.ListDatasets`<br><sub>openfoundry-go/services/dataset-versioning-service/internal/server/server.go:92</sub> | implemented |
| `/v1/datasets` | POST | `handlers::crud::create_dataset`<br><sub>services/dataset-versioning-service/src/data_asset_catalog/mod.rs:60</sub> | `h.CreateDataset`<br><sub>openfoundry-go/services/dataset-versioning-service/internal/server/server.go:93</sub> | implemented |
| `/v1/datasets/{rid}` | DELETE | `handlers::crud::delete_dataset`<br><sub>services/dataset-versioning-service/src/data_asset_catalog/mod.rs:64</sub> | `h.DeleteDataset`<br><sub>openfoundry-go/services/dataset-versioning-service/internal/server/server.go:96</sub> | implemented |
| `/v1/datasets/{rid}` | GET | `handlers::crud::get_dataset`<br><sub>services/dataset-versioning-service/src/data_asset_catalog/mod.rs:64</sub> | `h.GetDataset`<br><sub>openfoundry-go/services/dataset-versioning-service/internal/server/server.go:94</sub> | implemented |
| `/v1/datasets/{rid}` | PATCH | `handlers::crud::update_dataset`<br><sub>services/dataset-versioning-service/src/data_asset_catalog/mod.rs:64</sub> | `h.UpdateDataset`<br><sub>openfoundry-go/services/dataset-versioning-service/internal/server/server.go:95</sub> | implemented |
| `/v1/datasets/{rid}/branches` | GET | `handlers::foundry::list_branches`<br><sub>services/dataset-versioning-service/src/lib.rs:73</sub> | `h.ListBranches`<br><sub>openfoundry-go/services/dataset-versioning-service/internal/server/server.go:111</sub> | implemented |
| `/v1/datasets/{rid}/branches` | POST | `handlers::foundry::create_branch`<br><sub>services/dataset-versioning-service/src/lib.rs:73</sub> | `h.CreateBranch`<br><sub>openfoundry-go/services/dataset-versioning-service/internal/server/server.go:112</sub> | implemented |
| `/v1/datasets/{rid}/branches/compare` | GET | `handlers::compare::compare_branches`<br><sub>services/dataset-versioning-service/src/lib.rs:120</sub> | `h.CompareBranches`<br><sub>openfoundry-go/services/dataset-versioning-service/internal/server/server.go:113</sub> | implemented |
| `/v1/datasets/{rid}/branches/{branch}` | DELETE | `handlers::foundry::delete_branch`<br><sub>services/dataset-versioning-service/src/lib.rs:84</sub> | `h.DeleteBranch`<br><sub>openfoundry-go/services/dataset-versioning-service/internal/server/server.go:115</sub> | implemented |
| `/v1/datasets/{rid}/branches/{branch}` | GET | `handlers::foundry::get_branch`<br><sub>services/dataset-versioning-service/src/lib.rs:84</sub> | `h.GetBranch`<br><sub>openfoundry-go/services/dataset-versioning-service/internal/server/server.go:114</sub> | implemented |
| `/v1/datasets/{rid}/branches/{branch}` | POST | `handlers::foundry::branch_action`<br><sub>services/dataset-versioning-service/src/lib.rs:84</sub> | `h.BranchAction`<br><sub>openfoundry-go/services/dataset-versioning-service/internal/server/server.go:116</sub> | implemented |
| `/v1/datasets/{rid}/branches/{branch}/ancestry` | GET | `handlers::foundry::branch_ancestry`<br><sub>services/dataset-versioning-service/src/lib.rs:95</sub> | `h.BranchAncestry`<br><sub>openfoundry-go/services/dataset-versioning-service/internal/server/server.go:118</sub> | implemented |
| `/v1/datasets/{rid}/branches/{branch}/checkout` | POST | `handlers::branches::checkout_branch`<br><sub>services/dataset-versioning-service/src/lib.rs:90</sub> | `h.CheckoutBranch`<br><sub>openfoundry-go/services/dataset-versioning-service/internal/server/server.go:117</sub> | implemented |
| `/v1/datasets/{rid}/branches/{branch}/fallbacks` | GET | `handlers::foundry::list_fallbacks`<br><sub>services/dataset-versioning-service/src/lib.rs:141</sub> | `h.ListFallbacks`<br><sub>openfoundry-go/services/dataset-versioning-service/internal/server/server.go:124</sub> | implemented |
| `/v1/datasets/{rid}/branches/{branch}/fallbacks` | PUT | `handlers::foundry::put_fallbacks`<br><sub>services/dataset-versioning-service/src/lib.rs:141</sub> | `h.PutFallbacks`<br><sub>openfoundry-go/services/dataset-versioning-service/internal/server/server.go:125</sub> | implemented |
| `/v1/datasets/{rid}/branches/{branch}/markings` | GET | `handlers::retention::get_branch_markings`<br><sub>services/dataset-versioning-service/src/lib.rs:110</sub> | `h.GetBranchMarkings`<br><sub>openfoundry-go/services/dataset-versioning-service/internal/server/server.go:121</sub> | implemented |
| `/v1/datasets/{rid}/branches/{branch}/preview-delete` | GET | `handlers::foundry::preview_delete_branch`<br><sub>services/dataset-versioning-service/src/lib.rs:101</sub> | `h.PreviewDeleteBranch`<br><sub>openfoundry-go/services/dataset-versioning-service/internal/server/server.go:119</sub> | implemented |
| `/v1/datasets/{rid}/branches/{branch}/retention` | PATCH | `handlers::retention::update_retention`<br><sub>services/dataset-versioning-service/src/lib.rs:106</sub> | `h.UpdateRetention`<br><sub>openfoundry-go/services/dataset-versioning-service/internal/server/server.go:120</sub> | implemented |
| `/v1/datasets/{rid}/branches/{branch}/rollback` | POST | `handlers::foundry::rollback_branch`<br><sub>services/dataset-versioning-service/src/lib.rs:124</sub> | `h.RollbackBranch`<br><sub>openfoundry-go/services/dataset-versioning-service/internal/server/server.go:123</sub> | implemented |
| `/v1/datasets/{rid}/branches/{branch}/transactions` | POST | `handlers::foundry::start_transaction`<br><sub>services/dataset-versioning-service/src/lib.rs:129</sub> | `h.StartTransaction`<br><sub>openfoundry-go/services/dataset-versioning-service/internal/server/server.go:127</sub> | implemented |
| `/v1/datasets/{rid}/branches/{branch}/transactions/{txn}` | GET | `handlers::foundry::get_transaction`<br><sub>services/dataset-versioning-service/src/lib.rs:135</sub> | `h.GetTransaction`<br><sub>openfoundry-go/services/dataset-versioning-service/internal/server/server.go:128</sub> | implemented |
| `/v1/datasets/{rid}/branches/{branch}/transactions/{txn}` | POST | `handlers::foundry::transaction_action`<br><sub>services/dataset-versioning-service/src/lib.rs:135</sub> | `h.TransactionAction`<br><sub>openfoundry-go/services/dataset-versioning-service/internal/server/server.go:129</sub> | implemented |
| `/v1/datasets/{rid}/branches/{branch}/transactions/{txn}:abort` | POST | — | `h.AbortTransaction`<br><sub>openfoundry-go/services/dataset-versioning-service/internal/server/server.go:131</sub> | implemented |
| `/v1/datasets/{rid}/branches/{branch}/transactions/{txn}:commit` | POST | — | `h.CommitTransaction`<br><sub>openfoundry-go/services/dataset-versioning-service/internal/server/server.go:130</sub> | implemented |
| `/v1/datasets/{rid}/branches/{branch}:restore` | POST | `handlers::retention::restore_branch`<br><sub>services/dataset-versioning-service/src/lib.rs:114</sub> | `h.RestoreBranch`<br><sub>openfoundry-go/services/dataset-versioning-service/internal/server/server.go:122</sub> | implemented |
| `/v1/datasets/{rid}/compare` | GET | `handlers::foundry::compare_views`<br><sub>services/dataset-versioning-service/src/lib.rs:158</sub> | `h.CompareViews`<br><sub>openfoundry-go/services/dataset-versioning-service/internal/server/server.go:135</sub> | implemented |
| `/v1/datasets/{rid}/files` | GET | `handlers::files::list_files`<br><sub>services/dataset-versioning-service/src/lib.rs:193</sub> | `h.ListFiles`<br><sub>openfoundry-go/services/dataset-versioning-service/internal/server/server.go:149</sub> | implemented |
| `/v1/datasets/{rid}/files/index` | GET | `handlers::dataset_model::list_dataset_file_index`<br><sub>services/dataset-versioning-service/src/data_asset_catalog/mod.rs:93</sub> | `h.ListDatasetFileIndex`<br><sub>openfoundry-go/services/dataset-versioning-service/internal/server/server.go:106</sub> | implemented |
| `/v1/datasets/{rid}/files/index` | PUT | `handlers::dataset_model::put_dataset_file_index`<br><sub>services/dataset-versioning-service/src/data_asset_catalog/mod.rs:93</sub> | `h.PutDatasetFileIndex`<br><sub>openfoundry-go/services/dataset-versioning-service/internal/server/server.go:107</sub> | implemented |
| `/v1/datasets/{rid}/files/{file_id}/download` | GET | `handlers::files::download_file`<br><sub>services/dataset-versioning-service/src/lib.rs:197</sub> | `h.DownloadFile`<br><sub>openfoundry-go/services/dataset-versioning-service/internal/server/server.go:150</sub> | implemented |
| `/v1/datasets/{rid}/health` | GET | `handlers::health::get_dataset_health`<br><sub>services/dataset-versioning-service/src/dataset_quality/mod.rs:62</sub> | `h.GetDatasetHealth`<br><sub>openfoundry-go/services/dataset-versioning-service/internal/server/server.go:158</sub> | implemented |
| `/v1/datasets/{rid}/lineage-links` | GET | `handlers::dataset_model::list_dataset_lineage_links`<br><sub>services/dataset-versioning-service/src/data_asset_catalog/mod.rs:88</sub> | `h.ListDatasetLineageLinks`<br><sub>openfoundry-go/services/dataset-versioning-service/internal/server/server.go:104</sub> | implemented |
| `/v1/datasets/{rid}/lineage-links` | PUT | `handlers::dataset_model::put_dataset_lineage_links`<br><sub>services/dataset-versioning-service/src/data_asset_catalog/mod.rs:88</sub> | `h.PutDatasetLineageLinks`<br><sub>openfoundry-go/services/dataset-versioning-service/internal/server/server.go:105</sub> | implemented |
| `/v1/datasets/{rid}/markings` | GET | `handlers::dataset_model::list_dataset_markings`<br><sub>services/dataset-versioning-service/src/data_asset_catalog/mod.rs:78</sub> | `h.ListDatasetMarkings`<br><sub>openfoundry-go/services/dataset-versioning-service/internal/server/server.go:100</sub> | implemented |
| `/v1/datasets/{rid}/markings` | PUT | `handlers::dataset_model::put_dataset_markings`<br><sub>services/dataset-versioning-service/src/data_asset_catalog/mod.rs:78</sub> | `h.PutDatasetMarkings`<br><sub>openfoundry-go/services/dataset-versioning-service/internal/server/server.go:101</sub> | implemented |
| `/v1/datasets/{rid}/metadata` | PATCH | `handlers::dataset_model::patch_dataset_metadata`<br><sub>services/dataset-versioning-service/src/data_asset_catalog/mod.rs:74</sub> | `h.PatchDatasetMetadata`<br><sub>openfoundry-go/services/dataset-versioning-service/internal/server/server.go:99</sub> | implemented |
| `/v1/datasets/{rid}/model` | GET | `handlers::dataset_model::get_dataset_model`<br><sub>services/dataset-versioning-service/src/data_asset_catalog/mod.rs:70</sub> | `h.GetDatasetModel`<br><sub>openfoundry-go/services/dataset-versioning-service/internal/server/server.go:98</sub> | implemented |
| `/v1/datasets/{rid}/permissions` | GET | `handlers::dataset_model::list_dataset_permissions`<br><sub>services/dataset-versioning-service/src/data_asset_catalog/mod.rs:83</sub> | `h.ListDatasetPermissions`<br><sub>openfoundry-go/services/dataset-versioning-service/internal/server/server.go:102</sub> | implemented |
| `/v1/datasets/{rid}/permissions` | PUT | `handlers::dataset_model::put_dataset_permissions`<br><sub>services/dataset-versioning-service/src/data_asset_catalog/mod.rs:83</sub> | `h.PutDatasetPermissions`<br><sub>openfoundry-go/services/dataset-versioning-service/internal/server/server.go:103</sub> | implemented |
| `/v1/datasets/{rid}/preview` | GET | `handlers::preview::preview_data`<br><sub>services/dataset-versioning-service/src/data_asset_catalog/mod.rs:99</sub> | `h.PreviewDataset`<br><sub>openfoundry-go/services/dataset-versioning-service/internal/server/server.go:155</sub> | implemented |
| `/v1/datasets/{rid}/schema` | GET | `handlers::schema::get_current_schema`<br><sub>services/dataset-versioning-service/src/lib.rs:209</sub> | `h.GetCurrentSchema`<br><sub>openfoundry-go/services/dataset-versioning-service/internal/server/server.go:156</sub> | implemented |
| `/v1/datasets/{rid}/schema:validate` | POST | `handlers::schema_validate::validate_schema`<br><sub>services/dataset-versioning-service/src/data_asset_catalog/mod.rs:108</sub> | `h.ValidateSchema`<br><sub>openfoundry-go/services/dataset-versioning-service/internal/server/server.go:157</sub> | implemented |
| `/v1/datasets/{rid}/storage-details` | GET | `handlers::files::storage_details`<br><sub>services/dataset-versioning-service/src/lib.rs:205</sub> | `h.StorageDetails`<br><sub>openfoundry-go/services/dataset-versioning-service/internal/server/server.go:152</sub> | implemented |
| `/v1/datasets/{rid}/transactions` | GET | `handlers::foundry::list_transactions`<br><sub>services/dataset-versioning-service/src/lib.rs:146</sub> | `h.ListTransactions`<br><sub>openfoundry-go/services/dataset-versioning-service/internal/server/server.go:132</sub> | implemented |
| `/v1/datasets/{rid}/transactions/{txn_id}/files` | POST | `handlers::files::upload_url`<br><sub>services/dataset-versioning-service/src/lib.rs:201</sub> | `h.CreateFileUploadURL`<br><sub>openfoundry-go/services/dataset-versioning-service/internal/server/server.go:151</sub> | implemented |
| `/v1/datasets/{rid}/transactions:batchGet` | POST | `handlers::foundry::batch_get_transactions`<br><sub>services/dataset-versioning-service/src/lib.rs:154</sub> | `h.BatchGetTransactions`<br><sub>openfoundry-go/services/dataset-versioning-service/internal/server/server.go:133</sub> | implemented |
| `/v1/datasets/{rid}/upload` | POST | `handlers::upload::upload_data`<br><sub>services/dataset-versioning-service/src/data_asset_catalog/mod.rs:113</sub> | `h.UploadData`<br><sub>openfoundry-go/services/dataset-versioning-service/internal/server/server.go:153</sub> | implemented |
| `/v1/datasets/{rid}/versions` | GET | `handlers::versions::list_versions`<br><sub>services/dataset-versioning-service/src/lib.rs:77</sub> | `h.ListVersions`<br><sub>openfoundry-go/services/dataset-versioning-service/internal/server/server.go:109</sub> | implemented |
| `/v1/datasets/{rid}/views` | GET | `handlers::views::list_views`<br><sub>services/dataset-versioning-service/src/data_asset_catalog/mod.rs:123</sub> | `h.ListViews`<br><sub>openfoundry-go/services/dataset-versioning-service/internal/server/server.go:137</sub> | implemented |
| `/v1/datasets/{rid}/views` | POST | `handlers::views::create_view`<br><sub>services/dataset-versioning-service/src/data_asset_catalog/mod.rs:123</sub> | `h.CreateView`<br><sub>openfoundry-go/services/dataset-versioning-service/internal/server/server.go:138</sub> | implemented |
| `/v1/datasets/{rid}/views/at` | GET | `handlers::foundry::get_view_at`<br><sub>services/dataset-versioning-service/src/lib.rs:167</sub> | `h.GetViewAt`<br><sub>openfoundry-go/services/dataset-versioning-service/internal/server/server.go:140</sub> | implemented |
| `/v1/datasets/{rid}/views/current` | GET | `handlers::foundry::get_current_view`<br><sub>services/dataset-versioning-service/src/lib.rs:163</sub> | `h.GetCurrentView`<br><sub>openfoundry-go/services/dataset-versioning-service/internal/server/server.go:139</sub> | implemented |
| `/v1/datasets/{rid}/views/{view_or_action}` | GET | `handlers::views::get_view`<br><sub>services/dataset-versioning-service/src/data_asset_catalog/mod.rs:127</sub> | `h.GetView`<br><sub>openfoundry-go/services/dataset-versioning-service/internal/server/server.go:146</sub> | implemented |
| `/v1/datasets/{rid}/views/{view_or_action}` | POST | `view_action_dispatch`<br><sub>services/dataset-versioning-service/src/data_asset_catalog/mod.rs:127</sub> | `h.ViewAction`<br><sub>openfoundry-go/services/dataset-versioning-service/internal/server/server.go:147</sub> | implemented |
| `/v1/datasets/{rid}/views/{view_id}/data` | GET | `handlers::preview::preview_view`<br><sub>services/dataset-versioning-service/src/lib.rs:187</sub> | `h.PreviewViewData`<br><sub>openfoundry-go/services/dataset-versioning-service/internal/server/server.go:144</sub> | implemented |
| `/v1/datasets/{rid}/views/{view_id}/files` | GET | `handlers::foundry::list_view_files`<br><sub>services/dataset-versioning-service/src/lib.rs:171</sub> | `h.ListViewFiles`<br><sub>openfoundry-go/services/dataset-versioning-service/internal/server/server.go:141</sub> | implemented |
| `/v1/datasets/{rid}/views/{view_id}/preview` | GET | `handlers::views::preview_view`<br><sub>services/dataset-versioning-service/src/data_asset_catalog/mod.rs:131</sub> | `h.PreviewMaterializedView`<br><sub>openfoundry-go/services/dataset-versioning-service/internal/server/server.go:145</sub> | implemented |
| `/v1/datasets/{rid}/views/{view_id}/schema` | GET | `handlers::schema::get_view_schema`<br><sub>services/dataset-versioning-service/src/lib.rs:179</sub> | `h.GetViewSchema`<br><sub>openfoundry-go/services/dataset-versioning-service/internal/server/server.go:142</sub> | implemented |
| `/v1/datasets/{rid}/views/{view_id}/schema` | POST | `handlers::schema::put_view_schema`<br><sub>services/dataset-versioning-service/src/lib.rs:179</sub> | `h.PutViewSchema`<br><sub>openfoundry-go/services/dataset-versioning-service/internal/server/server.go:143</sub> | implemented |

## ingestion-replication-service

Rust routes: 12. Go routes: 25.
State counts: empty envelope: 1, implemented: 24, missing: 12.

| Route | Method | Rust handler | Go handler | State |
| --- | --- | --- | --- | --- |
| `/api/v1/cdc/streams` | GET | — | `h.ListCdcStreams`<br><sub>openfoundry-go/services/ingestion-replication-service/internal/server/server.go:85</sub> | implemented |
| `/api/v1/cdc/streams` | POST | — | `h.RegisterCdcStream`<br><sub>openfoundry-go/services/ingestion-replication-service/internal/server/server.go:86</sub> | implemented |
| `/api/v1/cdc/streams/{id}` | GET | — | `h.GetCdcStream`<br><sub>openfoundry-go/services/ingestion-replication-service/internal/server/server.go:87</sub> | implemented |
| `/api/v1/cdc/streams/{id}/checkpoint` | GET | — | `h.GetCdcCheckpoint`<br><sub>openfoundry-go/services/ingestion-replication-service/internal/server/server.go:88</sub> | implemented |
| `/api/v1/cdc/streams/{id}/resolution` | GET | — | `h.GetCdcResolution`<br><sub>openfoundry-go/services/ingestion-replication-service/internal/server/server.go:89</sub> | implemented |
| `/api/v1/ingest-jobs` | GET | — | `h.ListIngestJobs`<br><sub>openfoundry-go/services/ingestion-replication-service/internal/server/server.go:49</sub> | empty envelope |
| `/api/v1/ingest-jobs` | POST | — | `h.CreateIngestJob`<br><sub>openfoundry-go/services/ingestion-replication-service/internal/server/server.go:50</sub> | implemented |
| `/api/v1/ingest-jobs/{id}` | DELETE | — | `h.DeleteIngestJob`<br><sub>openfoundry-go/services/ingestion-replication-service/internal/server/server.go:53</sub> | implemented |
| `/api/v1/ingest-jobs/{id}` | GET | — | `h.GetIngestJob`<br><sub>openfoundry-go/services/ingestion-replication-service/internal/server/server.go:51</sub> | implemented |
| `/api/v1/ingest-jobs/{id}` | PATCH | — | `h.UpdateIngestJob`<br><sub>openfoundry-go/services/ingestion-replication-service/internal/server/server.go:52</sub> | implemented |
| `/api/v1/streams` | GET | — | `h.ListStreams`<br><sub>openfoundry-go/services/ingestion-replication-service/internal/server/server.go:55</sub> | implemented |
| `/api/v1/streams` | POST | — | `h.CreateStream`<br><sub>openfoundry-go/services/ingestion-replication-service/internal/server/server.go:56</sub> | implemented |
| `/api/v1/streams/{id}` | GET | — | `h.GetStream`<br><sub>openfoundry-go/services/ingestion-replication-service/internal/server/server.go:57</sub> | implemented |
| `/api/v1/streams/{id}` | PATCH | — | `h.UpdateStream`<br><sub>openfoundry-go/services/ingestion-replication-service/internal/server/server.go:58</sub> | implemented |
| `/api/v1/streams/{id}/branches` | GET | — | `sm.Branches.ListBranches`<br><sub>openfoundry-go/services/ingestion-replication-service/internal/server/server.go:77</sub> | implemented |
| `/api/v1/streams/{id}/branches` | POST | — | `sm.Branches.CreateBranch`<br><sub>openfoundry-go/services/ingestion-replication-service/internal/server/server.go:78</sub> | implemented |
| `/api/v1/streams/{id}/branches/{name}` | DELETE | — | `sm.Branches.DeleteBranch`<br><sub>openfoundry-go/services/ingestion-replication-service/internal/server/server.go:80</sub> | implemented |
| `/api/v1/streams/{id}/branches/{name}` | GET | — | `sm.Branches.GetBranch`<br><sub>openfoundry-go/services/ingestion-replication-service/internal/server/server.go:79</sub> | implemented |
| `/api/v1/streams/{id}/branches/{name}:archive` | POST | — | `sm.Branches.ArchiveBranch`<br><sub>openfoundry-go/services/ingestion-replication-service/internal/server/server.go:82</sub> | implemented |
| `/api/v1/streams/{id}/branches/{name}:merge` | POST | — | `sm.Branches.MergeBranch`<br><sub>openfoundry-go/services/ingestion-replication-service/internal/server/server.go:81</sub> | implemented |
| `/api/v1/streams/{id}/schema/history` | GET | — | `sm.Schemas.ListSchemaHistory`<br><sub>openfoundry-go/services/ingestion-replication-service/internal/server/server.go:70</sub> | implemented |
| `/api/v1/streams/{id}/schema:validate` | POST | — | `sm.Schemas.ValidateSchema`<br><sub>openfoundry-go/services/ingestion-replication-service/internal/server/server.go:69</sub> | implemented |
| `/api/v1/streams/{id}:reset` | POST | — | `h.ResetStream`<br><sub>openfoundry-go/services/ingestion-replication-service/internal/server/server.go:62</sub> | implemented |
| `/compatibility/subjects/:name/versions/:version` | POST | `check_compatibility`<br><sub>services/ingestion-replication-service/src/cdc_metadata/schema_registry.rs:47</sub> | — | missing |
| `/healthz` | GET | — | `func(w`<br><sub>openfoundry-go/services/ingestion-replication-service/internal/server/server.go:40</sub> | implemented |
| `/metrics` | GET | — | `m.Handler(`<br><sub>openfoundry-go/services/ingestion-replication-service/internal/server/server.go:44</sub> | implemented |
| `/streams` | GET | `handlers::list_streams`<br><sub>services/ingestion-replication-service/src/cdc_metadata/mod.rs:15</sub> | — | missing |
| `/streams` | POST | `handlers::register_stream`<br><sub>services/ingestion-replication-service/src/cdc_metadata/mod.rs:15</sub> | — | missing |
| `/streams/:id` | GET | `handlers::get_stream`<br><sub>services/ingestion-replication-service/src/cdc_metadata/mod.rs:19</sub> | — | missing |
| `/streams/:id/checkpoint` | GET | `handlers::get_checkpoint`<br><sub>services/ingestion-replication-service/src/cdc_metadata/mod.rs:20</sub> | — | missing |
| `/streams/:id/checkpoint` | POST | `handlers::record_checkpoint`<br><sub>services/ingestion-replication-service/src/cdc_metadata/mod.rs:20</sub> | — | missing |
| `/streams/:id/resolution` | GET | `handlers::get_resolution`<br><sub>services/ingestion-replication-service/src/cdc_metadata/mod.rs:24</sub> | — | missing |
| `/streams/:id/resolution` | PUT | `handlers::update_resolution`<br><sub>services/ingestion-replication-service/src/cdc_metadata/mod.rs:24</sub> | — | missing |
| `/subjects` | GET | `list_subjects`<br><sub>services/ingestion-replication-service/src/cdc_metadata/schema_registry.rs:41</sub> | — | missing |
| `/subjects/:name/versions` | GET | `list_versions`<br><sub>services/ingestion-replication-service/src/cdc_metadata/schema_registry.rs:42</sub> | — | missing |
| `/subjects/:name/versions` | POST | `register_version`<br><sub>services/ingestion-replication-service/src/cdc_metadata/schema_registry.rs:42</sub> | — | missing |
| `/subjects/:name/versions/:version` | GET | `get_version`<br><sub>services/ingestion-replication-service/src/cdc_metadata/schema_registry.rs:46</sub> | — | missing |

## iceberg-catalog-service

Rust routes: 29. Go routes: 46.
State counts: implemented: 46, missing: 14.

| Route | Method | Rust handler | Go handler | State |
| --- | --- | --- | --- | --- |
| `/api/v1/iceberg-tables` | GET | `handlers::admin::list_iceberg_tables`<br><sub>services/iceberg-catalog-service/src/lib.rs:119</sub> | — | missing |
| `/api/v1/iceberg-tables/{id}` | GET | `handlers::admin::get_iceberg_table_detail`<br><sub>services/iceberg-catalog-service/src/lib.rs:123</sub> | — | missing |
| `/api/v1/iceberg-tables/{id}/branches` | GET | `handlers::admin::list_iceberg_table_branches`<br><sub>services/iceberg-catalog-service/src/lib.rs:135</sub> | — | missing |
| `/api/v1/iceberg-tables/{id}/metadata` | GET | `handlers::admin::get_iceberg_table_metadata`<br><sub>services/iceberg-catalog-service/src/lib.rs:131</sub> | — | missing |
| `/api/v1/iceberg-tables/{id}/snapshots` | GET | `handlers::admin::list_iceberg_table_snapshots`<br><sub>services/iceberg-catalog-service/src/lib.rs:127</sub> | — | missing |
| `/api/v1/namespaces` | GET | — | `h.ListNamespaces`<br><sub>openfoundry-go/services/iceberg-catalog-service/internal/server/server.go:59</sub> | implemented |
| `/api/v1/namespaces` | POST | — | `h.CreateNamespace`<br><sub>openfoundry-go/services/iceberg-catalog-service/internal/server/server.go:60</sub> | implemented |
| `/api/v1/namespaces/{id}` | DELETE | — | `h.DeleteNamespace`<br><sub>openfoundry-go/services/iceberg-catalog-service/internal/server/server.go:63</sub> | implemented |
| `/api/v1/namespaces/{id}` | GET | — | `h.GetNamespace`<br><sub>openfoundry-go/services/iceberg-catalog-service/internal/server/server.go:61</sub> | implemented |
| `/api/v1/namespaces/{id}` | PATCH | — | `h.UpdateNamespace`<br><sub>openfoundry-go/services/iceberg-catalog-service/internal/server/server.go:62</sub> | implemented |
| `/api/v1/namespaces/{namespace}/tables` | GET | — | `h.ListTables`<br><sub>openfoundry-go/services/iceberg-catalog-service/internal/server/server.go:65</sub> | implemented |
| `/api/v1/namespaces/{namespace}/tables` | POST | — | `h.CreateTable`<br><sub>openfoundry-go/services/iceberg-catalog-service/internal/server/server.go:66</sub> | implemented |
| `/api/v1/namespaces/{namespace}/tables/{table}` | DELETE | — | `h.DropTable`<br><sub>openfoundry-go/services/iceberg-catalog-service/internal/server/server.go:70</sub> | implemented |
| `/api/v1/namespaces/{namespace}/tables/{table}` | GET | — | `h.LoadTable`<br><sub>openfoundry-go/services/iceberg-catalog-service/internal/server/server.go:68</sub> | implemented |
| `/api/v1/namespaces/{namespace}/tables/{table}` | POST | — | `h.CommitTable`<br><sub>openfoundry-go/services/iceberg-catalog-service/internal/server/server.go:69</sub> | implemented |
| `/api/v1/namespaces/{namespace}/tables/{table}/metadata` | GET | — | `h.ListMetadataFiles`<br><sub>openfoundry-go/services/iceberg-catalog-service/internal/server/server.go:75</sub> | implemented |
| `/api/v1/namespaces/{namespace}/tables/{table}/metadata/{version}` | GET | — | `h.GetMetadataFile`<br><sub>openfoundry-go/services/iceberg-catalog-service/internal/server/server.go:76</sub> | implemented |
| `/api/v1/namespaces/{namespace}/tables/{table}/refs` | GET | — | `h.ListRefs`<br><sub>openfoundry-go/services/iceberg-catalog-service/internal/server/server.go:71</sub> | implemented |
| `/api/v1/namespaces/{namespace}/tables/{table}/refs/{ref}` | DELETE | — | `h.DeleteRef`<br><sub>openfoundry-go/services/iceberg-catalog-service/internal/server/server.go:74</sub> | implemented |
| `/api/v1/namespaces/{namespace}/tables/{table}/refs/{ref}` | GET | — | `h.GetRef`<br><sub>openfoundry-go/services/iceberg-catalog-service/internal/server/server.go:72</sub> | implemented |
| `/api/v1/namespaces/{namespace}/tables/{table}/refs/{ref}` | PUT | — | `h.UpsertRef`<br><sub>openfoundry-go/services/iceberg-catalog-service/internal/server/server.go:73</sub> | implemented |
| `/api/v1/namespaces/{namespace}/tables/{table}/snapshots` | GET | — | `h.ListSnapshots`<br><sub>openfoundry-go/services/iceberg-catalog-service/internal/server/server.go:77</sub> | implemented |
| `/api/v1/namespaces/{namespace}/tables/{table}/snapshots/{snapshot_id}` | GET | — | `h.GetSnapshot`<br><sub>openfoundry-go/services/iceberg-catalog-service/internal/server/server.go:78</sub> | implemented |
| `/api/v1/tables/rename` | POST | — | `h.RenameTable`<br><sub>openfoundry-go/services/iceberg-catalog-service/internal/server/server.go:67</sub> | implemented |
| `/health` | GET | `||`<br><sub>services/iceberg-catalog-service/src/lib.rs:144</sub> | — | missing |
| `/healthz` | GET | — | `func(w`<br><sub>openfoundry-go/services/iceberg-catalog-service/internal/server/server.go:49</sub> | implemented |
| `/iceberg/v1/config` | GET | `handlers::rest_catalog::config::get_config`<br><sub>services/iceberg-catalog-service/src/lib.rs:41</sub> | `h.GetConfig`<br><sub>openfoundry-go/services/iceberg-catalog-service/internal/server/server.go:118</sub> | implemented |
| `/iceberg/v1/diagnose` | POST | `handlers::diagnose::run_diagnose`<br><sub>services/iceberg-catalog-service/src/lib.rs:99</sub> | `h.RunDiagnose`<br><sub>openfoundry-go/services/iceberg-catalog-service/internal/server/server.go:119</sub> | implemented |
| `/iceberg/v1/namespaces` | GET | `handlers::rest_catalog::namespaces::list_namespaces`<br><sub>services/iceberg-catalog-service/src/lib.rs:46</sub> | — | missing |
| `/iceberg/v1/namespaces` | POST | `handlers::rest_catalog::namespaces::create_namespace`<br><sub>services/iceberg-catalog-service/src/lib.rs:46</sub> | — | missing |
| `/iceberg/v1/namespaces/{namespace}` | DELETE | `handlers::rest_catalog::namespaces::drop_namespace`<br><sub>services/iceberg-catalog-service/src/lib.rs:51</sub> | — | missing |
| `/iceberg/v1/namespaces/{namespace}` | GET | `handlers::rest_catalog::namespaces::load_namespace`<br><sub>services/iceberg-catalog-service/src/lib.rs:51</sub> | — | missing |
| `/iceberg/v1/namespaces/{namespace}/markings` | GET | `handlers::markings::get_namespace_markings`<br><sub>services/iceberg-catalog-service/src/lib.rs:88</sub> | `deps.Markings.GetNamespaceMarkings`<br><sub>openfoundry-go/services/iceberg-catalog-service/internal/server/server.go:106</sub> | implemented |
| `/iceberg/v1/namespaces/{namespace}/markings` | POST | `handlers::markings::update_namespace_markings`<br><sub>services/iceberg-catalog-service/src/lib.rs:88</sub> | `deps.Markings.UpdateNamespaceMarkings`<br><sub>openfoundry-go/services/iceberg-catalog-service/internal/server/server.go:107</sub> | implemented |
| `/iceberg/v1/namespaces/{namespace}/properties` | GET | `handlers::rest_catalog::namespaces::get_properties`<br><sub>services/iceberg-catalog-service/src/lib.rs:56</sub> | — | missing |
| `/iceberg/v1/namespaces/{namespace}/properties` | POST | `handlers::rest_catalog::namespaces::update_properties`<br><sub>services/iceberg-catalog-service/src/lib.rs:56</sub> | — | missing |
| `/iceberg/v1/namespaces/{namespace}/tables` | GET | `handlers::rest_catalog::tables::list_tables`<br><sub>services/iceberg-catalog-service/src/lib.rs:62</sub> | `h.ListTables`<br><sub>openfoundry-go/services/iceberg-catalog-service/internal/server/server.go:120</sub> | implemented |
| `/iceberg/v1/namespaces/{namespace}/tables` | POST | `handlers::rest_catalog::tables::create_table`<br><sub>services/iceberg-catalog-service/src/lib.rs:62</sub> | `h.CreateTable`<br><sub>openfoundry-go/services/iceberg-catalog-service/internal/server/server.go:121</sub> | implemented |
| `/iceberg/v1/namespaces/{namespace}/tables/{table}` | DELETE | `handlers::rest_catalog::tables::drop_table`<br><sub>services/iceberg-catalog-service/src/lib.rs:67</sub> | `h.DropTable`<br><sub>openfoundry-go/services/iceberg-catalog-service/internal/server/server.go:125</sub> | implemented |
| `/iceberg/v1/namespaces/{namespace}/tables/{table}` | GET | `handlers::rest_catalog::tables::load_table`<br><sub>services/iceberg-catalog-service/src/lib.rs:67</sub> | `h.LoadTable`<br><sub>openfoundry-go/services/iceberg-catalog-service/internal/server/server.go:123</sub> | implemented |
| `/iceberg/v1/namespaces/{namespace}/tables/{table}` | HEAD | `handlers::rest_catalog::tables::table_exists`<br><sub>services/iceberg-catalog-service/src/lib.rs:73</sub> | — | missing |
| `/iceberg/v1/namespaces/{namespace}/tables/{table}` | POST | `handlers::rest_catalog::tables::commit_table`<br><sub>services/iceberg-catalog-service/src/lib.rs:67</sub> | `h.CommitTable`<br><sub>openfoundry-go/services/iceberg-catalog-service/internal/server/server.go:124</sub> | implemented |
| `/iceberg/v1/namespaces/{namespace}/tables/{table}/alter-schema` | POST | `handlers::rest_catalog::tables::alter_schema`<br><sub>services/iceberg-catalog-service/src/lib.rs:78</sub> | — | missing |
| `/iceberg/v1/namespaces/{namespace}/tables/{table}/markings` | GET | `handlers::markings::get_table_markings`<br><sub>services/iceberg-catalog-service/src/lib.rs:93</sub> | `deps.Markings.GetTableMarkings`<br><sub>openfoundry-go/services/iceberg-catalog-service/internal/server/server.go:111</sub> | implemented |
| `/iceberg/v1/namespaces/{namespace}/tables/{table}/markings` | PATCH | `handlers::markings::update_table_markings`<br><sub>services/iceberg-catalog-service/src/lib.rs:93</sub> | `deps.Markings.UpdateTableMarkings`<br><sub>openfoundry-go/services/iceberg-catalog-service/internal/server/server.go:112</sub> | implemented |
| `/iceberg/v1/namespaces/{namespace}/tables/{table}/metadata` | GET | — | `h.ListMetadataFiles`<br><sub>openfoundry-go/services/iceberg-catalog-service/internal/server/server.go:130</sub> | implemented |
| `/iceberg/v1/namespaces/{namespace}/tables/{table}/metadata/{version}` | GET | — | `h.GetMetadataFile`<br><sub>openfoundry-go/services/iceberg-catalog-service/internal/server/server.go:131</sub> | implemented |
| `/iceberg/v1/namespaces/{namespace}/tables/{table}/refs` | GET | — | `h.ListRefs`<br><sub>openfoundry-go/services/iceberg-catalog-service/internal/server/server.go:126</sub> | implemented |
| `/iceberg/v1/namespaces/{namespace}/tables/{table}/refs/{ref}` | DELETE | — | `h.DeleteRef`<br><sub>openfoundry-go/services/iceberg-catalog-service/internal/server/server.go:129</sub> | implemented |
| `/iceberg/v1/namespaces/{namespace}/tables/{table}/refs/{ref}` | GET | — | `h.GetRef`<br><sub>openfoundry-go/services/iceberg-catalog-service/internal/server/server.go:127</sub> | implemented |
| `/iceberg/v1/namespaces/{namespace}/tables/{table}/refs/{ref}` | PUT | — | `h.UpsertRef`<br><sub>openfoundry-go/services/iceberg-catalog-service/internal/server/server.go:128</sub> | implemented |
| `/iceberg/v1/namespaces/{namespace}/tables/{table}/snapshots` | GET | — | `h.ListSnapshots`<br><sub>openfoundry-go/services/iceberg-catalog-service/internal/server/server.go:132</sub> | implemented |
| `/iceberg/v1/namespaces/{namespace}/tables/{table}/snapshots/{snapshot_id}` | GET | — | `h.GetSnapshot`<br><sub>openfoundry-go/services/iceberg-catalog-service/internal/server/server.go:133</sub> | implemented |
| `/iceberg/v1/oauth/tokens` | POST | `handlers::auth::oauth::issue_token`<br><sub>services/iceberg-catalog-service/src/lib.rs:108</sub> | `auth.IssueTokenHandler(deps.Bearer`<br><sub>openfoundry-go/services/iceberg-catalog-service/internal/server/server.go:85</sub> | implemented |
| `/iceberg/v1/tables/rename` | POST | — | `h.RenameTable`<br><sub>openfoundry-go/services/iceberg-catalog-service/internal/server/server.go:122</sub> | implemented |
| `/iceberg/v1/transactions/commit` | POST | `handlers::rest_catalog::transactions::multi_table_commit`<br><sub>services/iceberg-catalog-service/src/lib.rs:83</sub> | `h.MultiTableCommit`<br><sub>openfoundry-go/services/iceberg-catalog-service/internal/server/server.go:134</sub> | implemented |
| `/metrics` | GET | `metrics::render_metrics`<br><sub>services/iceberg-catalog-service/src/lib.rs:145</sub> | `m.Handler(`<br><sub>openfoundry-go/services/iceberg-catalog-service/internal/server/server.go:54</sub> | implemented |
| `/openfoundry/iceberg/v1/append` | POST | — | `h.AppendBatch`<br><sub>openfoundry-go/services/iceberg-catalog-service/internal/server/server.go:81</sub> | implemented |
| `/v1/iceberg-clients/api-tokens` | POST | `handlers::auth::api_tokens::create_api_token`<br><sub>services/iceberg-catalog-service/src/lib.rs:112</sub> | `auth.CreateAPITokenHandler(deps.IssueAPIStore`<br><sub>openfoundry-go/services/iceberg-catalog-service/internal/server/server.go:96</sub> | implemented |
| `/version` | GET | — | `versionHandler(cfg`<br><sub>openfoundry-go/services/iceberg-catalog-service/internal/server/server.go:53</sub> | implemented |

## federation-product-exchange-service

Rust routes: 23. Go routes: 48.
State counts: implemented: 48.

| Route | Method | Rust handler | Go handler | State |
| --- | --- | --- | --- | --- |
| `/api/v1/marketplace/dependency-plan` | POST | — | `h.PreviewDependencyPlan`<br><sub>openfoundry-go/services/federation-product-exchange-service/internal/server/server.go:68</sub> | implemented |
| `/api/v1/marketplace/installs` | GET | — | `h.ListInstalls`<br><sub>openfoundry-go/services/federation-product-exchange-service/internal/server/server.go:66</sub> | implemented |
| `/api/v1/marketplace/installs` | POST | — | `h.CreateInstall`<br><sub>openfoundry-go/services/federation-product-exchange-service/internal/server/server.go:67</sub> | implemented |
| `/api/v1/marketplace/listings` | GET | — | `h.ListListings`<br><sub>openfoundry-go/services/federation-product-exchange-service/internal/server/server.go:60</sub> | implemented |
| `/api/v1/marketplace/listings` | POST | — | `h.CreateListing`<br><sub>openfoundry-go/services/federation-product-exchange-service/internal/server/server.go:61</sub> | implemented |
| `/api/v1/marketplace/listings/slug/{slug}` | GET | — | `h.GetListing`<br><sub>openfoundry-go/services/federation-product-exchange-service/internal/server/server.go:62</sub> | implemented |
| `/api/v1/marketplace/listings/{ref}` | GET | — | `h.GetListing`<br><sub>openfoundry-go/services/federation-product-exchange-service/internal/server/server.go:63</sub> | implemented |
| `/api/v1/marketplace/listings/{id}` | PATCH | — | `h.UpdateListing`<br><sub>openfoundry-go/services/federation-product-exchange-service/internal/server/server.go:64</sub> | implemented |
| `/api/v1/marketplace/listings/{id}/versions` | POST | — | `h.PublishVersion`<br><sub>openfoundry-go/services/federation-product-exchange-service/internal/server/server.go:65</sub> | implemented |
| `/api/v1/product-distribution/contracts` | GET | — | `d.ListContracts`<br><sub>openfoundry-go/services/federation-product-exchange-service/internal/server/server.go:116</sub> | implemented |
| `/api/v1/product-distribution/contracts` | POST | — | `d.CreateContract`<br><sub>openfoundry-go/services/federation-product-exchange-service/internal/server/server.go:117</sub> | implemented |
| `/api/v1/product-distribution/contracts/{id}` | PATCH | — | `d.UpdateContract`<br><sub>openfoundry-go/services/federation-product-exchange-service/internal/server/server.go:118</sub> | implemented |
| `/api/v1/product-distribution/peers` | GET | — | `d.ListPeers`<br><sub>openfoundry-go/services/federation-product-exchange-service/internal/server/server.go:111</sub> | implemented |
| `/api/v1/product-distribution/peers` | POST | — | `d.CreatePeer`<br><sub>openfoundry-go/services/federation-product-exchange-service/internal/server/server.go:112</sub> | implemented |
| `/api/v1/product-distribution/peers/{id}` | DELETE | — | `d.DeletePeer`<br><sub>openfoundry-go/services/federation-product-exchange-service/internal/server/server.go:115</sub> | implemented |
| `/api/v1/product-distribution/peers/{id}` | GET | — | `d.GetPeer`<br><sub>openfoundry-go/services/federation-product-exchange-service/internal/server/server.go:113</sub> | implemented |
| `/api/v1/product-distribution/peers/{id}` | PATCH | — | `d.UpdatePeer`<br><sub>openfoundry-go/services/federation-product-exchange-service/internal/server/server.go:114</sub> | implemented |
| `/api/v1/product-distribution/queries` | POST | — | `d.ConsumeQuery`<br><sub>openfoundry-go/services/federation-product-exchange-service/internal/server/server.go:124</sub> | implemented |
| `/api/v1/product-distribution/shares` | GET | — | `d.ListShareManifests`<br><sub>openfoundry-go/services/federation-product-exchange-service/internal/server/server.go:119</sub> | implemented |
| `/api/v1/product-distribution/shares` | POST | — | `d.CreateShareManifest`<br><sub>openfoundry-go/services/federation-product-exchange-service/internal/server/server.go:120</sub> | implemented |
| `/api/v1/product-distribution/shares/{id}` | GET | — | `d.GetShareManifest`<br><sub>openfoundry-go/services/federation-product-exchange-service/internal/server/server.go:121</sub> | implemented |
| `/api/v1/product-distribution/shares/{id}/sync-status` | PATCH | — | `d.UpdateSyncStatus`<br><sub>openfoundry-go/services/federation-product-exchange-service/internal/server/server.go:123</sub> | implemented |
| `/api/v1/product-distribution/sync-statuses` | GET | — | `d.ListSyncStatuses`<br><sub>openfoundry-go/services/federation-product-exchange-service/internal/server/server.go:122</sub> | implemented |
| `/health` | GET | `handlers::health::healthz`<br><sub>services/federation-product-exchange-service/src/marketplace/mod.rs:117</sub> | `healthHandler`<br><sub>openfoundry-go/services/federation-product-exchange-service/internal/server/server.go:50</sub> | implemented |
| `/healthz` | GET | `handlers::health::healthz`<br><sub>services/federation-product-exchange-service/src/marketplace/mod.rs:116</sub> | `healthHandler`<br><sub>openfoundry-go/services/federation-product-exchange-service/internal/server/server.go:49</sub> | implemented |
| `/metrics` | GET | `handlers::health::metrics`<br><sub>services/federation-product-exchange-service/src/marketplace/mod.rs:118</sub> | `m.Handler(`<br><sub>openfoundry-go/services/federation-product-exchange-service/internal/server/server.go:52</sub> | implemented |
| `/v1/marketplace/categories` | GET | `handlers::browse::list_categories`<br><sub>services/federation-product-exchange-service/src/marketplace/mod.rs:43</sub> | `h.ListCategories`<br><sub>openfoundry-go/services/federation-product-exchange-service/internal/server/server.go:76</sub> | implemented |
| `/v1/marketplace/installs` | GET | `handlers::install::list_installs`<br><sub>services/federation-product-exchange-service/src/marketplace/mod.rs:71</sub> | `h.ListInstallsEnvelope`<br><sub>openfoundry-go/services/federation-product-exchange-service/internal/server/server.go:85</sub> | implemented |
| `/v1/marketplace/installs` | POST | `handlers::install::create_install`<br><sub>services/federation-product-exchange-service/src/marketplace/mod.rs:71</sub> | `h.CreateInstall`<br><sub>openfoundry-go/services/federation-product-exchange-service/internal/server/server.go:86</sub> | implemented |
| `/v1/marketplace/listings` | GET | `handlers::browse::list_listings`<br><sub>services/federation-product-exchange-service/src/marketplace/mod.rs:47</sub> | `h.ListListingsEnvelope`<br><sub>openfoundry-go/services/federation-product-exchange-service/internal/server/server.go:77</sub> | implemented |
| `/v1/marketplace/listings` | POST | `handlers::publish::publish_listing`<br><sub>services/federation-product-exchange-service/src/marketplace/mod.rs:54</sub> | `h.CreateListing`<br><sub>openfoundry-go/services/federation-product-exchange-service/internal/server/server.go:78</sub> | implemented |
| `/v1/marketplace/listings/{id}` | GET | `handlers::browse::get_listing`<br><sub>services/federation-product-exchange-service/src/marketplace/mod.rs:48</sub> | `h.GetListing`<br><sub>openfoundry-go/services/federation-product-exchange-service/internal/server/server.go:79</sub> | implemented |
| `/v1/marketplace/listings/{id}` | PATCH | `handlers::publish::update_listing`<br><sub>services/federation-product-exchange-service/src/marketplace/mod.rs:58</sub> | `h.UpdateListing`<br><sub>openfoundry-go/services/federation-product-exchange-service/internal/server/server.go:80</sub> | implemented |
| `/v1/marketplace/listings/{id}/actions` | POST | `handlers::publish::include_action_in_product`<br><sub>services/federation-product-exchange-service/src/marketplace/mod.rs:66</sub> | `h.IncludeActionInProduct`<br><sub>openfoundry-go/services/federation-product-exchange-service/internal/server/server.go:83</sub> | implemented |
| `/v1/marketplace/listings/{id}/versions` | GET | `handlers::publish::list_versions`<br><sub>services/federation-product-exchange-service/src/marketplace/mod.rs:62</sub> | `h.ListVersions`<br><sub>openfoundry-go/services/federation-product-exchange-service/internal/server/server.go:81</sub> | implemented |
| `/v1/marketplace/listings/{id}/versions` | POST | `handlers::publish::publish_version`<br><sub>services/federation-product-exchange-service/src/marketplace/mod.rs:62</sub> | `h.PublishVersion`<br><sub>openfoundry-go/services/federation-product-exchange-service/internal/server/server.go:82</sub> | implemented |
| `/v1/marketplace/overview` | GET | `handlers::browse::get_overview`<br><sub>services/federation-product-exchange-service/src/marketplace/mod.rs:42</sub> | `h.GetOverview`<br><sub>openfoundry-go/services/federation-product-exchange-service/internal/server/server.go:75</sub> | implemented |
| `/v1/marketplace/products/from-dataset/{rid}` | POST | `handlers::dataset_product::create_from_dataset`<br><sub>services/federation-product-exchange-service/src/marketplace/mod.rs:93</sub> | `h.CreateDatasetProduct`<br><sub>openfoundry-go/services/federation-product-exchange-service/internal/server/server.go:87</sub> | implemented |
| `/v1/marketplace/products/{id}` | GET | `handlers::dataset_product::get_dataset_product`<br><sub>services/federation-product-exchange-service/src/marketplace/mod.rs:97</sub> | `h.GetDatasetProduct`<br><sub>openfoundry-go/services/federation-product-exchange-service/internal/server/server.go:88</sub> | implemented |
| `/v1/marketplace/products/{id}/install` | POST | `handlers::dataset_product::install_dataset_product`<br><sub>services/federation-product-exchange-service/src/marketplace/mod.rs:101</sub> | `h.InstallDatasetProduct`<br><sub>openfoundry-go/services/federation-product-exchange-service/internal/server/server.go:89</sub> | implemented |
| `/v1/marketplace/products/{id}/install:schedules` | POST | — | `h.MaterialiseInstallSchedules`<br><sub>openfoundry-go/services/federation-product-exchange-service/internal/server/server.go:91</sub> | implemented |
| `/v1/marketplace/products/{id}/schedules` | POST | — | `h.AddScheduleManifest`<br><sub>openfoundry-go/services/federation-product-exchange-service/internal/server/server.go:90</sub> | implemented |
| `/v1/marketplace/search` | GET | `handlers::browse::search_listings`<br><sub>services/federation-product-exchange-service/src/marketplace/mod.rs:52</sub> | `h.SearchListings`<br><sub>openfoundry-go/services/federation-product-exchange-service/internal/server/server.go:84</sub> | implemented |
| `/v1/products/from-dataset/{rid}` | POST | `handlers::dataset_product::create_from_dataset`<br><sub>services/federation-product-exchange-service/src/marketplace/mod.rs:81</sub> | `h.CreateDatasetProduct`<br><sub>openfoundry-go/services/federation-product-exchange-service/internal/server/server.go:98</sub> | implemented |
| `/v1/products/{id}` | GET | `handlers::dataset_product::get_dataset_product`<br><sub>services/federation-product-exchange-service/src/marketplace/mod.rs:85</sub> | `h.GetDatasetProduct`<br><sub>openfoundry-go/services/federation-product-exchange-service/internal/server/server.go:99</sub> | implemented |
| `/v1/products/{id}/install` | POST | `handlers::dataset_product::install_dataset_product`<br><sub>services/federation-product-exchange-service/src/marketplace/mod.rs:89</sub> | `h.InstallDatasetProduct`<br><sub>openfoundry-go/services/federation-product-exchange-service/internal/server/server.go:100</sub> | implemented |
| `/v1/products/{id}/install:schedules` | POST | `handlers::schedule_manifest::materialise_install_schedules`<br><sub>services/federation-product-exchange-service/src/marketplace/mod.rs:110</sub> | `h.MaterialiseInstallSchedules`<br><sub>openfoundry-go/services/federation-product-exchange-service/internal/server/server.go:102</sub> | implemented |
| `/v1/products/{id}/schedules` | POST | `handlers::schedule_manifest::add_schedule_manifest`<br><sub>services/federation-product-exchange-service/src/marketplace/mod.rs:106</sub> | `h.AddScheduleManifest`<br><sub>openfoundry-go/services/federation-product-exchange-service/internal/server/server.go:101</sub> | implemented |
