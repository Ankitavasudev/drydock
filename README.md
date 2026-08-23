# k8s-scheduler-sim

Kubernetes scheduler simulator for visualizing scheduling decisions, testing custom policies, and analyzing node allocation patterns. Useful for understanding how kube-scheduler, Volcano, and other schedulers make decisions.

## Features

- Visualize pod scheduling decisions
- Test custom scheduling policies
- Analyze node resource allocation
- Simulate cluster scaling
- Support for kube-scheduler, Volcano, and custom schedulers
- Export scheduling history

## Installation

```bash
go install github.com/Ankitavasudev/k8s-scheduler-sim@latest
```

## Usage

```bash
# Start simulator
k8s-scheduler-sim serve

# Analyze existing cluster
k8s-scheduler-sim analyze

# Test scheduling policy
k8s-scheduler-sim test --policy custom-policy.yaml

# Simulate pod scheduling
k8s-scheduler-sim simulate --pod test-pod.yaml

# Export scheduling decisions
k8s-scheduler-sim export --format json --output decisions.json
```

## Example Output

```
SCHEDULING DECISIONS
=========================================================
POD NAME        NODE          SCORE   REASON
---------------------------------------------------------
nginx-1         node-1        95      LeastRequestedPriority
nginx-2         node-2        87      BalancedResourceAllocation
redis-1         node-3        92      MostRequestedPriority
training-job    gpu-node-1    100     NodeAffinityMatch
=========================================================
Total Pods: 4 | Scheduled: 4 | Pending: 0
```

## Supported Schedulers

| Scheduler | Support Level | Features |
|-----------|---------------|----------|
| kube-scheduler | Full | All extension points |
| Volcano | Full | Queue, priority, gang |
| Custom | Partial | Basic predicates |

## Building

```bash
git clone https://github.com/Ankitavasudev/k8s-scheduler-sim.git
cd k8s-scheduler-sim
go build -o k8s-scheduler-sim .
```

## License

MIT
