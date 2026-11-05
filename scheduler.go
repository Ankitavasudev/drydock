package main

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

type Usage struct {
	Capacity resourceSet `json:"capacity"`
	Used     resourceSet `json:"used"`
	Free     resourceSet `json:"free"`
}

type NodeReport struct {
	Name          string `json:"name"`
	Unschedulable bool   `json:"unschedulable"`
	Usage         Usage  `json:"usage"`
	Pods          int    `json:"pods"`
}

type Decision struct {
	Namespace string      `json:"namespace"`
	Name      string      `json:"name"`
	Requests  resourceSet `json:"requests"`
	Node      string      `json:"node,omitempty"`
	Score     int         `json:"score"`
	Reasons   []string    `json:"reasons,omitempty"`
}

type Summary struct {
	Nodes         int `json:"nodes"`
	Pending       int `json:"pending"`
	Scheduled     int `json:"scheduled"`
	Unschedulable int `json:"unschedulable"`
}

type Report struct {
	Generated string       `json:"generated"`
	Source    string       `json:"source"`
	Namespace string       `json:"namespace,omitempty"`
	Summary   Summary      `json:"summary"`
	Decisions []Decision   `json:"decisions"`
	Nodes     []NodeReport `json:"nodes"`
}

func podKey(namespace, name string) string { return namespace + "/" + name }

func nodeOrder(nodes []Node) []string {
	names := make([]string, 0, len(nodes))
	for _, node := range nodes {
		names = append(names, node.Metadata.Name)
	}
	sort.Strings(names)
	return names
}

// score is the Kubernetes LeastAllocated idea: the average percentage of each
// resource still free after the pod would land. Higher is better, so the pod
// spreads across nodes instead of stacking on the emptiest-looking one.
func score(capacity, usedAfter resourceSet) int {
	total, count := 0, 0
	add := func(free, cap int64) {
		if cap <= 0 {
			return
		}
		value := int(free * 100 / cap)
		if value < 0 {
			value = 0
		}
		if value > 100 {
			value = 100
		}
		total += value
		count++
	}
	add(capacity.CPU-usedAfter.CPU, capacity.CPU)
	add(capacity.Memory-usedAfter.Memory, capacity.Memory)
	if capacity.GPUs > 0 {
		add(int64(capacity.GPUs-usedAfter.GPUs), int64(capacity.GPUs))
	}
	if count == 0 {
		return 0
	}
	return total / count
}

func selectorMatches(selector map[string]string, labels map[string]string) (bool, string) {
	for key, want := range selector {
		if labels[key] != want {
			return false, fmt.Sprintf("nodeSelector %s=%s does not match", key, want)
		}
	}
	return true, ""
}

func blockingTaint(node Node, pod Pod) (string, bool) {
	for _, taint := range node.Spec.Taints {
		if taint.Effect != "NoSchedule" && taint.Effect != "NoExecute" {
			continue
		}
		if !tolerates(pod.Spec.Tolerations, taint) {
			return fmt.Sprintf("taint %s=%s:%s not tolerated", taint.Key, taint.Value, taint.Effect), true
		}
	}
	return "", false
}

func fitReason(node Node, capacity, used, requests resourceSet) string {
	reasons := []string{}
	if requests.CPU > capacity.CPU-used.CPU {
		reasons = append(reasons, fmt.Sprintf("insufficient cpu (needs %s, free %s)", formatCPU(requests.CPU), formatCPU(capacity.CPU-used.CPU)))
	}
	if requests.Memory > capacity.Memory-used.Memory {
		reasons = append(reasons, fmt.Sprintf("insufficient memory (needs %s, free %s)", formatMemory(requests.Memory), formatMemory(capacity.Memory-used.Memory)))
	}
	if requests.GPUs > capacity.GPUs-used.GPUs {
		reasons = append(reasons, fmt.Sprintf("insufficient gpu (needs %d, free %d)", requests.GPUs, capacity.GPUs-used.GPUs))
	}
	return strings.Join(reasons, ", ")
}

