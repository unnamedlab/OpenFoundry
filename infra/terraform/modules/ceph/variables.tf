variable "namespace" {
  description = "Namespace where the Rook operator and CephCluster live."
  type        = string
  default     = "rook-ceph"
}

variable "app_namespace" {
  description = "Namespace that hosts the OpenFoundry workloads and the ObjectBucketClaims."
  type        = string
  default     = "openfoundry"
}

variable "create_app_namespace" {
  description = "Create the application namespace before applying ObjectBucketClaims. Set to false when it is managed elsewhere."
  type        = bool
  default     = false
}

variable "chart_repository" {
  description = "Helm repository hosting the rook-ceph operator chart. See https://charts.rook.io/release."
  type        = string
  default     = "https://charts.rook.io/release"
}

variable "chart_version" {
  description = "Version of the rook-ceph operator chart to install. Pin to a known-good release."
  type        = string
  default     = "v1.15.5"
}

variable "enable_monitoring" {
  description = "Expose ServiceMonitors so that Prometheus Operator can scrape Ceph metrics."
  type        = bool
  default     = true
}

variable "operator_values_override" {
  description = "Extra YAML appended to the rook-ceph chart values for environment-specific tweaks."
  type        = string
  default     = "{}"
}

variable "apply_cluster" {
  description = "Apply infra/k8s/platform/manifests/rook/cluster.yaml (the CephCluster CR)."
  type        = bool
  default     = true
}

variable "apply_object_store" {
  description = "Apply infra/k8s/platform/manifests/rook/objectstore.yaml (CephObjectStore + StorageClass)."
  type        = bool
  default     = true
}

variable "apply_buckets" {
  description = "Apply infra/k8s/platform/manifests/rook/bucket.yaml (ObjectBucketClaims for openfoundry-{datasets,models,iceberg})."
  type        = bool
  default     = true
}
