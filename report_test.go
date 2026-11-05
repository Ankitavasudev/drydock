package main

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"
)

func testNode(name string, cpu int64, memory int64, gpus int, labels map[string]string) Node {
	node := Node{Metadata: ObjectMeta{Name: name, Labels: labels}}
	node.Status.Allocatable = map[string]string{
		"cpu":    formatCPU(cpu),
		"memory": strconv.FormatInt(memory, 10),
	}
	if gpus > 0 {
		node.Status.Allocatable["nvidia.com/gpu"] = strconv.Itoa(gpus)
	}
	return node
}

func testPod(namespace, name, node string, cpu int64, memory int64, gpus int) Pod {
	pod := Pod{Metadata: ObjectMeta{Namespace: namespace, Name: name}}
	pod.Spec.NodeName = node
	pod.Spec.Containers = []Container{{Name: "main"}}
	pod.Spec.Containers[0].Resources.Requests = map[string]string{
		"cpu":    formatCPU(cpu),
		"memory": strconv.FormatInt(memory, 10),
	}
	if gpus > 0 {
		pod.Spec.Containers[0].Resources.Requests["nvidia.com/gpu"] = strconv.Itoa(gpus)
	}
	return pod
}

func TestQuantityParsing(t *testing.T) {
	cpu := map[string]int64{"": 0, "2": 2000, "500m": 500, "0.5": 500, "bad": 0}
	for input, want := range cpu {
		if got := quantityMillis(input); got != want {
			t.Errorf("quantityMillis(%q) = %d, want %d", input, got, want)
		}
	}
	memory := map[string]int64{"": 0, "512Mi": 512 * 1024 * 1024, "1Gi": 1024 * 1024 * 1024, "2G": 2e9, "broken": 0}
	for input, want := range memory {
		if got := quantityBytes(input); got != want {
			t.Errorf("quantityBytes(%q) = %d, want %d", input, got, want)
		}
	}
}

func TestFormatting(t *testing.T) {
	if got := formatCPU(2000); got != "2" {
		t.Errorf("formatCPU(2000) = %q", got)
	}
	if got := formatCPU(500); got != "0.5" {
		t.Errorf("formatCPU(500) = %q", got)
	}
	if got := formatMemory(2 * 1024 * 1024 * 1024); got != "2.0Gi" {
		t.Errorf("formatMemory(2Gi) = %q", got)
	}
	if got := formatMemory(512 * 1024 * 1024); got != "512Mi" {
		t.Errorf("formatMemory(512Mi) = %q", got)
	}
}

func TestPodRequestsSumsContainersAndFallsBackToLimits(t *testing.T) {
	pod := testPod("ml", "p", "", 0, 0, 0)
	pod.Spec.Containers = []Container{{Name: "a"}, {Name: "b"}}
	pod.Spec.Containers[0].Resources.Limits = map[string]string{"cpu": "1", "memory": "1Gi"}
	pod.Spec.Containers[1].Resources.Requests = map[string]string{"cpu": "2", "memory": "2Gi"}
	pod.Spec.Containers[1].Resources.Limits = map[string]string{"nvidia.com/gpu": "1"}
	got := podRequests(pod)
	if got.CPU != 3000 || got.Memory != 3*1024*1024*1024 || got.GPUs != 1 {
		t.Errorf("podRequests() = %+v", got)
	}
}

