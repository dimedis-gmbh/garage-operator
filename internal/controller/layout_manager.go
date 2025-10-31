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
	// Wait for all pods to be ready
	for i := int32(0); i < gc.Spec.ReplicaCount; i++ {
		podName := fmt.Sprintf("%s-%d", gc.Name, i)
		if err := lm.waitForPod(ctx, gc.Namespace, podName, 5*time.Minute); err != nil {
			return fmt.Errorf("pod %s not ready: %w", podName, err)
		}
	}

	// Give Garage a moment to start up completely
	time.Sleep(5 * time.Second)

	// Get node IDs and addresses from each pod
	type nodeInfo struct {
		id      string
		address string
	}
	nodes := make([]nodeInfo, 0, gc.Spec.ReplicaCount)
	
	for i := int32(0); i < gc.Spec.ReplicaCount; i++ {
		podName := fmt.Sprintf("%s-%d", gc.Name, i)
		nodeID, err := lm.getNodeID(ctx, gc.Namespace, podName)
		if err != nil {
			return fmt.Errorf("failed to get node ID from pod %s: %w", podName, err)
		}
		
		// Build the full node address with DNS name
		nodeAddress := fmt.Sprintf("%s@%s.%s.%s.svc.cluster.local:3901",
			nodeID, podName, gc.Name, gc.Namespace)
		
		nodes = append(nodes, nodeInfo{
			id:      nodeID,
			address: nodeAddress,
		})
	}

	// Connect all nodes to each other from pod-0
	podName := fmt.Sprintf("%s-0", gc.Name)
	for i, node := range nodes {
		if i == 0 {
			// Skip connecting pod-0 to itself
			continue
		}
		
		cmd := []string{"./garage", "node", "connect", node.address}
		output, err := lm.execCommandWithOutput(ctx, gc.Namespace, podName, cmd)
		if err != nil {
			return fmt.Errorf("failed to connect to node %s: %w, output: %s", node.address, err, output)
		}
	}

	// Give nodes time to establish connections
	time.Sleep(2 * time.Second)

	// Assign layout for each node
	for i, node := range nodes {
		zone := fmt.Sprintf("z%d", i+1)
		capacity := gc.Spec.VolumeSize

		cmd := []string{
			"./garage",
			"layout",
			"assign",
			"-z", zone,
			"-c", capacity,
			node.id,
		}

		output, err := lm.execCommandWithOutput(ctx, gc.Namespace, podName, cmd)
		if err != nil {
			return fmt.Errorf("failed to assign layout for node %s: %w, output: %s", node.id, err, output)
		}
	}

	// Apply layout
	applyCmd := []string{
		"./garage",
		"layout",
		"apply",
		"--version", "1",
	}

	output, err := lm.execCommandWithOutput(ctx, gc.Namespace, podName, applyCmd)
	if err != nil {
		return fmt.Errorf("failed to apply layout: %w, output: %s", err, output)
	}

	// Update GarageCluster status with node information
	gc.Status.Nodes = make([]garagev1alpha1.GarageNode, len(nodes))
	for i, node := range nodes {
		gc.Status.Nodes[i] = garagev1alpha1.GarageNode{
			ID:       node.id,
			Zone:     fmt.Sprintf("z%d", i+1),
			Capacity: gc.Spec.VolumeSize,
			Status:   "Ready",
		}
	}

	return nil
}

// getNodeID retrieves the node ID from a specific pod
func (lm *LayoutManager) getNodeID(ctx context.Context, namespace, podName string) (string, error) {
	cmd := []string{
		"./garage",
		"node", "id",
	}

	output, err := lm.execCommandWithOutput(ctx, namespace, podName, cmd)
	if err != nil {
		return "", fmt.Errorf("failed to execute command: %w", err)
	}

	// The output may be in format: nodeID@hostname:port
	// We need just the nodeID part (before the @)
	nodeID := strings.TrimSpace(output)
	if nodeID == "" {
		return "", fmt.Errorf("empty node ID returned")
	}

	// Extract just the ID part if @ is present
	if idx := strings.Index(nodeID, "@"); idx > 0 {
		nodeID = nodeID[:idx]
	}

	return nodeID, nil
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
