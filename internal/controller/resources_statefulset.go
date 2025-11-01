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

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	garagev1alpha1 "github.com/dimedis-gmbh/garage-operator/api/v1alpha1"
)

const (
	garageImage = "dxflrs/garage:v2.1.0"
)

func (r *GarageClusterReconciler) reconcileStatefulSet(ctx context.Context, gc *garagev1alpha1.GarageCluster) error {
	// Determine data volume configuration
	dataSize := "20Gi"
	dataStorageClass := ""
	if gc.Spec.Persistence != nil && gc.Spec.Persistence.Data != nil {
		if gc.Spec.Persistence.Data.Size != "" {
			dataSize = gc.Spec.Persistence.Data.Size
		}
		dataStorageClass = gc.Spec.Persistence.Data.StorageClass
	}

	// Determine meta volume configuration
	metaSize := "20Gi"
	metaStorageClass := ""
	if gc.Spec.Persistence != nil && gc.Spec.Persistence.Meta != nil {
		if gc.Spec.Persistence.Meta.Size != "" {
			metaSize = gc.Spec.Persistence.Meta.Size
		}
		metaStorageClass = gc.Spec.Persistence.Meta.StorageClass
	}

	dataSizeQuantity, err := resource.ParseQuantity(dataSize)
	if err != nil {
		return fmt.Errorf("invalid data volume size: %w", err)
	}

	metaSizeQuantity, err := resource.ParseQuantity(metaSize)
	if err != nil {
		return fmt.Errorf("invalid meta volume size: %w", err)
	}

	// Default resources if not specified
	resources := corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("100m"),
			corev1.ResourceMemory: resource.MustParse("256Mi"),
		},
	}
	if gc.Spec.Resources != nil {
		resources = *gc.Spec.Resources
	}

	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      gc.Name,
			Namespace: gc.Namespace,
		},
		Spec: appsv1.StatefulSetSpec{
			ServiceName: gc.Name,
			Replicas:    &gc.Spec.ReplicaCount,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": gc.Name},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": gc.Name},
				},
				Spec: corev1.PodSpec{
					ServiceAccountName: gc.Name,
					Affinity: &corev1.Affinity{
						PodAntiAffinity: &corev1.PodAntiAffinity{
							RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{
								{
									LabelSelector: &metav1.LabelSelector{
										MatchLabels: map[string]string{"app": gc.Name},
									},
									TopologyKey: "kubernetes.io/hostname",
								},
							},
						},
					},
					Containers: []corev1.Container{
						{
							Name:    "garage",
							Image:   garageImage,
							Command: []string{"/garage"},
							Args:    []string{"server"},
							Ports: []corev1.ContainerPort{
								{Name: "s3-api", ContainerPort: 3900},
								{Name: "rpc", ContainerPort: 3901},
								{Name: "admin", ContainerPort: 3903},
							},
							VolumeMounts: []corev1.VolumeMount{
								{Name: "meta", MountPath: "/mnt/meta"},
								{Name: "data", MountPath: "/mnt/data"},
								{
									Name:      "config",
									MountPath: "/etc/garage.toml",
									SubPath:   "garage.toml",
								},
								{
									Name:      "rpc-secret",
									MountPath: "/etc/garage",
									ReadOnly:  true,
								},
							},
							Resources: resources,
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "config",
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: gc.Name + "-config",
									},
								},
							},
						},
						{
							Name: "rpc-secret",
							VolumeSource: corev1.VolumeSource{
								Secret: &corev1.SecretVolumeSource{
									SecretName:  gc.Name + "-rpc-secret",
									DefaultMode: func() *int32 { mode := int32(0400); return &mode }(),
								},
							},
						},
					},
				},
			},
			VolumeClaimTemplates: []corev1.PersistentVolumeClaim{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "meta"},
					Spec: corev1.PersistentVolumeClaimSpec{
						AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
						Resources: corev1.VolumeResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceStorage: metaSizeQuantity,
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "data"},
					Spec: corev1.PersistentVolumeClaimSpec{
						AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
						Resources: corev1.VolumeResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceStorage: dataSizeQuantity,
							},
						},
					},
				},
			},
		},
	}

	// Set storageClassName for meta volume if specified
	if metaStorageClass != "" {
		sts.Spec.VolumeClaimTemplates[0].Spec.StorageClassName = &metaStorageClass
	}

	// Set storageClassName for data volume if specified
	if dataStorageClass != "" {
		sts.Spec.VolumeClaimTemplates[1].Spec.StorageClassName = &dataStorageClass
	}

	if err := controllerutil.SetControllerReference(gc, sts, r.Scheme); err != nil {
		return err
	}

	found := &appsv1.StatefulSet{}
	err = r.Get(ctx, types.NamespacedName{Name: sts.Name, Namespace: sts.Namespace}, found)
	if err != nil && errors.IsNotFound(err) {
		return r.Create(ctx, sts)
	} else if err != nil {
		return err
	}

	// Update only if there are changes (simplified check)
	if found.Spec.Replicas == nil || *found.Spec.Replicas != gc.Spec.ReplicaCount {
		found.Spec.Replicas = &gc.Spec.ReplicaCount
		return r.Update(ctx, found)
	}

	return nil
}
