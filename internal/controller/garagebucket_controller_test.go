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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	garagev1alpha1 "github.com/dimedis-gmbh/garage-operator/api/v1alpha1"
)

var _ = Describe("GarageBucket Controller", func() {
	Context("When reconciling a resource", func() {
		const (
			bucketName  = "test-bucket"
			clusterName = "test-cluster"
			namespace   = "default"
		)

		ctx := context.Background()

		bucketNamespacedName := types.NamespacedName{
			Name:      bucketName,
			Namespace: namespace,
		}

		clusterNamespacedName := types.NamespacedName{
			Name:      clusterName,
			Namespace: namespace,
		}

		BeforeEach(func() {
			By("creating the GarageCluster")
			err := k8sClient.Get(ctx, clusterNamespacedName, &garagev1alpha1.GarageCluster{})
			if err != nil && errors.IsNotFound(err) {
				cluster := &garagev1alpha1.GarageCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name:      clusterName,
						Namespace: namespace,
					},
					Spec: garagev1alpha1.GarageClusterSpec{
						ReplicaCount:      3,
						ReplicationFactor: 2,
						ConsistencyMode:   "consistent",
					},
				}
				Expect(k8sClient.Create(ctx, cluster)).To(Succeed())
			}

			By("creating the GarageBucket resource")
			err = k8sClient.Get(ctx, bucketNamespacedName, &garagev1alpha1.GarageBucket{})
			if err != nil && errors.IsNotFound(err) {
				maxObjects := int64(1000)
				bucket := &garagev1alpha1.GarageBucket{
					ObjectMeta: metav1.ObjectMeta{
						Name:      bucketName,
						Namespace: namespace,
					},
					Spec: garagev1alpha1.GarageBucketSpec{
						ClusterRef: garagev1alpha1.ClusterReference{
							Name: clusterName,
						},
						BucketName: "my-test-bucket",
						PublicRead: false,
						Quotas: &garagev1alpha1.BucketQuotas{
							MaxSize:    "10Gi",
							MaxObjects: &maxObjects,
						},
						Keys: []garagev1alpha1.BucketKey{
							{
								Name: "test-key",
								Permissions: garagev1alpha1.KeyPermissions{
									Read:  true,
									Write: true,
									Owner: false,
								},
							},
						},
					},
				}
				Expect(k8sClient.Create(ctx, bucket)).To(Succeed())
			}
		})

		AfterEach(func() {
			By("Cleanup the GarageBucket resource")
			bucket := &garagev1alpha1.GarageBucket{}
			err := k8sClient.Get(ctx, bucketNamespacedName, bucket)
			if err == nil {
				Expect(k8sClient.Delete(ctx, bucket)).To(Succeed())
			}

			By("Cleanup the GarageCluster resource")
			cluster := &garagev1alpha1.GarageCluster{}
			err = k8sClient.Get(ctx, clusterNamespacedName, cluster)
			if err == nil {
				Expect(k8sClient.Delete(ctx, cluster)).To(Succeed())
			}
		})

		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")
			controllerReconciler := &GarageBucketReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: bucketNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Checking if the bucket resource was created")
			bucket := &garagev1alpha1.GarageBucket{}
			err = k8sClient.Get(ctx, bucketNamespacedName, bucket)
			Expect(err).NotTo(HaveOccurred())
			Expect(bucket.Spec.BucketName).To(Equal("my-test-bucket"))
			Expect(bucket.Spec.PublicRead).To(BeFalse())
			Expect(bucket.Spec.Keys).To(HaveLen(1))
			Expect(bucket.Spec.Keys[0].Name).To(Equal("test-key"))
			Expect(bucket.Spec.Keys[0].Permissions.Read).To(BeTrue())
			Expect(bucket.Spec.Keys[0].Permissions.Write).To(BeTrue())
			Expect(bucket.Spec.Keys[0].Permissions.Owner).To(BeFalse())
		})
	})
})
