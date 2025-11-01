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
	"encoding/json"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	"sigs.k8s.io/controller-runtime/pkg/log"

	garagev1alpha1 "github.com/dimedis-gmbh/garage-operator/api/v1alpha1"
)

// ClusterHealth represents Garage cluster health information
type ClusterHealth struct {
	Status           string `json:"status"`
	KnownNodes       int    `json:"knownNodes"`
	ConnectedNodes   int    `json:"connectedNodes"`
	StorageNodes     int    `json:"storageNodes"`
	StorageNodesUp   int    `json:"storageNodesUp"`
	Partitions       int    `json:"partitions"`
	PartitionsQuorum int    `json:"partitionsQuorum"`
	PartitionsAllOk  int    `json:"partitionsAllOk"`
}

// ClusterStatus represents Garage cluster status information
type ClusterStatus struct {
	LayoutVersion int64 `json:"layoutVersion"`
}

// checkClusterHealth queries the Garage cluster health via kubectl exec
func (r *GarageClusterReconciler) checkClusterHealth(ctx context.Context, gc *garagev1alpha1.GarageCluster, config *rest.Config) (*ClusterHealth, *ClusterStatus, error) {
	logger := log.FromContext(ctx)

	// Get the first ready pod to query
	podName := fmt.Sprintf("%s-0", gc.Name)

	// Query cluster health
	healthCmd := []string{"./garage", "json-api", "GetClusterHealth"}
	healthOutput, err := r.execInPod(ctx, gc.Namespace, podName, healthCmd, config)
	if err != nil {
		logger.Error(err, "Failed to execute GetClusterHealth")
		return nil, nil, fmt.Errorf("failed to query cluster health: %w", err)
	}

	health := &ClusterHealth{}
	if err := json.Unmarshal([]byte(healthOutput), health); err != nil {
		logger.Error(err, "Failed to parse cluster health response", "output", healthOutput)
		return nil, nil, fmt.Errorf("failed to parse health response: %w", err)
	}

	// Query cluster status for layout version
	statusCmd := []string{"./garage", "json-api", "GetClusterStatus"}
	statusOutput, err := r.execInPod(ctx, gc.Namespace, podName, statusCmd, config)
	if err != nil {
		logger.Error(err, "Failed to execute GetClusterStatus")
		return health, nil, fmt.Errorf("failed to query cluster status: %w", err)
	}

	status := &ClusterStatus{}
	if err := json.Unmarshal([]byte(statusOutput), status); err != nil {
		logger.Error(err, "Failed to parse cluster status response", "output", statusOutput)
		return health, nil, fmt.Errorf("failed to parse status response: %w", err)
	}

	return health, status, nil
}

// execInPod executes a command in a pod and returns the output
func (r *GarageClusterReconciler) execInPod(ctx context.Context, namespace, podName string, command []string, config *rest.Config) (string, error) {
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return "", fmt.Errorf("failed to create clientset: %w", err)
	}

	req := clientset.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(podName).
		Namespace(namespace).
		SubResource("exec")

	req.VersionedParams(&corev1.PodExecOptions{
		Command: command,
		Stdout:  true,
		Stderr:  true,
	}, runtime.NewParameterCodec(r.Scheme))

	exec, err := remotecommand.NewSPDYExecutor(config, "POST", req.URL())
	if err != nil {
		return "", fmt.Errorf("failed to create executor: %w", err)
	}

	var stdout, stderr string
	stdoutWriter := &stringWriter{str: &stdout}
	stderrWriter := &stringWriter{str: &stderr}

	err = exec.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdout: stdoutWriter,
		Stderr: stderrWriter,
	})

	if err != nil {
		return "", fmt.Errorf("exec failed: %w (stderr: %s)", err, stderr)
	}

	return stdout, nil
}

// stringWriter implements io.Writer to capture output as string
type stringWriter struct {
	str *string
}

func (sw *stringWriter) Write(p []byte) (n int, err error) {
	*sw.str += string(p)
	return len(p), nil
}
