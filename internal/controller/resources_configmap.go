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

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	garagev1alpha1 "github.com/dimedis-gmbh/garage-operator/api/v1alpha1"
)

func (r *GarageClusterReconciler) reconcileConfigMap(ctx context.Context, gc *garagev1alpha1.GarageCluster) error {
	region := "garage"
	if gc.Spec.S3Api != nil && gc.Spec.S3Api.Region != "" {
		region = gc.Spec.S3Api.Region
	}

	rootDomain := ".s3.garage.localhost"
	if gc.Spec.S3Api != nil && gc.Spec.S3Api.RootDomain != "" {
		rootDomain = gc.Spec.S3Api.RootDomain
	}

	// Default values matching Garage defaults
	replicationFactor := int32(3)
	if gc.Spec.ReplicationFactor > 0 {
		replicationFactor = gc.Spec.ReplicationFactor
	}

	consistencyMode := "consistent"
	if gc.Spec.ConsistencyMode != "" {
		consistencyMode = gc.Spec.ConsistencyMode
	}

	blockSize := "1Mi"
	if gc.Spec.BlockSize != "" {
		blockSize = gc.Spec.BlockSize
	}

	configData := fmt.Sprintf(`metadata_dir = "/mnt/meta"
data_dir = "/mnt/data"
db_engine = "sqlite"
block_size = "%s"
replication_factor = %d
consistency_mode = "%s"

rpc_bind_addr = "[::]:3901"
rpc_secret_file = "/etc/garage/rpc-secret"

bootstrap_peers = []

[kubernetes_discovery]
service_name = "%s"
namespace = "%s"
skip_crd = false

[s3_api]
s3_region = "%s"
api_bind_addr = "[::]:3900"
root_domain = "%s"

[admin]
api_bind_addr = "[::]:3903"
`, blockSize, replicationFactor, consistencyMode, gc.Name, gc.Namespace, region, rootDomain)

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      gc.Name + "-config",
			Namespace: gc.Namespace,
		},
		Data: map[string]string{
			"garage.toml": configData,
		},
	}

	if err := controllerutil.SetControllerReference(gc, cm, r.Scheme); err != nil {
		return err
	}

	found := &corev1.ConfigMap{}
	err := r.Get(ctx, types.NamespacedName{Name: cm.Name, Namespace: cm.Namespace}, found)
	if err != nil && errors.IsNotFound(err) {
		return r.Create(ctx, cm)
	} else if err != nil {
		return err
	}

	// Update if changed
	found.Data = cm.Data
	return r.Update(ctx, found)
}
