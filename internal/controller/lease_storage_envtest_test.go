/*
Copyright 2024.

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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	keav1alpha1 "github.com/openshift/ocp-kea-dhcp/api/v1alpha1"
)

// The CRD schema, not the controller, enforces the leaseStorage type and its
// default; check it against the real API server.
var _ = Describe("leaseStorage schema", func() {
	ctx := context.Background()
	newServer := func(name string, storage *keav1alpha1.LeaseStorage) *keav1alpha1.KeaDhcp4Server {
		return &keav1alpha1.KeaDhcp4Server{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNamespaceDefault},
			Spec: keav1alpha1.KeaDhcp4ServerSpec{
				InterfacesConfig: keav1alpha1.InterfacesConfig{Interfaces: []string{"eth0"}},
				LeaseStorage:     storage,
			},
		}
	}

	It("rejects an unknown storage type", func() {
		err := k8sClient.Create(ctx, newServer("lease-storage-bogus", &keav1alpha1.LeaseStorage{Type: "Bogus"}))
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("Unsupported value"))
	})

	It("accepts the documented types and defaults the type to Ephemeral", func() {
		for name, storage := range map[string]*keav1alpha1.LeaseStorage{
			"lease-storage-claim":    {Type: keav1alpha1.LeaseStorageExistingClaim, ClaimName: "pvc"},
			"lease-storage-template": {Type: keav1alpha1.LeaseStorageClaimTemplate},
		} {
			s := newServer(name, storage)
			Expect(k8sClient.Create(ctx, s)).To(Succeed())
			Expect(k8sClient.Delete(ctx, s)).To(Succeed())
		}
		unstructuredDefault := newServer("lease-storage-default", &keav1alpha1.LeaseStorage{})
		// An empty type is omitted on the wire, so the schema default applies.
		Expect(k8sClient.Create(ctx, unstructuredDefault)).To(Succeed())
		got := &keav1alpha1.KeaDhcp4Server{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(unstructuredDefault), got)).To(Succeed())
		Expect(got.Spec.LeaseStorage.Type).To(Equal(keav1alpha1.LeaseStorageEphemeral))
		Expect(k8sClient.Delete(ctx, got)).To(Succeed())
	})
})