// buildReport is pure and deterministic: nodes and pending pods are sorted
// before scheduling, so the same capture always produces the same placements.
func buildReport(capture Capture, namespace string, now time.Time) Report {
	report := Report{
		Namespace: namespace,
		Decisions: []Decision{},
		Nodes:     []NodeReport{},
	}

	order := nodeOrder(capture.Nodes)
	byName := make(map[string]*Node, len(capture.Nodes))
	states := make(map[string]*NodeReport, len(capture.Nodes))
	for _, node := range capture.Nodes {
		copy := node
		byName[node.Metadata.Name] = &copy
		states[node.Metadata.Name] = &NodeReport{
			Name:          node.Metadata.Name,
			Unschedulable: node.Spec.Unschedulable,
			Usage: Usage{
				Capacity: nodeResources(node),
			},
		}
	}

	// Pods that already have a node consume capacity. They are counted even
	// when a namespace filter is set, because node pressure is cluster-wide.
	for _, pod := range capture.Pods {
		if pod.Spec.NodeName == "" {
			continue
		}
		state := states[pod.Spec.NodeName]
		if state == nil {
			continue
		}
		state.Usage.Used = state.Usage.Used.add(podRequests(pod))
		state.Pods++
	}

	pending := make([]Pod, 0, len(capture.Pods))
	for _, pod := range capture.Pods {
		if pod.Spec.NodeName != "" {
			continue
		}
		if namespace != "" && pod.Metadata.Namespace != namespace {
			continue
		}
		pending = append(pending, pod)
	}
	sort.SliceStable(pending, func(i, j int) bool {
		return podKey(pending[i].Metadata.Namespace, pending[i].Metadata.Name) <
			podKey(pending[j].Metadata.Namespace, pending[j].Metadata.Name)
	})

	report.Summary.Pending = len(pending)
	for _, pod := range pending {
		requests := podRequests(pod)
		decision := Decision{
			Namespace: pod.Metadata.Namespace,
			Name:      pod.Metadata.Name,
			Requests:  requests,
		}

		best := ""
		bestScore := -1
		reasons := []string{}
		for _, name := range order {
			node := byName[name]
			state := states[name]
			if node.Spec.Unschedulable {
				reasons = append(reasons, name+": node is unschedulable")
				continue
			}
			if ok, reason := selectorMatches(pod.Spec.NodeSelector, node.Metadata.Labels); !ok {
				reasons = append(reasons, name+": "+reason)
				continue
			}
			if reason, blocked := blockingTaint(*node, pod); blocked {
				reasons = append(reasons, name+": "+reason)
				continue
			}
			if reason := fitReason(*node, state.Usage.Capacity, state.Usage.Used, requests); reason != "" {
				reasons = append(reasons, name+": "+reason)
				continue
			}
			after := state.Usage.Used.add(requests)
			candidate := score(state.Usage.Capacity, after)
			if candidate > bestScore {
				best = name
				bestScore = candidate
			}
		}

		if best == "" {
			decision.Reasons = reasons
			report.Summary.Unschedulable++
		} else {
			state := states[best]
			state.Usage.Used = state.Usage.Used.add(requests)
			state.Pods++
			decision.Node = best
			decision.Score = bestScore
			report.Summary.Scheduled++
		}
		report.Decisions = append(report.Decisions, decision)
	}

	for _, name := range order {
		state := states[name]
		state.Usage.Free = resourceSet{
			CPU:    max64(0, state.Usage.Capacity.CPU-state.Usage.Used.CPU),
			Memory: max64(0, state.Usage.Capacity.Memory-state.Usage.Used.Memory),
			GPUs:   maxInt(0, state.Usage.Capacity.GPUs-state.Usage.Used.GPUs),
		}
		report.Nodes = append(report.Nodes, *state)
	}
	report.Summary.Nodes = len(report.Nodes)
	return report
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func reasonText(reasons []string) string {
	if len(reasons) == 0 {
		return "no node passed the filters"
	}
	return strings.Join(reasons, "; ")
}
