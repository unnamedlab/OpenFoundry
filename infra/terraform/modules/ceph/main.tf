terraform {
  required_version = ">= 1.7.0"

  required_providers {
    helm = {
      source  = "hashicorp/helm"
      version = ">= 2.13.0"
    }
    kubernetes = {
      source  = "hashicorp/kubernetes"
      version = ">= 2.30.0"
    }
  }
}

# ---------------------------------------------------------------------------
# Namespace for the Rook operator and the Ceph cluster.
# ---------------------------------------------------------------------------
resource "kubernetes_namespace_v1" "rook_ceph" {
  metadata {
    name = var.namespace
    labels = {
      "app.kubernetes.io/managed-by" = "terraform"
      "app.kubernetes.io/part-of"    = "openfoundry"
      "openfoundry.io/component"     = "ceph"
    }
  }
}

# ---------------------------------------------------------------------------
# Rook-Ceph operator + CRDs (official chart).
#
# Coordinates: https://charts.rook.io/release  (chart name: rook-ceph)
# The operator chart only installs the operator and CRDs; the CephCluster /
# CephObjectStore / OBC instances are applied separately as raw manifests
# below so that the desired topology lives in source control under
# infra/k8s/platform/manifests/rook.
# ---------------------------------------------------------------------------
resource "helm_release" "rook_ceph" {
  name       = "rook-ceph"
  namespace  = kubernetes_namespace_v1.rook_ceph.metadata[0].name
  repository = var.chart_repository
  chart      = "rook-ceph"
  version    = var.chart_version

  # Wait for the operator Deployment to become Ready before we try to apply
  # the CephCluster CR (the CRD must be installed first).
  wait             = true
  atomic           = true
  cleanup_on_fail  = true
  create_namespace = false
  timeout          = 600

  values = [
    yamlencode({
      crds = {
        enabled = true
      }
      enableDiscoveryDaemon         = true
      currentNamespaceOnly          = false
      monitoring                    = { enabled = var.enable_monitoring }
      csi = {
        enableRbdDriver    = true
        enableCephfsDriver = true
      }
      resources = {
        requests = { cpu = "200m", memory = "256Mi" }
        limits   = { cpu = "500m", memory = "512Mi" }
      }
    }),
    var.operator_values_override,
  ]
}

# ---------------------------------------------------------------------------
# Render the in-tree manifests so they can be applied via kubernetes_manifest.
#
# Each YAML file under infra/k8s/platform/manifests/rook may contain multiple documents
# (e.g. objectstore.yaml has the CephObjectStore + a StorageClass, and
# bucket.yaml has three OBCs). We split on the YAML separator and decode
# each document into a HCL map for kubernetes_manifest.
# ---------------------------------------------------------------------------
locals {
  manifests_dir = "${path.module}/../../../k8s/platform/manifests/rook"

  # CephCluster (single doc)
  cluster_docs = [
    for d in split("\n---", file("${local.manifests_dir}/cluster.yaml")) :
    yamldecode(d) if trimspace(d) != "" && trimspace(d) != "---"
  ]

  # CephObjectStore + StorageClass
  objectstore_docs = [
    for d in split("\n---", file("${local.manifests_dir}/objectstore.yaml")) :
    yamldecode(d) if trimspace(d) != "" && trimspace(d) != "---"
  ]

  # OBCs (datasets / models / iceberg)
  bucket_docs = [
    for d in split("\n---", file("${local.manifests_dir}/bucket.yaml")) :
    yamldecode(d) if trimspace(d) != "" && trimspace(d) != "---"
  ]

  cluster_by_kind = {
    for doc in local.cluster_docs : doc.kind => doc
  }
  objectstore_by_kind = {
    for doc in local.objectstore_docs : doc.kind => doc
  }
  bucket_by_name = {
    for doc in local.bucket_docs : doc.metadata.name => doc
  }
}

resource "kubernetes_manifest" "ceph_cluster" {
  for_each = { for k, v in local.cluster_by_kind : k => v if var.apply_cluster }

  manifest = each.value

  field_manager {
    name            = "openfoundry-terraform"
    force_conflicts = true
  }

  # Wait for the Ceph cluster to settle before downstream resources race for
  # the CRDs / pools.
  wait {
    fields = {
      "status.phase" = "Ready"
    }
  }
  timeouts {
    create = "30m"
    update = "20m"
  }

  depends_on = [helm_release.rook_ceph]
}

resource "kubernetes_manifest" "ceph_objectstore" {
  for_each = { for k, v in local.objectstore_by_kind : k => v if var.apply_object_store }

  manifest = each.value

  field_manager {
    name            = "openfoundry-terraform"
    force_conflicts = true
  }

  depends_on = [kubernetes_manifest.ceph_cluster]
}

# ObjectBucketClaims live in the OpenFoundry application namespace, not in
# rook-ceph. Make sure that namespace exists before applying them.
resource "kubernetes_namespace_v1" "openfoundry" {
  count = var.apply_buckets && var.create_app_namespace ? 1 : 0

  metadata {
    name = var.app_namespace
    labels = {
      "app.kubernetes.io/part-of" = "openfoundry"
    }
  }
}

resource "kubernetes_manifest" "ceph_buckets" {
  for_each = { for k, v in local.bucket_by_name : k => v if var.apply_buckets }

  manifest = each.value

  field_manager {
    name            = "openfoundry-terraform"
    force_conflicts = true
  }

  depends_on = [
    kubernetes_manifest.ceph_objectstore,
    kubernetes_namespace_v1.openfoundry,
  ]
}