func TestTolerations(t *testing.T) {
	taint := Taint{Key: "dedicated", Value: "gpu", Effect: "NoSchedule"}
	cases := []struct {
		name        string
		tolerations []Toleration
		want        bool
	}{
		{"none", nil, false},
		{"equal", []Toleration{{Key: "dedicated", Operator: "Equal", Value: "gpu", Effect: "NoSchedule"}}, true},
		{"wrong value", []Toleration{{Key: "dedicated", Operator: "Equal", Value: "cpu", Effect: "NoSchedule"}}, false},
		{"exists", []Toleration{{Key: "dedicated", Operator: "Exists", Effect: "NoSchedule"}}, true},
		{"wrong effect", []Toleration{{Key: "dedicated", Operator: "Exists", Effect: "NoExecute"}}, false},
		{"all taints", []Toleration{{Operator: "Exists"}}, true},
	}
	for _, c := range cases {
		if got := tolerates(c.tolerations, taint); got != c.want {
			t.Errorf("%s: tolerates() = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestBuildReportPlacesOnLeastAllocatedNode(t *testing.T) {
	capture := Capture{
		Nodes: []Node{
			testNode("a", 4000, 8*1024*1024*1024, 0, nil),
			testNode("b", 4000, 8*1024*1024*1024, 0, nil),
		},
		Pods: []Pod{
			testPod("ml", "existing", "a", 1000, 2*1024*1024*1024, 0),
			testPod("ml", "pending", "", 1000, 1*1024*1024*1024, 0),
		},
	}
	report := buildReport(capture, "", time.Now())
	if report.Summary.Scheduled != 1 || report.Summary.Unschedulable != 0 {
		t.Fatalf("summary = %+v", report.Summary)
	}
	if report.Decisions[0].Node != "b" {
		t.Errorf("pending pod went to %q, want b (least allocated)", report.Decisions[0].Node)
	}
	if report.Nodes[1].Usage.Used.CPU != 1000 {
		t.Errorf("node b used = %d, want 1000", report.Nodes[1].Usage.Used.CPU)
	}
	if report.Nodes[0].Pods != 1 {
		t.Errorf("node a pods = %d, want the existing pod only", report.Nodes[0].Pods)
	}
}

func TestBuildReportTaintBlocksWithoutToleration(t *testing.T) {
	gpu := testNode("gpu", 8000, 16*1024*1024*1024, 1, map[string]string{"pool": "gpu"})
	gpu.Spec.Taints = []Taint{{Key: "dedicated", Value: "gpu", Effect: "NoSchedule"}}
	capture := Capture{Nodes: []Node{gpu}, Pods: []Pod{testPod("ml", "train", "", 1000, 1024*1024*1024, 1)}}

	blocked := buildReport(capture, "", time.Now())
	if blocked.Summary.Unschedulable != 1 || !strings.Contains(strings.Join(blocked.Decisions[0].Reasons, " "), "taint") {
		t.Errorf("expected a taint rejection, got %+v", blocked.Decisions[0])
	}

	pod := capture.Pods[0]
	pod.Spec.Tolerations = []Toleration{{Key: "dedicated", Operator: "Equal", Value: "gpu", Effect: "NoSchedule"}}
	allowed := buildReport(Capture{Nodes: capture.Nodes, Pods: []Pod{pod}}, "", time.Now())
	if allowed.Summary.Scheduled != 1 {
		t.Errorf("tolerating pod was not scheduled: %+v", allowed.Decisions[0])
	}
}

func TestBuildReportSelectorAndUnschedulableNode(t *testing.T) {
	cpu := testNode("cpu", 8000, 16*1024*1024*1024, 0, map[string]string{"pool": "cpu"})
	off := testNode("off", 8000, 16*1024*1024*1024, 0, nil)
	off.Spec.Unschedulable = true
	pod := testPod("ml", "p", "", 1000, 1024*1024*1024, 0)
	pod.Spec.NodeSelector = map[string]string{"pool": "gpu"}

	report := buildReport(Capture{Nodes: []Node{cpu, off}, Pods: []Pod{pod}}, "", time.Now())
	if report.Summary.Unschedulable != 1 {
		t.Fatalf("summary = %+v", report.Summary)
	}
	reasons := strings.Join(report.Decisions[0].Reasons, " ")
	if !strings.Contains(reasons, "nodeSelector") || !strings.Contains(reasons, "unschedulable") {
		t.Errorf("reasons = %q", reasons)
	}
}

func TestBuildReportResourceShortage(t *testing.T) {
	node := testNode("small", 2000, 2*1024*1024*1024, 0, nil)
	pod := testPod("ml", "big", "", 4000, 8*1024*1024*1024, 0)
	report := buildReport(Capture{Nodes: []Node{node}, Pods: []Pod{pod}}, "", time.Now())
	reasons := strings.Join(report.Decisions[0].Reasons, " ")
	if !strings.Contains(reasons, "insufficient cpu") || !strings.Contains(reasons, "insufficient memory") {
		t.Errorf("reasons = %q", reasons)
	}
}

func TestBuildReportNamespaceFilterKeepsExistingUsage(t *testing.T) {
	capture := Capture{
		Nodes: []Node{testNode("a", 4000, 8*1024*1024*1024, 0, nil)},
		Pods: []Pod{
			testPod("other", "existing", "a", 2000, 2*1024*1024*1024, 0),
			testPod("ml", "keep", "", 1000, 1024*1024*1024, 0),
			testPod("other", "drop", "", 1000, 1024*1024*1024, 0),
		},
	}
	report := buildReport(capture, "ml", time.Now())
	if report.Summary.Pending != 1 || len(report.Decisions) != 1 || report.Decisions[0].Name != "keep" {
		t.Errorf("namespace filter = %+v", report.Decisions)
	}
	if report.Nodes[0].Usage.Used.CPU != 3000 {
		t.Errorf("existing usage was not counted: %+v", report.Nodes[0].Usage)
	}
}

func TestBuildReportIsDeterministic(t *testing.T) {
	capture := Capture{
		Nodes: []Node{
			testNode("b", 4000, 8*1024*1024*1024, 0, nil),
			testNode("a", 4000, 8*1024*1024*1024, 0, nil),
		},
		Pods: []Pod{
			testPod("z", "last", "", 1000, 1024*1024*1024, 0),
			testPod("a", "first", "", 1000, 1024*1024*1024, 0),
		},
	}
	first := buildReport(capture, "", time.Now())
	second := buildReport(capture, "", time.Now())
	if first.Decisions[0].Name != "first" || first.Decisions[1].Name != "last" {
		t.Errorf("decisions are not sorted: %+v", first.Decisions)
	}
	if first.Decisions[0].Node != second.Decisions[0].Node || first.Nodes[0].Name != "a" {
		t.Error("buildReport is not deterministic")
	}
}

func TestRenderText(t *testing.T) {
	report := buildReport(Capture{
		Nodes: []Node{testNode("a", 4000, 8*1024*1024*1024, 0, nil)},
		Pods:  []Pod{testPod("ml", "p", "", 1000, 1024*1024*1024, 0)},
	}, "", time.Now())
	var out bytes.Buffer
	if err := renderText(&out, report); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Scheduler simulation", "ml/p", "Node usage", "scheduled"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("text output is missing %q", want)
		}
	}
}

func TestRenderJSONRoundTrips(t *testing.T) {
	report := buildReport(Capture{
		Nodes: []Node{testNode("a", 4000, 8*1024*1024*1024, 1, nil)},
		Pods:  []Pod{testPod("ml", "p", "", 1000, 1024*1024*1024, 1)},
	}, "", time.Now())
	var out bytes.Buffer
	if err := renderJSON(&out, report); err != nil {
		t.Fatal(err)
	}
	var decoded Report
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if decoded.Summary.Scheduled != 1 || decoded.Decisions[0].Node != "a" {
		t.Errorf("decoded report = %+v", decoded)
	}
}

func TestRenderCSV(t *testing.T) {
	report := buildReport(Capture{
		Nodes: []Node{testNode("a", 4000, 8*1024*1024*1024, 0, nil)},
		Pods:  []Pod{testPod("ml", "p", "", 1000, 1024*1024*1024, 0)},
	}, "", time.Now())
	var out bytes.Buffer
	if err := renderCSV(&out, report); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected a header and one row, got %d lines", len(lines))
	}
	if lines[0] != "namespace,pod,cpu_millis,memory_bytes,gpus,node,score,reasons" {
		t.Errorf("header = %q", lines[0])
	}
	if !strings.HasPrefix(lines[1], "ml,p,1000,1073741824,0,a,") {
		t.Errorf("row = %q", lines[1])
	}
}

