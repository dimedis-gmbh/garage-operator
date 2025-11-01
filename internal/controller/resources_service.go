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
	"reflect"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	garagev1alpha1 "github.com/dimedis-gmbh/garage-operator/api/v1alpha1"
)

func (r *GarageClusterReconciler) reconcileService(ctx context.Context, gc *garagev1alpha1.GarageCluster) error {
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      gc.Name,
			Namespace: gc.Namespace,
		},
		Spec: corev1.ServiceSpec{
			ClusterIP: "None", // Headless service for StatefulSet
			Selector: map[string]string{
				"app": gc.Name,
			},
			Ports: []corev1.ServicePort{
				{
					Name:       "s3-api",
					Port:       3900,
					TargetPort: intstr.FromInt(3900),
					Protocol:   corev1.ProtocolTCP,
				},
				{
					Name:       "rpc",
					Port:       3901,
					TargetPort: intstr.FromInt(3901),
					Protocol:   corev1.ProtocolTCP,
				},
				{
					Name:       "admin",
					Port:       3903,
					TargetPort: intstr.FromInt(3903),
					Protocol:   corev1.ProtocolTCP,
				},
			},
			PublishNotReadyAddresses: true,
			// Do not set SessionAffinity for headless services
		},
	}

	if err := controllerutil.SetControllerReference(gc, svc, r.Scheme); err != nil {
		return err
	}

	found := &corev1.Service{}
	err := r.Get(ctx, types.NamespacedName{Name: svc.Name, Namespace: svc.Namespace}, found)
	if err != nil && errors.IsNotFound(err) {
		log.FromContext(ctx).Info("Creating new Service")
		return r.Create(ctx, svc)
	} else if err == nil {
		// Only update if there are actual changes to avoid unnecessary API calls
		needsUpdate := false
		if !reflect.DeepEqual(found.Spec.Selector, svc.Spec.Selector) {
			found.Spec.Selector = svc.Spec.Selector
			needsUpdate = true
		}
		if !reflect.DeepEqual(found.Spec.Ports, svc.Spec.Ports) {
			found.Spec.Ports = svc.Spec.Ports
			needsUpdate = true
		}
		if found.Spec.PublishNotReadyAddresses != svc.Spec.PublishNotReadyAddresses {
			found.Spec.PublishNotReadyAddresses = svc.Spec.PublishNotReadyAddresses
			needsUpdate = true
		}
		if needsUpdate {
			log.FromContext(ctx).Info("Updating Service due to spec changes")
			return r.Update(ctx, found)
		}
		return nil
	}
	return err
}
