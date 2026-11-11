package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"strconv"
	"strings"
)

func renderText(w io.Writer, report Report) error {
	fmt.Fprintf(w, "Scheduler simulation — %s\n", report.Source)
	fmt.Fprintf(w, "Generated %s", report.Generated)
	if report.Namespace != "" {
		fmt.Fprintf(w, " · namespace %s", report.Namespace)
	}
	fmt.Fprintln(w)
	fmt.Fprintf(w, "\n%d nodes · %d pending pods · %d scheduled · %d unschedulable\n\n",
		report.Summary.Nodes, report.Summary.Pending, report.Summary.Scheduled, report.Summary.Unschedulable)

	if len(report.Decisions) == 0 {
		fmt.Fprintln(w, "No pending pods to simulate.")
	} else {
		fmt.Fprintf(w, "%-30s %-26s %-18s %s\n", "POD", "REQUESTS", "DECISION", "SCORE")
		fmt.Fprintln(w, strings.Repeat("-", 88))
		for _, decision := range report.Decisions {
			where := decision.Node
			scoreText := strconv.Itoa(decision.Score)
			if where == "" {
				where = "unschedulable"
				scoreText = "-"
			}
			fmt.Fprintf(w, "%-30s %-26s %-18s %s\n",
				decision.Namespace+"/"+decision.Name, formatResources(decision.Requests), where, scoreText)
			if len(decision.Reasons) > 0 {
				for _, reason := range decision.Reasons {
					fmt.Fprintf(w, "  %s\n", reason)
				}
			}
		}
		fmt.Fprintln(w, strings.Repeat("-", 88))
	}

	fmt.Fprintln(w, "\nNode usage after simulation")
	fmt.Fprintf(w, "%-22s %-18s %-20s %-10s %s\n", "NODE", "CPU", "MEMORY", "GPU", "PODS")
	fmt.Fprintln(w, strings.Repeat("-", 82))
	for _, node := range report.Nodes {
		cpu := fmt.Sprintf("%s/%s", formatCPU(node.Usage.Used.CPU), formatCPU(node.Usage.Capacity.CPU))
		memory := fmt.Sprintf("%s/%s", formatMemory(node.Usage.Used.Memory), formatMemory(node.Usage.Capacity.Memory))
		gpu := fmt.Sprintf("%d/%d", node.Usage.Used.GPUs, node.Usage.Capacity.GPUs)
		name := node.Name
		if node.Unschedulable {
			name += " *"
		}
		fmt.Fprintf(w, "%-22s %-18s %-20s %-10s %d\n", name, cpu, memory, gpu, node.Pods)
	}
	if len(report.Nodes) > 0 {
		fmt.Fprintln(w, "\n* node is marked unschedulable")
	}
	return nil
}

func renderJSON(w io.Writer, report Report) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(report)
}

