# Requirements & Architecture

Review these infrastructure and hardware recommendations before deploying Omniflux to production.

## Software Prerequisites

| Component | Minimum Version | Recommended Version | Role |
| :--- | :--- | :--- | :--- |
| **Go** | 1.27+ | Latest stable | Building or running from source. |
| **Node.js** | 20+ | 22 LTS | Building the Vue 3 management console. |
| **pnpm** | 10.5+ | 12.18+ | Frontend package manager. |
| **PostgreSQL** | 16+ | 17 (Alpine / Managed RDS) | Primary database for metadata, encrypted keys, and logs. |
| **Redis** | 7.0+ | 8.0+ | Distributed rate limiting and sliding window counters. |
| **Docker** | 20.10+ | Latest Docker CE | Recommended container runtime. |

## Sizing Guidelines

Omniflux's data plane is written in Go, offering high throughput and low memory overhead.

| Workload | Typical RPS | Omniflux Nodes | PostgreSQL Spec | Redis Spec |
| :--- | :--- | :--- | :--- | :--- |
| **Evaluation / Dev** | < 20 RPS | 1 node (1 vCPU, 2GB RAM) | 1 vCPU, 2GB RAM, 20GB SSD | 512MB RAM |
| **Mid-Size Production**| 20 ~ 200 RPS | 2 nodes (2 vCPU, 4GB RAM) | 2 vCPU, 4GB RAM, 100GB SSD | 2GB RAM |
| **High-Throughput** | 200 ~ 2000+ RPS | 3+ nodes (4 vCPU, 8GB RAM) | 4 vCPU, 16GB+ RAM, Read replica | 4GB RAM, HA cluster |

## Recommended Production Topology

Deploy Omniflux within a private VPC behind a reverse proxy:

![Omniflux Recommended Production Topology](/assets/production-topology.en-US.png)

- **Reverse Proxy**: Handles TLS termination and disables buffering (`proxy_buffering off`) for Server-Sent Events (SSE).
- **Single Binary**: Each instance serves both `/v1/*` data plane and `/admin/` console, simplifying horizontal scaling.
- **At-Rest Encryption**: Upstream API keys are encrypted with AES-256-GCM before writing to PostgreSQL.
