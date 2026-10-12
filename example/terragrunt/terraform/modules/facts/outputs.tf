output "nodes" {
  value       = data.cargoship_cluster_facts.this.nodes
  description = "What each host reported: operating system, version, architecture, hostname, private address, and the engine version it is running"
}

output "engine_versions" {
  value       = { for node in data.cargoship_cluster_facts.this.nodes : node.hostname => node.engine_version }
  description = "The engine version per host, keyed by the hostname each one reported. v0.0.0 is a host running no engine"
}
