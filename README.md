# k8s-scheduler-sim

Simulate where pending pods would land, without a cluster. It reads a saved
snapshot of nodes and pods, applies the scheduler's filters, scores the nodes
with least-allocated, and prints the decision — or the reason every node was
rejected.

```console
$ k8s-scheduler-sim --from examples/capture.json
Scheduler simulation — examples/capture.json
Generated 2026-11-05 08:57 IST

4 nodes · 5 pending pods · 3 scheduled · 2 unschedulable

POD                            REQUESTS                   DECISION           SCORE
----------------------------------------------------------------------------------------
data/needs-ssd                 cpu 1, mem 2.0Gi           unschedulable      -
  cpu-a: nodeSelector disk=ssd does not match
  cpu-b: node is unschedulable
  gpu-a: nodeSelector disk=ssd does not match
  gpu-b: nodeSelector disk=ssd does not match
default/batch                  cpu 2, mem 4.0Gi           cpu-a              62
ml/inference                   cpu 2, mem 8.0Gi, gpu 1    gpu-b              74
ml/train-new                   cpu 4, mem 16.0Gi, gpu 1   gpu-b              41
```

## How a decision is made

For each pending pod, in `namespace/name` order:

1. **Filter** every node:
   - `spec.unschedulable` is rejected.
   - every `spec.nodeSelector` key must equal the node label.
   - `NoSchedule` and `NoExecute` taints need a matching toleration. `Exists`
     matches any value; `Equal` (the default operator) matches the value.
   - CPU, memory and GPU requests must fit in `allocatable - used`.
2. **Score** the nodes that pass, using the Kubernetes least-allocated idea:
   the average percentage of CPU, memory and GPU still free *after* the pod
   lands. The highest score wins. Ties go to the node name that sorts first.
3. **Place** the pod, add its requests to that node's used total, and move to
   the next pod.

Pods that already have `spec.nodeName` count against the node even when
`--namespace` filters the simulation, because node pressure is cluster-wide.

## Install

```bash
go install github.com/Ankitavasudev/k8s-scheduler-sim@latest
```

Requires Go 1.21 or newer.

## Usage

```bash
# A saved snapshot
k8s-scheduler-sim --from examples/capture.json

# Raw kubectl output
kubectl get nodes -o json > nodes.json
kubectl get pods -A -o json > pods.json
k8s-scheduler-sim --nodes nodes.json --pods pods.json

# The current kubectl context
k8s-scheduler-sim

# Simulate pending pods from one namespace
k8s-scheduler-sim --namespace ml

# Other formats
k8s-scheduler-sim --from examples/capture.json -o json
k8s-scheduler-sim --from examples/capture.json -o csv
k8s-scheduler-sim --from examples/capture.json -o html > scheduling.html
```

## Capture format

One JSON file with `nodes` and `pods` arrays in the Kubernetes API shape.
Only the fields used by the simulator are read, so extra fields are ignored:

```json
{
  "nodes": [
    {
      "metadata": { "name": "gpu-a", "labels": { "pool": "gpu" } },
      "spec": { "taints": [{ "key": "dedicated", "value": "gpu", "effect": "NoSchedule" }] },
      "status": { "allocatable": { "cpu": "16", "memory": "64Gi", "nvidia.com/gpu": "2" } }
    }
  ],
  "pods": [
    {
      "metadata": { "name": "train", "namespace": "ml" },
      "spec": {
        "nodeSelector": { "pool": "gpu" },
        "tolerations": [{ "key": "dedicated", "operator": "Equal", "value": "gpu", "effect": "NoSchedule" }],
        "containers": [
          {
            "name": "trainer",
            "resources": { "requests": { "cpu": "4", "memory": "16Gi", "nvidia.com/gpu": "1" } }
          }
        ]
      }
    }
  ]
}
```

A pod with no `nodeName` is treated as pending and simulated. A pod with a
`nodeName` is treated as already placed.

## Output

| Format | Use |
| --- | --- |
| `text` (default) | Placements, rejection reasons, final node usage |
| `json` | Full report with decisions, scores and per-node capacity |
| `csv` | One row per simulated pod |
| `html` | Single file with inline CSS and usage bars, no external assets |

## What it does not model

This is a filter-and-score simulation, not kube-scheduler itself. It does not
implement:

- pod affinity/anti-affinity, topology spread constraints, or inter-pod
  affinity
- PriorityClasses, preemption, or queueing
- image locality, volume zone/region rules, or CSI topology
- plugin ordering, `PreferNoSchedule`, or `kubelet` admission
- rescheduling of pods that are already placed

If a pod's placement depends on one of those, the simulator can disagree with
the real scheduler. The README would rather say so than pretend otherwise.

## Demo

A generated report for the example capture is published at
<https://ankitavasudev.github.io/k8s-scheduler-sim/>.

## License

MIT
