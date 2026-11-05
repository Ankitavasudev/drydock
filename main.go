package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime/debug"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

var version = "v1.0.0"

func versionString() string {
	if info, ok := debug.ReadBuildInfo(); ok {
		v := info.Main.Version
		if v != "" && v != "(devel)" && !strings.HasPrefix(v, "v0.0.0-") && !strings.Contains(v, "+dirty") {
			return v
		}
	}
	return version
}

var (
	flagFrom      string
	flagNodes     string
	flagPods      string
	flagNamespace string
	flagOutput    string
)

var rootCmd = &cobra.Command{
	Use:   "k8s-scheduler-sim",
	Short: "Simulate Kubernetes pod placement over a saved capture",
	Long: "Filter and score nodes for pending pods using the same inputs the\n" +
		"kube-scheduler uses: allocatable resources, existing pod requests,\n" +
		"node selectors, taints and tolerations.\n\n" +
		"Use --from with a saved capture, --nodes/--pods with kubectl output,\n" +
		"or no flags to read the current kubectl context.",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		capture, source, err := loadInput()
		if err != nil {
			return err
		}
		report := buildReport(capture, flagNamespace, time.Now())
		report.Generated = time.Now().Format("2006-01-02 15:04 MST")
		report.Source = source

		out := cmd.OutOrStdout()
		switch flagOutput {
		case "text":
			return renderText(out, report)
		case "json":
			return renderJSON(out, report)
		case "csv":
			return renderCSV(out, report)
		case "html":
			return renderHTML(out, report)
		default:
			return fmt.Errorf("unknown output %q: use text, json, csv or html", flagOutput)
		}
	},
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Fprintln(cmd.OutOrStdout(), versionString())
	},
}

func loadInput() (Capture, string, error) {
	switch {
	case flagFrom != "":
		capture, err := loadCapture(flagFrom)
		return capture, flagFrom, err
	case flagNodes != "" || flagPods != "":
		if flagNodes == "" || flagPods == "" {
			return Capture{}, "", errors.New("--nodes and --pods must be given together, or use --from with one capture")
		}
		nodes, err := loadNodes(flagNodes)
		if err != nil {
			return Capture{}, "", fmt.Errorf("reading nodes: %w", err)
		}
		pods, err := loadPods(flagPods)
		if err != nil {
			return Capture{}, "", fmt.Errorf("reading pods: %w", err)
		}
		return Capture{Nodes: nodes, Pods: pods}, flagNodes + " + " + flagPods, nil
	default:
		capture, err := collectLive()
		return capture, "live cluster (kubectl)", err
	}
}

func kubectlJSON(args ...string) ([]byte, error) {
	output, err := exec.Command("kubectl", args...).Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && len(exitErr.Stderr) > 0 {
			return nil, fmt.Errorf("kubectl %s: %s", args[0], string(exitErr.Stderr))
		}
		return nil, fmt.Errorf("kubectl %s: %w (is kubectl installed and a context selected?)", args[0], err)
	}
	return output, nil
}

func collectLive() (Capture, error) {
	nodesJSON, err := kubectlJSON("get", "nodes", "-o", "json")
	if err != nil {
		return Capture{}, err
	}
	podsJSON, err := kubectlJSON("get", "pods", "-A", "-o", "json")
	if err != nil {
		return Capture{}, err
	}
	nodes, err := parseNodes(nodesJSON)
	if err != nil {
		return Capture{}, fmt.Errorf("parsing kubectl nodes output: %w", err)
	}
	pods, err := parsePods(podsJSON)
	if err != nil {
		return Capture{}, fmt.Errorf("parsing kubectl pods output: %w", err)
	}
	return Capture{Nodes: nodes, Pods: pods}, nil
}

func init() {
	rootCmd.Flags().StringVar(&flagFrom, "from", "", "combined capture with nodes and pods arrays")
	rootCmd.Flags().StringVar(&flagNodes, "nodes", "", "NodeList JSON from kubectl get nodes -o json")
	rootCmd.Flags().StringVar(&flagPods, "pods", "", "PodList JSON from kubectl get pods -A -o json")
	rootCmd.Flags().StringVarP(&flagNamespace, "namespace", "n", "", "only simulate pending pods in this namespace")
	rootCmd.Flags().StringVarP(&flagOutput, "output", "o", "text", "output format: text, json, csv or html")
	rootCmd.AddCommand(versionCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}
