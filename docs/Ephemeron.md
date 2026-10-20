# Ephemeron


```
               [CI System / stub CLI]
                     │  (Lease API: create BuildAgentLease CR)
                     ▼
┌──────────────────────────── Kubernetes Cluster────────────────────────────┐
│                                                                            │
│  [BuildAgentPool CR] ──watch──▶ ┌──────────────────────┐                   │
│  [BuildAgentLease CR] ─watch──▶ │   Forge Controller   │──creates──▶ Agent │
│                                 │  (pool + lease       │             Pods  │
│                                 │   reconcilers)       │               │   │
│                                 └──────────┬───────────┘               │   │
│                                            │ placement hints /         │   │
│                                            ▼ score requests            ▼   │
│  ┌──────────────────┐          ┌──────────────────────┐        [Node A]    │
│  │ Cache Inventory  │─reports─▶│  Locality Scheduler   │        hot cache
  │
│  │ DaemonSet        │  (Node   │  (Score plugin or     │        [Node B]   │
│  │ (per-node warmth)│  CacheProfile CR) extender)      │        cold       │
│  └──────────────────┘          └──────────────────────┘                    │
│                                                                            │
│  [Pool Autoscaler] ◀── lease queue depth / P95 wait (custom metrics)       │
│  [OTel Collector] ◀── traces + metrics from all components                 │
└────────────────────────────────────────────────────────────────────────────┘
```
