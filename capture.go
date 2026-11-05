package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Capture is one snapshot: the nodes, the pods already placed, and the pending
// pods to simulate. A NodeList and PodList can be combined in the same shape.
type Capture struct {
	Nodes []Node `json:"nodes"`
	Pods  []Pod  `json:"pods"`
}

type ObjectMeta struct {
	Name      string            `json:"name"`
	Namespace string            `json:"namespace"`
	Labels    map[string]string `json:"labels"`
}

type Taint struct {
	Key    string `json:"key"`
	Value  string `json:"value"`
	Effect string `json:"effect"`
}

type Toleration struct {
	Key      string `json:"key"`
	Operator string `json:"operator"`
	Value    string `json:"value"`
	Effect   string `json:"effect"`
}

type Node struct {
	Metadata ObjectMeta `json:"metadata"`
	Spec     struct {
		Unschedulable bool    `json:"unschedulable"`
		Taints        []Taint `json:"taints"`
	} `json:"spec"`
	Status struct {
		Allocatable map[string]string `json:"allocatable"`
		Capacity    map[string]string `json:"capacity"`
	} `json:"status"`
}

type Pod struct {
	Metadata ObjectMeta `json:"metadata"`
	Spec     struct {
		NodeName     string            `json:"nodeName"`
		NodeSelector map[string]string `json:"nodeSelector"`
		Tolerations  []Toleration      `json:"tolerations"`
		Containers   []Container       `json:"containers"`
	} `json:"spec"`
	Status struct {
		Phase string `json:"phase"`
	} `json:"status"`
}

type Container struct {
	Name      string `json:"name"`
	Resources struct {
		Requests map[string]string `json:"requests"`
		Limits   map[string]string `json:"limits"`
	} `json:"resources"`
}

func parseNodes(data []byte) ([]Node, error) {
	var list struct {
		Items []Node `json:"items"`
	}
	if err := json.Unmarshal(data, &list); err == nil && list.Items != nil {
		return list.Items, nil
	}
	var wrapped struct {
		Nodes []Node `json:"nodes"`
	}
	if err := json.Unmarshal(data, &wrapped); err == nil && wrapped.Nodes != nil {
		return wrapped.Nodes, nil
	}
	var single Node
	if err := json.Unmarshal(data, &single); err == nil && single.Metadata.Name != "" {
		return []Node{single}, nil
	}
	return nil, errors.New("does not look like a NodeList, a Node, or a capture with a nodes array")
}

func parsePods(data []byte) ([]Pod, error) {
	var list struct {
		Items []Pod `json:"items"`
	}
	if err := json.Unmarshal(data, &list); err == nil && list.Items != nil {
		return list.Items, nil
	}
	var wrapped struct {
		Pods []Pod `json:"pods"`
	}
	if err := json.Unmarshal(data, &wrapped); err == nil && wrapped.Pods != nil {
		return wrapped.Pods, nil
	}
	var single Pod
	if err := json.Unmarshal(data, &single); err == nil && single.Metadata.Name != "" {
		return []Pod{single}, nil
	}
	return nil, errors.New("does not look like a PodList, a Pod, or a capture with a pods array")
}

func loadCapture(path string) (Capture, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Capture{}, err
	}
	var capture Capture
	if err := json.Unmarshal(data, &capture); err != nil {
		return Capture{}, fmt.Errorf("%s is not a capture: %w", path, err)
	}
	if capture.Nodes == nil && capture.Pods == nil {
		return Capture{}, fmt.Errorf("%s is not a capture: expected nodes and pods arrays", path)
	}
	return capture, nil
}

func loadNodes(path string) ([]Node, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	nodes, err := parseNodes(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return nodes, nil
}

func loadPods(path string) ([]Pod, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	pods, err := parsePods(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return pods, nil
}

func nodeResources(node Node) resourceSet {
	source := node.Status.Allocatable
	if len(source) == 0 {
		source = node.Status.Capacity
	}
	return resourceSet{
		CPU:    quantityMillis(source["cpu"]),
		Memory: quantityBytes(source["memory"]),
		GPUs:   gpuCount(source),
	}
}

func gpuCount(resources map[string]string) int {
	total := 0
	for key, value := range resources {
		if !isGPUResource(key) {
			continue
		}
		if n, err := strconv.Atoi(strings.TrimSpace(value)); err == nil && n > 0 {
			total += n
		}
	}
	return total
}

func isGPUResource(key string) bool {
	k := strings.ToLower(strings.TrimSpace(key))
	if strings.HasPrefix(k, "gpu.intel.com/") {
		return true
	}
	name := k
	if slash := strings.LastIndex(k, "/"); slash >= 0 {
		name = k[slash+1:]
	}
	return name == "gpu" || strings.HasPrefix(name, "mig-") || strings.HasPrefix(name, "ascend")
}

// podRequests sums every container's requests, falling back to limits. A pod
// is the unit the scheduler places, so the sum is what must fit on a node.
func podRequests(pod Pod) resourceSet {
	total := resourceSet{}
	for _, container := range pod.Spec.Containers {
		total.CPU += quantityMillis(pick(container.Resources.Requests, container.Resources.Limits, "cpu"))
		total.Memory += quantityBytes(pick(container.Resources.Requests, container.Resources.Limits, "memory"))
		gpus := gpuCount(container.Resources.Requests)
		if gpus == 0 {
			gpus = gpuCount(container.Resources.Limits)
		}
		total.GPUs += gpus
	}
	return total
}

func pick(requests, limits map[string]string, key string) string {
	if value := strings.TrimSpace(requests[key]); value != "" {
		return value
	}
	return strings.TrimSpace(limits[key])
}

func tolerates(tolerations []Toleration, taint Taint) bool {
	for _, toleration := range tolerations {
		if toleration.Effect != "" && toleration.Effect != taint.Effect {
			continue
		}
		if toleration.Key == "" {
			return true
		}
		if toleration.Key != taint.Key {
			continue
		}
		switch toleration.Operator {
		case "Exists":
			return true
		case "Equal", "":
			if toleration.Value == taint.Value {
				return true
			}
		}
	}
	return false
}
