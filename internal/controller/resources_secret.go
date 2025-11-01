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
	"crypto/rand"
	"encoding/hex"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	garagev1alpha1 "github.com/dimedis-gmbh/garage-operator/api/v1alpha1"
)

func (r *GarageClusterReconciler) reconcileSecret(ctx context.Context, gc *garagev1alpha1.GarageCluster) error {
	secretName := gc.Name + "-rpc-secret"
	secret := &corev1.Secret{}
	err := r.Get(ctx, types.NamespacedName{Name: secretName, Namespace: gc.Namespace}, secret)

	if err != nil && errors.IsNotFound(err) {
		// Create new secret with generated RPC secret
		secret = &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      secretName,
				Namespace: gc.Namespace,
			},
			StringData: map[string]string{
				"rpc-secret": generateSecret(32),
			},
		}

		if err := controllerutil.SetControllerReference(gc, secret, r.Scheme); err != nil {
			return err
		}

		return r.Create(ctx, secret)
	}

	return err
}

func generateSecret(length int) string {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		panic(err)
	}
	return hex.EncodeToString(bytes)
}