func TestRenderHTMLIsSelfContainedAndEscaped(t *testing.T) {
	pod := testPod("ml", "<script>alert(1)</script>", "", 1000, 1024*1024*1024, 0)
	report := buildReport(Capture{Nodes: []Node{testNode("a", 4000, 8*1024*1024*1024, 0, nil)}, Pods: []Pod{pod}}, "", time.Now())
	var out bytes.Buffer
	if err := renderHTML(&out, report); err != nil {
		t.Fatal(err)
	}
	html := out.String()
	for _, want := range []string{"<!DOCTYPE html>", "<style>", "Scheduler simulation", "Generated by k8s-scheduler-sim"} {
		if !strings.Contains(html, want) {
			t.Errorf("HTML is missing %q", want)
		}
	}
	if strings.Contains(html, "<script") {
		t.Error("HTML contains a script tag")
	}
	if !strings.Contains(html, "&lt;script&gt;alert(1)&lt;/script&gt;") {
		t.Error("pod name was not escaped")
	}
	if strings.Contains(html, `<link`) || strings.Contains(html, `src="http`) {
		t.Error("HTML loads an external resource")
	}
}

func TestExampleCapture(t *testing.T) {
	capture, err := loadCapture("examples/capture.json")
	if err != nil {
		t.Fatal(err)
	}
	report := buildReport(capture, "", time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC))
	if report.Summary.Nodes != 4 || report.Summary.Pending != 5 {
		t.Errorf("summary = %+v", report.Summary)
	}
	if report.Summary.Scheduled != 3 || report.Summary.Unschedulable != 2 {
		t.Errorf("expected 3 scheduled and 2 unschedulable, got %+v", report.Summary)
	}
	placements := map[string]string{}
	for _, decision := range report.Decisions {
		placements[decision.Name] = decision.Node
	}
	if placements["batch"] != "cpu-a" {
		t.Errorf("batch went to %q, want cpu-a", placements["batch"])
	}
	if placements["inference"] != "gpu-b" || placements["train-new"] != "gpu-b" {
		t.Errorf("gpu placements = %+v", placements)
	}
	if placements["needs-ssd"] != "" || placements["too-big"] != "" {
		t.Errorf("unschedulable pods got a node: %+v", placements)
	}
}

func TestVersionString(t *testing.T) {
	if strings.TrimSpace(versionString()) == "" {
		t.Error("versionString() is empty")
	}
}
