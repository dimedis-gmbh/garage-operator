package controller

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"

	garagev1alpha1 "github.com/dimedis-gmbh/garage-operator/api/v1alpha1"
)

// LayoutManager handles Garage cluster layout configuration
type LayoutManager struct {
	config    *rest.Config
	clientset *kubernetes.Clientset
}

// NewLayoutManager creates a new LayoutManager
func NewLayoutManager(config *rest.Config) (*LayoutManager, error) {
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create clientset: %w", err)
	}
	return &LayoutManager{
		config:    config,
		clientset: clientset,
	}, nil
}

// ConfigureLayout configures the Garage cluster layout
func (lm *LayoutManager) ConfigureLayout(ctx context.Context, gc *garagev1alpha1.GarageCluster) error {
	podName := fmt.Sprintf("%s-0", gc.Name)

	// Wait for pod to be ready
	if err := lm.waitForPod(ctx, gc.Namespace, podName, 5*time.Minute); err != nil {
		return fmt.Errorf("pod not ready: %w", err)
	}

	// Give Garage a moment to start up completely
	time.Sleep(5 * time.Second)

	// Get node IDs
	nodeIDs, err := lm.getNodeIDs(ctx, gc.Namespace, podName)
	if err != nil {
		return fmt.Errorf("failed to get node IDs: %w", err)
	}

	if len(nodeIDs) != int(gc.Spec.ReplicaCount) {
		return fmt.Errorf("expected %d nodes, got %d", gc.Spec.ReplicaCount, len(nodeIDs))
	}

	// Assign layout for each node
	for i, nodeID := range nodeIDs {
		zone := fmt.Sprintf("z%d", i+1)
		capacity := gc.Spec.VolumeSize

		cmd := []string{
			"./garage",
			"layout",
			"assign",
			"-z", zone,
			"-c", capacity,
			nodeID,
		}

		output, err := lm.execCommandWithOutput(ctx, gc.Namespace, podName, cmd)
		if err != nil {
			return fmt.Errorf("failed to assign layout for node %s: %w, output: %s", nodeID, err, output)
		}
	}

	// Apply layout with automatic yes confirmation
	applyCmd := []string{
		"sh", "-c",
		"echo 'yes' | ./garage layout apply --version 1",
	}

	output, err := lm.execCommandWithOutput(ctx, gc.Namespace, podName, applyCmd)
	if err != nil {
		return fmt.Errorf("failed to apply layout: %w, output: %s", err, output)
	}

	// Update GarageCluster status with node information
	gc.Status.Nodes = make([]garagev1alpha1.GarageNode, len(nodeIDs))
	for i, nodeID := range nodeIDs {
		gc.Status.Nodes[i] = garagev1alpha1.GarageNode{
			ID:       nodeID,
			Zone:     fmt.Sprintf("z%d", i+1),
			Capacity: gc.Spec.VolumeSize,
			Status:   "Ready",
		}
	}

	return nil
}

// getNodeIDs retrieves all node IDs from the Garage cluster
func (lm *LayoutManager) getNodeIDs(ctx context.Context, namespace, podName string) ([]string, error) {
	cmd := []string{
		"sh", "-c",
		"./garage status | grep -v '^=' | tail -n +2 | awk '{print $1}'",
	}

	output, err := lm.execCommandWithOutput(ctx, namespace, podName, cmd)
	if err != nil {
		return nil, fmt.Errorf("failed to execute command: %w", err)
	}

	lines := strings.Split(strings.TrimSpace(output), "\n")
	nodeIDs := make([]string, 0, len(lines))

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "=") {
			nodeIDs = append(nodeIDs, line)
		}
	}

	return nodeIDs, nil
}

// execCommandWithOutput executes a command in a pod and returns its output
func (lm *LayoutManager) execCommandWithOutput(ctx context.Context, namespace, podName string, cmd []string) (string, error) {
	req := lm.clientset.CoreV1().RESTClient().
		Post().
		Namespace(namespace).
		Resource("pods").
		Name(podName).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Command:   cmd,
			Container: "garage",
			Stdin:     false,
			Stdout:    true,
			Stderr:    true,
			TTY:       false,
		}, scheme.ParameterCodec)

	exec, err := remotecommand.NewSPDYExecutor(lm.config, "POST", req.URL())
	if err != nil {
		return "", fmt.Errorf("failed to create executor: %w", err)
	}

	var stdout, stderr bytes.Buffer
	err = exec.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdout: &stdout,
		Stderr: &stderr,
	})

	if err != nil {
		return stdout.String(), fmt.Errorf("command execution failed: %w, stderr: %s", err, stderr.String())
	}

	return stdout.String(), nil
}

// waitForPod waits for a pod to be ready
func (lm *LayoutManager) waitForPod(ctx context.Context, namespace, podName string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for pod %s to be ready", podName)
		case <-ticker.C:
			pod, err := lm.clientset.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
			if err != nil {
				continue
			}

			// Check if pod is ready
			for _, condition := range pod.Status.Conditions {
				if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
					return nil
				}
			}
		}
	}
}

// GetClusterStatus retrieves the current cluster status
func (lm *LayoutManager) GetClusterStatus(ctx context.Context, namespace, podName string) (string, error) {
	cmd := []string{"./garage", "status"}
	return lm.execCommandWithOutput(ctx, namespace, podName, cmd)
}