func renderCSV(w io.Writer, report Report) error {
	cw := csv.NewWriter(w)
	if err := cw.Write([]string{
		"namespace", "pod", "cpu_millis", "memory_bytes", "gpus", "node", "score", "reasons",
	}); err != nil {
		return err
	}
	for _, decision := range report.Decisions {
		if err := cw.Write([]string{
			decision.Namespace,
			decision.Name,
			strconv.FormatInt(decision.Requests.CPU, 10),
			strconv.FormatInt(decision.Requests.Memory, 10),
			strconv.Itoa(decision.Requests.GPUs),
			decision.Node,
			strconv.Itoa(decision.Score),
			reasonText(decision.Reasons),
		}); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

var reportTemplate = template.Must(template.New("report").Funcs(template.FuncMap{
	"cpu":     formatCPU,
	"memory":  formatMemory,
	"percent": percent,
	"percentGPU": func(part, total int) int {
		return percent(int64(part), int64(total))
	},
}).Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>drydock report</title>
<style>
:root{--bg:#0b0f14;--panel:#121b23;--card:#16232d;--line:#23343f;--text:#eaf1f8;--muted:#8ba3b5;--accent:#00adb5;--ok:#3fb950;--warn:#d29922;--bad:#f85149;--run:#58a6ff}
*{box-sizing:border-box}
body{margin:0;background:radial-gradient(1000px 500px at 50% -10%,#123039 0%,var(--bg) 55%);color:var(--text);font:15px/1.5 ui-sans-serif,system-ui,-apple-system,"Segoe UI",Roboto,sans-serif}
main{max-width:1120px;margin:0 auto;padding:40px 20px 64px}
h1{font-size:26px;margin:0 0 4px}
.sub{color:var(--muted);font-size:13px;margin-bottom:24px}
.cards{display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:12px;margin-bottom:22px}
@media(max-width:760px){.cards{grid-template-columns:repeat(2,minmax(0,1fr))}}
.card{background:var(--panel);border:1px solid var(--line);border-radius:12px;padding:14px}
.card .k{color:var(--muted);font-size:12px;text-transform:uppercase;letter-spacing:.06em}
.card .v{font-size:26px;font-weight:700;margin-top:2px}
.card .v.ok{color:var(--ok)}.card .v.warn{color:var(--warn)}.card .v.bad{color:var(--bad)}.card .v.run{color:var(--run)}
.panel{background:var(--panel);border:1px solid var(--line);border-radius:12px;padding:16px;margin-bottom:14px}
.panel h2{font-size:16px;margin:0 0 10px}
table{width:100%;border-collapse:collapse;font-size:13px}
th,td{text-align:left;padding:7px 8px;border-bottom:1px solid var(--line);vertical-align:top}
th{color:var(--muted);font-weight:500;font-size:11px;text-transform:uppercase;letter-spacing:.05em}
td.num{font-family:ui-monospace,Consolas,monospace}
code{font-family:ui-monospace,SFMono-Regular,Consolas,monospace;background:#0b141a;border:1px solid var(--line);border-radius:4px;padding:1px 6px;font-size:12px}
.badge{display:inline-block;font-size:11px;border-radius:999px;padding:1px 8px;border:1px solid var(--line)}
.badge.ok{color:var(--ok);border-color:var(--ok)}.badge.bad{color:var(--bad);border-color:var(--bad)}
.badge.run{color:var(--run);border-color:var(--run)}.badge.warn{color:var(--warn);border-color:var(--warn)}
.muted{color:var(--muted)}
.reasons{color:var(--muted);font-size:12px;margin-top:4px}
.reasons div{margin-top:2px}
.node{background:var(--card);border:1px solid var(--line);border-radius:10px;padding:14px;margin-bottom:10px}
.node h3{font-size:14px;margin:0;display:flex;gap:8px;align-items:baseline}
.node h3 .pods{margin-left:auto;color:var(--muted);font-size:12px;font-weight:400}
.bar-row{display:grid;grid-template-columns:52px 1fr 120px;gap:10px;align-items:center;margin-top:9px;font-size:12px}
.bar-row .label{color:var(--muted)}
.bar{height:8px;border-radius:999px;background:#0b141a;overflow:hidden}
.bar span{display:block;height:100%;background:linear-gradient(90deg,#00adb5,#3fb950)}
.bar.high span{background:linear-gradient(90deg,#d29922,#f85149)}
.bar-row .value{text-align:right;font-family:ui-monospace,Consolas,monospace}
.empty{color:var(--muted);font-size:13px}
footer{color:var(--muted);font-size:12px;margin-top:30px}
</style>
</head>
<body>
<main>
<h1>Scheduler simulation</h1>
<div class="sub">Source: {{.Source}} · generated {{.Generated}}{{if .Namespace}} · namespace {{.Namespace}}{{end}}</div>

<div class="cards">
  <div class="card"><div class="k">Nodes</div><div class="v">{{.Summary.Nodes}}</div></div>
  <div class="card"><div class="k">Pending pods</div><div class="v">{{.Summary.Pending}}</div></div>
  <div class="card"><div class="k">Scheduled</div><div class="v ok">{{.Summary.Scheduled}}</div></div>
  <div class="card"><div class="k">Unschedulable</div><div class="v {{if .Summary.Unschedulable}}bad{{else}}ok{{end}}">{{.Summary.Unschedulable}}</div></div>
</div>

<div class="panel">
<h2>Placements</h2>
{{if .Decisions}}<table>
<thead><tr><th>Pod</th><th>Requests</th><th>Decision</th><th>Score</th></tr></thead>
<tbody>{{range .Decisions}}<tr>
<td><code>{{.Namespace}}/{{.Name}}</code></td>
<td class="num">cpu {{cpu .Requests.CPU}}<br>mem {{memory .Requests.Memory}}{{if .Requests.GPUs}}<br>gpu {{.Requests.GPUs}}{{end}}</td>
<td>{{if .Node}}<span class="badge ok">{{.Node}}</span>{{else}}<span class="badge bad">unschedulable</span><div class="reasons">{{range .Reasons}}<div>{{.}}</div>{{end}}</div>{{end}}</td>
<td class="num">{{if .Node}}{{.Score}}{{else}}-{{end}}</td>
</tr>{{end}}</tbody>
</table>{{else}}<p class="empty">No pending pods to simulate.</p>{{end}}
</div>

<div class="panel">
<h2>Node usage after simulation</h2>
{{range .Nodes}}<article class="node">
<h3>{{.Name}}{{if .Unschedulable}} <span class="badge warn">unschedulable</span>{{end}}<span class="pods">{{.Pods}} pods</span></h3>
<div class="bar-row"><span class="label">CPU</span><div class="bar"><span style="width:{{percent .Usage.Used.CPU .Usage.Capacity.CPU}}%"></span></div><span class="value">{{cpu .Usage.Used.CPU}}/{{cpu .Usage.Capacity.CPU}}</span></div>
<div class="bar-row"><span class="label">Memory</span><div class="bar"><span style="width:{{percent .Usage.Used.Memory .Usage.Capacity.Memory}}%"></span></div><span class="value">{{memory .Usage.Used.Memory}}/{{memory .Usage.Capacity.Memory}}</span></div>
<div class="bar-row"><span class="label">GPU</span><div class="bar"><span style="width:{{percentGPU .Usage.Used.GPUs .Usage.Capacity.GPUs}}%"></span></div><span class="value">{{.Usage.Used.GPUs}}/{{.Usage.Capacity.GPUs}}</span></div>
</article>{{end}}
</div>

<footer>Generated by drydock {{.Version}}. This is a filter-and-score simulation over a saved capture, not a running kube-scheduler. Existing pods count as allocated; pending pods are placed in namespace/name order.</footer>
</main>
</body>
</html>
`))

func percent(part, total int64) int {
	if total <= 0 {
		return 0
	}
	value := int(part * 100 / total)
	if value < 0 {
		return 0
	}
	if value > 100 {
		return 100
	}
	return value
}

func renderHTML(w io.Writer, report Report) error {
	return reportTemplate.Execute(w, struct {
		Report
		Version string
	}{report, versionString()})
}
