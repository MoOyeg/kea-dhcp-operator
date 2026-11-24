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
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	keav1alpha1 "github.com/openshift/ocp-kea-dhcp/api/v1alpha1"
)

// requestLog records the names a controller was asked to reconcile.
type requestLog struct {
	mu    sync.Mutex
	names []string
}

func (l *requestLog) Reconcile(_ context.Context, req reconcile.Request) (reconcile.Result, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.names = append(l.names, req.Name)
	return reconcile.Result{}, nil
}

func (l *requestLog) seen(name string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, n := range l.names {
		if n == name {
			return true
		}
	}
	return false
}

func (l *requestLog) reset() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.names = nil
}

// Calling Reconcile directly cannot show that a Secret update reaches the
// right CR. This runs a real manager against the envtest API server, with the
// same Secret map function the KeaDhcp4Server controller registers, and
// updates Secrets through the API. The manager only caches one namespace, so
// it does not interfere with the other specs.
var _ = Describe("Credential Secret watch", Ordered, func() {
	const (
		ns        = "credential-watch"
		dependent = "uses-db-creds"
		bystander = "no-credentials"
		dbSecret  = "watched-db-creds"
		otherSec  = "not-referenced"
	)
	var (
		ctx    context.Context
		cancel context.CancelFunc
		log    = &requestLog{}
	)

	BeforeAll(func() {
		ctx, cancel = context.WithCancel(context.Background())
		Expect(k8sClient.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}})).To(Succeed())
		for _, name := range []string{dbSecret, otherSec} {
			Expect(k8sClient.Create(ctx, &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
				StringData: map[string]string{"username": testKeaName, "password": "initial"},
			})).To(Succeed())
		}
		iface := keav1alpha1.InterfacesConfig{Interfaces: []string{testIfaceEth0}}
		Expect(k8sClient.Create(ctx, &keav1alpha1.KeaDhcp4Server{
			ObjectMeta: metav1.ObjectMeta{Name: dependent, Namespace: ns},
			Spec: keav1alpha1.KeaDhcp4ServerSpec{InterfacesConfig: iface, LeaseDatabase: &keav1alpha1.DatabaseConfig{
				Type: keav1alpha1.DatabaseTypeMySQL, Name: testKeaName, Host: testDBHost,
				CredentialsSecretRef: &corev1.LocalObjectReference{Name: dbSecret},
			}},
		})).To(Succeed())
		Expect(k8sClient.Create(ctx, &keav1alpha1.KeaDhcp4Server{
			ObjectMeta: metav1.ObjectMeta{Name: bystander, Namespace: ns},
			Spec:       keav1alpha1.KeaDhcp4ServerSpec{InterfacesConfig: iface},
		})).To(Succeed())

		mgr, err := ctrl.NewManager(cfg, ctrl.Options{
			Scheme:                 scheme.Scheme,
			Cache:                  cache.Options{DefaultNamespaces: map[string]cache.Config{ns: {}}},
			Metrics:                metricsserver.Options{BindAddress: "0"},
			HealthProbeBindAddress: "0",
			Controller:             config.Controller{SkipNameValidation: ptr.To(true)},
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(ctrl.NewControllerManagedBy(mgr).
			Named("credential-watch-test").
			For(&keav1alpha1.KeaDhcp4Server{}).
			Watches(&corev1.Secret{}, handler.EnqueueRequestsFromMapFunc(dhcp4SecretMapFunc(mgr.GetClient()))).
			Complete(log)).To(Succeed())
		go func() {
			defer GinkgoRecover()
			Expect(mgr.Start(ctx)).To(Succeed())
		}()

		// The initial list reconciles both CRs once; start from a clean log.
		Eventually(func() bool { return log.seen(dependent) && log.seen(bystander) }, 30*time.Second).Should(BeTrue())
		time.Sleep(time.Second)
		log.reset()
	})

	AfterAll(func() {
		cancel()
		Expect(k8sClient.Delete(context.Background(), &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}})).To(Succeed())
	})

	updateSecret := func(name, password string) {
		s := &corev1.Secret{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: name, Namespace: ns}, s)).To(Succeed())
		s.StringData = map[string]string{"password": password}
		Expect(k8sClient.Update(ctx, s)).To(Succeed())
	}

	It("enqueues only the CR that references an updated Secret", func() {
		updateSecret(dbSecret, "rotated")
		Eventually(func() bool { return log.seen(dependent) }, 10*time.Second).Should(BeTrue())
		Consistently(func() bool { return log.seen(bystander) }, 2*time.Second).Should(BeFalse())
	})

	It("enqueues nothing for a Secret no CR references", func() {
		log.reset()
		updateSecret(otherSec, "changed")
		Consistently(func() bool { return log.seen(dependent) || log.seen(bystander) }, 2*time.Second).Should(BeFalse())
	})

	It("enqueues the dependent CR when its Secret is deleted", func() {
		log.reset()
		Expect(k8sClient.Delete(ctx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: dbSecret, Namespace: ns}})).To(Succeed())
		Eventually(func() bool { return log.seen(dependent) }, 10*time.Second).Should(BeTrue())
	})
})
