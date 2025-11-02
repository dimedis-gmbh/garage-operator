/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

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
	// Create GarageClient for this cluster
	client, err := NewGarageClient(lm.config, gc)
	if err != nil {
		return fmt.Errorf("failed to create garage client: %w", err)
	}

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
	for i, node := range nodes {
		if i == 0 {
			// Skip connecting pod-0 to itself
			continue
		}

		output, err := client.ExecCommand(ctx, "node", "connect", node.address)
		if err != nil {
			return fmt.Errorf("failed to connect to node %s: %w, output: %s", node.address, err, output)
		}
	}

	// Give nodes time to establish connections
	time.Sleep(2 * time.Second)

	// Determine capacity - use data volume size
	capacity := "20Gi" // default
	if gc.Spec.Persistence != nil && gc.Spec.Persistence.Data != nil && gc.Spec.Persistence.Data.Size != "" {
		capacity = gc.Spec.Persistence.Data.Size
	}

	// Assign layout for each node
	for i, node := range nodes {
		zone := fmt.Sprintf("z%d", i+1)

		output, err := client.ExecCommand(ctx,
			"layout",
			"assign",
			"-z", zone,
			"-c", capacity,
			node.id,
		)
		if err != nil {
			return fmt.Errorf("failed to assign layout for node %s: %w, output: %s", node.id, err, output)
		}
	}

	// Apply layout
	output, err := client.ExecCommand(ctx,
		"layout",
		"apply",
		"--version", "1",
	)
	if err != nil {
		return fmt.Errorf("failed to apply layout: %w, output: %s", err, output)
	}

	return nil
}

// getNodeID retrieves the node ID from a specific pod
func (lm *LayoutManager) getNodeID(ctx context.Context, namespace, podName string) (string, error) {
	// Create a temporary GarageCluster reference for the client
	tempCluster := &garagev1alpha1.GarageCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      strings.Split(podName, "-")[0], // Extract cluster name from pod name
			Namespace: namespace,
		},
	}

	client, err := NewGarageClient(lm.config, tempCluster)
	if err != nil {
		return "", fmt.Errorf("failed to create garage client: %w", err)
	}

	output, err := client.ExecCommandInPod(ctx, podName, "node", "id")
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
