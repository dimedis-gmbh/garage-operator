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
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	garagev1alpha1 "github.com/dimedis-gmbh/garage-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
)

// GarageClient handles communication with Garage nodes
type GarageClient struct {
	config    *rest.Config
	clientset *kubernetes.Clientset
	cluster   *garagev1alpha1.GarageCluster
}

// NewGarageClient creates a new GarageClient
func NewGarageClient(config *rest.Config, cluster *garagev1alpha1.GarageCluster) (*GarageClient, error) {
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create clientset: %w", err)
	}
	return &GarageClient{
		config:    config,
		clientset: clientset,
		cluster:   cluster,
	}, nil
}

// ExecCommand executes a garage CLI command in the first pod
func (gc *GarageClient) ExecCommand(ctx context.Context, args ...string) (string, error) {
	podName := fmt.Sprintf("%s-0", gc.cluster.Name)
	cmd := append([]string{"./garage"}, args...)
	return gc.execInPod(ctx, podName, cmd, nil)
}

// ExecCommandInPod executes a garage CLI command in a specific pod
func (gc *GarageClient) ExecCommandInPod(ctx context.Context, podName string, args ...string) (string, error) {
	cmd := append([]string{"./garage"}, args...)
	return gc.execInPod(ctx, podName, cmd, nil)
}

// ExecJSON executes a garage json-api command with JSON payload
func (gc *GarageClient) ExecJSON(ctx context.Context, endpoint string, payload interface{}) (string, error) {
	// Marshal payload to JSON
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("failed to marshal JSON payload: %w", err)
	}

	podName := fmt.Sprintf("%s-0", gc.cluster.Name)
	// Pass JSON as command argument, not via stdin
	cmd := []string{"./garage", "json-api", endpoint, string(jsonData)}

	return gc.execInPod(ctx, podName, cmd, nil)
}

// execInPod executes a command in a pod with optional stdin data
func (gc *GarageClient) execInPod(ctx context.Context, podName string, cmd []string, stdin []byte) (string, error) {
	hasStdin := len(stdin) > 0

	req := gc.clientset.CoreV1().RESTClient().
		Post().
		Resource("pods").
		Namespace(gc.cluster.Namespace).
		Name(podName).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Command:   cmd,
			Container: "garage",
			Stdin:     hasStdin,
			Stdout:    true,
			Stderr:    true,
			TTY:       false,
		}, scheme.ParameterCodec)

	exec, err := remotecommand.NewSPDYExecutor(gc.config, "POST", req.URL())
	if err != nil {
		return "", fmt.Errorf("failed to create executor: %w", err)
	}

	var stdout, stderr bytes.Buffer
	streamOpts := remotecommand.StreamOptions{
		Stdout: &stdout,
		Stderr: &stderr,
	}

	if hasStdin {
		streamOpts.Stdin = bytes.NewBuffer(stdin)
	}

	err = exec.StreamWithContext(ctx, streamOpts)
	if err != nil {
		return stdout.String(), fmt.Errorf("command execution failed: %w, stderr: %s", err, stderr.String())
	}

	return stdout.String(), nil
}
