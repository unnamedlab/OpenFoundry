output "namespace" {
  description = "Namespace where the Rook operator and CephCluster were deployed."
  value       = kubernetes_namespace_v1.rook_ceph.metadata[0].name
}

output "operator_release_name" {
  description = "Helm release name of the rook-ceph operator chart."
  value       = helm_release.rook_ceph.name
}

output "operator_chart_version" {
  description = "Version of the rook-ceph operator chart that was installed."
  value       = helm_release.rook_ceph.version
}

output "object_store_name" {
  description = "Name of the CephObjectStore CR that backs the S3 endpoint."
  value       = "openfoundry"
}

output "s3_endpoint" {
  description = "In-cluster S3 endpoint exposed by the RGW Service. Use this for OBJECT_STORE_ENDPOINT."
  value       = "http://rook-ceph-rgw-openfoundry.${kubernetes_namespace_v1.rook_ceph.metadata[0].name}.svc:80"
}

output "bucket_claims" {
  description = "Names of the ObjectBucketClaims provisioned for OpenFoundry."
  value       = keys(local.bucket_by_name)
}
