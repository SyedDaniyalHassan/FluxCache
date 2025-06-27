# FluxCache

A high-performance, high-availability distributed in-memory cache system written in Go. Inspired by Redis, designed for modern cloud-native environments.

## Features
- Distributed storage with consistent hashing
- Basic data types: strings, numbers, JSON objects
- TTL (time-to-live) support
- Node replication and automatic failover
- REST API and TCP protocol
- Node discovery, health checking, and load balancing
- Docker containerization
- Monitoring dashboard with real-time metrics

## Architecture
- Multi-node cluster (3-5 nodes recommended)
- In-memory storage with optional persistence
- Go concurrency (goroutines, channels)
- Prometheus metrics and web dashboard

## Development Phases
1. **Single node**: Basic GET/SET/DELETE operations
2. **Cluster**: Consistent hashing, multi-node support
3. **Replication & Failover**: Redundancy and HA
4. **Monitoring & Deployment**: Metrics, dashboard, Docker

## Project Structure
- `cmd/` - Entrypoints (cache node server, client CLI)
- `pkg/` - Core logic (cache, cluster, protocol, monitoring)
- `internal/` - Utilities and internal helpers
- `configs/` - Configuration files
- `Docker/` - Containerization files

## Quick Start
_Coming soon_

## Monitoring & Metrics
- Prometheus metrics exposed at `/metrics`
- Docker Compose includes Prometheus for cluster monitoring
- Example metrics: request count, error count, latency, node health

## Running with Docker Compose
```sh
docker-compose -f Docker/docker-compose.yml up --build
```

## Performance Benchmarking
- Use `hey`, `wrk`, or `ab` to benchmark GET/SET/DELETE endpoints
- Example:
  ```sh
  hey -n 10000 -c 100 -m POST -H "Content-Type: application/json" -d '{"key":"foo","value":"bar"}' http://localhost:8081/set
  ```
- Prometheus + Grafana can be used for advanced dashboards

## Dashboard
- Prometheus UI: http://localhost:9090
- Add Grafana for custom dashboards (optional)

## Documentation
- See code comments and this README for architecture and usage
- `/nodes`, `/health`, `/metrics` endpoints for introspection 