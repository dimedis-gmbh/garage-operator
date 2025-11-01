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

	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	garagev1alpha1 "github.com/dimedis-gmbh/garage-operator/api/v1alpha1"
)

func (r *GarageClusterReconciler) reconcileIngress(ctx context.Context, gc *garagev1alpha1.GarageCluster) error {
	// If ingress is not enabled or not configured, delete any existing ingress
	if gc.Spec.Ingress == nil || !gc.Spec.Ingress.Enabled {
		ingress := &networkingv1.Ingress{}
		err := r.Get(ctx, types.NamespacedName{Name: gc.Name, Namespace: gc.Namespace}, ingress)
		if err == nil {
			// Ingress exists but should not, delete it
			return r.Delete(ctx, ingress)
		}
		if errors.IsNotFound(err) {
			// Ingress doesn't exist, which is correct
			return nil
		}
		return err
	}

	// Build ingress spec
	pathTypePrefix := networkingv1.PathTypePrefix
	ingress := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:        gc.Name,
			Namespace:   gc.Namespace,
			Annotations: gc.Spec.Ingress.Annotations,
		},
		Spec: networkingv1.IngressSpec{
			Rules: []networkingv1.IngressRule{
				{
					Host: gc.Spec.Ingress.Host,
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: []networkingv1.HTTPIngressPath{
								{
									Path:     "/",
									PathType: &pathTypePrefix,
									Backend: networkingv1.IngressBackend{
										Service: &networkingv1.IngressServiceBackend{
											Name: gc.Name,
											Port: networkingv1.ServiceBackendPort{
												Name: "s3-api",
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	// Add IngressClassName if specified
	if gc.Spec.Ingress.ClassName != "" {
		ingress.Spec.IngressClassName = &gc.Spec.Ingress.ClassName
	}

	// Add TLS configuration if enabled
	if gc.Spec.Ingress.TLS != nil && gc.Spec.Ingress.TLS.Enabled {
		secretName := gc.Name + "-tls"
		if gc.Spec.Ingress.TLS.SecretName != "" {
			secretName = gc.Spec.Ingress.TLS.SecretName
		}

		ingress.Spec.TLS = []networkingv1.IngressTLS{
			{
				Hosts:      []string{gc.Spec.Ingress.Host},
				SecretName: secretName,
			},
		}
	}

	if err := controllerutil.SetControllerReference(gc, ingress, r.Scheme); err != nil {
		return err
	}

	found := &networkingv1.Ingress{}
	err := r.Get(ctx, types.NamespacedName{Name: ingress.Name, Namespace: ingress.Namespace}, found)
	if err != nil && errors.IsNotFound(err) {
		log.FromContext(ctx).Info("Creating new Ingress")
		return r.Create(ctx, ingress)
	} else if err == nil {
		// Update existing ingress
		found.Annotations = ingress.Annotations
		found.Spec = ingress.Spec
		log.FromContext(ctx).Info("Updating Ingress")
		return r.Update(ctx, found)
	}
	return err
}
