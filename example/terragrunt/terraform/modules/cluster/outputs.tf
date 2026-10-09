output "distro" {
  value       = cargoship_cluster.this.distro
  description = "The engine the package carried, read from the package rather than stated twice"
}

output "engine_version" {
  value       = cargoship_cluster.this.engine_version
  description = "The engine version the package carried"
}

output "nodes" {
  value       = cargoship_cluster.this.nodes
  description = "What each host reported during the run, controllers first"
}

output "kubeconfig" {
  value       = cargoship_cluster.this.kubeconfig
  description = "The cluster's admin credentials, empty unless export_kubeconfig was set"
  sensitive   = true
}
