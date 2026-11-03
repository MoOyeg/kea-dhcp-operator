/*
Copyright 2026.

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
	"encoding/base64"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"

	keav1alpha1 "github.com/openshift/ocp-kea-dhcp/api/v1alpha1"
	"github.com/openshift/ocp-kea-dhcp/internal/resources"
)

// KeaStorkServerReconciler reconciles a KeaStorkServer object.
type KeaStorkServerReconciler struct {
	client.Client
	Scheme      *runtime.Scheme
	IsOpenShift bool
	Recorder    record.EventRecorder

	// FetchServerToken reads the agent token from a running Stork server.
	// Defaults to fetchStorkServerToken; tests replace it.
	FetchServerToken storkTokenFetcher

	// ConnectStork opens the Stork session used to keep agent machines in
	// step with their pods. Defaults to connectStork; tests replace it.
	ConnectStork storkMachineConnector
}

const (
	// storkTokenRetryInterval is how soon to retry a failed token sync.
	storkTokenRetryInterval = 30 * time.Second
	// storkTokenResyncInterval re-reads the token so a regeneration in the
	// Stork UI reaches the agents.
	storkTokenResyncInterval = 5 * time.Minute
	// storkMachineRecheckInterval is how soon to look again while an agent is
	// still registering or its machine has no state yet.
	storkMachineRecheckInterval = 30 * time.Second
)

// +kubebuilder:rbac:groups=kea.openshift.io,resources=keastorkservers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kea.openshift.io,resources=keastorkservers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=kea.openshift.io,resources=keastorkservers/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch
// +kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=get;list;watch

// Reconcile reconciles the KeaStorkServer custom resource.
func (r *KeaStorkServerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// 1. Fetch the CR.
	server := &keav1alpha1.KeaStorkServer{}
	if err := r.Get(ctx, req.NamespacedName, server); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("KeaStorkServer resource not found, ignoring")
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	logger.Info("reconciling KeaStorkServer", "name", server.Name)

	component := resources.StorkServerComponent
	labels := resources.CommonLabels(server.Name, component)

	// 2. Build database credential env vars via SecretKeyRef.
	secretEnvVars := []corev1.EnvVar{
		{
			Name: "STORK_DATABASE_USER_NAME",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: server.Spec.Database.CredentialsSecretRef,
					Key:                  secretKeyUsername,
				},
			},
		},
		{
			Name: "STORK_DATABASE_PASSWORD",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: server.Spec.Database.CredentialsSecretRef,
					Key:                  secretKeyPassword,
				},
			},
		},
	}

	// 3. Resolve parameters.
	port := resources.DefaultStorkServerPort
	if server.Spec.Port != nil {
		port = *server.Spec.Port
	}

	dbPort := int32(5432)
	if server.Spec.Database.Port != nil {
		dbPort = *server.Spec.Database.Port
	}

	dbName := "stork"
	if server.Spec.Database.Name != "" {
		dbName = server.Spec.Database.Name
	}

	dbSSLMode := "disable"
	if server.Spec.Database.SSLMode != "" {
		dbSSLMode = server.Spec.Database.SSLMode
	}

	enableMetrics := true
	if server.Spec.EnableMetrics != nil {
		enableMetrics = *server.Spec.EnableMetrics
	}

	var nodeSelector map[string]string
	var tolerations []corev1.Toleration
	var affinity *corev1.Affinity
	var podAnnotations map[string]string
	if server.Spec.Placement != nil {
		nodeSelector = server.Spec.Placement.NodeSelector
		tolerations = server.Spec.Placement.Tolerations
		affinity = server.Spec.Placement.Affinity
		podAnnotations = server.Spec.Placement.PodAnnotations
	}

	// 4. Reconcile ServiceAccount.
	sa := resources.BuildServiceAccount(server.Namespace, server.Name, labels)
	if err := reconcileResource(ctx, r.Client, r.Scheme, server, sa); err != nil {
		return ctrl.Result{}, fmt.Errorf("reconciling ServiceAccount: %w", err)
	}

	// 5. Reconcile admin credentials Secret.
	adminSecretName := resources.StorkAdminSecretName(server.Name)
	adminSecret := &corev1.Secret{}
	adminSecretKey := types.NamespacedName{Name: adminSecretName, Namespace: server.Namespace}
	if err := r.Get(ctx, adminSecretKey, adminSecret); errors.IsNotFound(err) {
		// Generate a random password on first creation only.
		// Password policy: 12+ chars, uppercase, special char.
		passwordBytes := make([]byte, 24)
		if _, err := rand.Read(passwordBytes); err != nil {
			return ctrl.Result{}, fmt.Errorf("generating admin password: %w", err)
		}
		password := base64.RawURLEncoding.EncodeToString(passwordBytes) + "!A"
		adminSecret = resources.BuildStorkServerAdminSecret(server.Namespace, adminSecretName, labels, password)
		if err := reconcileResource(ctx, r.Client, r.Scheme, server, adminSecret); err != nil {
			return ctrl.Result{}, fmt.Errorf("reconciling admin Secret: %w", err)
		}
	} else if err != nil {
		return ctrl.Result{}, fmt.Errorf("fetching admin Secret: %w", err)
	}

	// 6. Reconcile server agent token Secret. Stork generates the real token
	// itself, so the Secret starts empty and is filled by syncServerToken once
	// the server is up. Agents register without a token until then.
	tokenSecretName := resources.StorkServerTokenSecretName(server.Name)
	tokenSecret := &corev1.Secret{}
	tokenSecretKey := types.NamespacedName{Name: tokenSecretName, Namespace: server.Namespace}
	if err := r.Get(ctx, tokenSecretKey, tokenSecret); errors.IsNotFound(err) {
		tokenSecret = resources.BuildStorkServerTokenSecret(server.Namespace, tokenSecretName, labels, "")
		if err := reconcileResource(ctx, r.Client, r.Scheme, server, tokenSecret); err != nil {
			return ctrl.Result{}, fmt.Errorf("reconciling server token Secret: %w", err)
		}
	} else if err != nil {
		return ctrl.Result{}, fmt.Errorf("fetching server token Secret: %w", err)
	}

	// 7. Reconcile Deployment.
	dp := resources.StorkServerParams{
		Namespace:          server.Namespace,
		CRName:             server.Name,
		Image:              server.Spec.Container.Image,
		ImagePullPolicy:    server.Spec.Container.ImagePullPolicy,
		Replicas:           server.Spec.Replicas,
		Resources:          server.Spec.Container.Resources,
		Port:               port,
		EnableMetrics:      enableMetrics,
		ImagePullSecrets:   server.Spec.Container.ImagePullSecrets,
		ServiceAccountName: resources.ServiceAccountName(server.Name),
		DBHost:             server.Spec.Database.Host,
		DBPort:             dbPort,
		DBName:             dbName,
		DBSSLMode:          dbSSLMode,
		NodeSelector:       nodeSelector,
		Tolerations:        tolerations,
		Affinity:           affinity,
		PodAnnotations:     podAnnotations,
		SecretEnvVars:      secretEnvVars,
		AdminSecretName:    adminSecretName,
	}
	deploy := resources.BuildStorkServerDeployment(dp)
	if err := reconcileResource(ctx, r.Client, r.Scheme, server, deploy); err != nil {
		return ctrl.Result{}, fmt.Errorf("reconciling Deployment: %w", err)
	}

	// 8. Reconcile Service.
	svc := resources.BuildStorkServerService(server.Namespace, server.Name, port)
	if err := reconcileResource(ctx, r.Client, r.Scheme, server, svc); err != nil {
		return ctrl.Result{}, fmt.Errorf("reconciling Service: %w", err)
	}

	// 9. On OpenShift, create a Route for the Stork web UI.
	if r.IsOpenShift {
		route := resources.BuildStorkServerRoute(server.Namespace, server.Name)
		if err := reconcileRoute(ctx, r.Client, r.Scheme, server, route); err != nil {
			logger.Error(err, "failed to reconcile Stork server Route")
		}

		// Extract the Route hostname for the status URL.
		routeName := resources.ServiceName(server.Name, resources.StorkServerComponent)
		server.Status.URL = fmt.Sprintf("https://%s-%s.apps.%s",
			routeName, server.Namespace, "cluster.local")
	}

	// 10. Update status.
	currentDeploy := &appsv1.Deployment{}
	var readyReplicas int32
	deployKey := types.NamespacedName{
		Name:      resources.DeploymentName(server.Name, component),
		Namespace: server.Namespace,
	}
	if err := r.Get(ctx, deployKey, currentDeploy); err == nil {
		readyReplicas = currentDeploy.Status.ReadyReplicas
	}

	replicas := int32(1)
	if server.Spec.Replicas != nil {
		replicas = *server.Spec.Replicas
	}

	server.Status.ReadyReplicas = readyReplicas
	server.Status.ObservedGeneration = server.Generation
	server.Status.AdminSecretName = adminSecretName
	server.Status.ServerTokenSecretName = tokenSecretName

	if readyReplicas > 0 && readyReplicas == replicas {
		server.Status.Phase = keav1alpha1.PhaseRunning
		setCondition(&server.Status.Conditions, keav1alpha1.ConditionTypeReady, "True", "DeploymentReady", "All replicas are ready")
		setCondition(&server.Status.Conditions, keav1alpha1.ConditionTypeProgressing, "False", "DeploymentComplete", "Deployment is complete")
	} else {
		server.Status.Phase = keav1alpha1.PhaseProgressing
		setCondition(&server.Status.Conditions, keav1alpha1.ConditionTypeReady, "False", "DeploymentProgressing",
			fmt.Sprintf("%d/%d replicas ready", readyReplicas, replicas))
		setCondition(&server.Status.Conditions, keav1alpha1.ConditionTypeProgressing, "True", "DeploymentProgressing", "Waiting for replicas")
	}

	// Read the actual Route hostname from the cluster if on OpenShift.
	if r.IsOpenShift {
		routeURL := r.getRouteURL(ctx, server.Namespace, resources.ServiceName(server.Name, resources.StorkServerComponent))
		if routeURL != "" {
			server.Status.URL = routeURL
		}
	}

	// 11. Sync the agent token and the agent machines with the running server.
	var result ctrl.Result
	if readyReplicas > 0 {
		result.RequeueAfter = r.syncWithStorkServer(ctx, server, adminSecretName, tokenSecretName)
	}

	if err := r.Status().Update(ctx, server); err != nil {
		return ctrl.Result{}, fmt.Errorf("updating status: %w", err)
	}

	r.Recorder.Event(server, corev1.EventTypeNormal, "Reconciled", "Successfully reconciled KeaStorkServer")

	return result, nil
}

// syncServerToken logs into the Stork server with the generated admin
// credentials, reads the agent token it accepts, and writes it into the token
// Secret when it differs. KeaDhcp4Server/KeaDhcp6Server watch that Secret and
// roll their stork-agent sidecars onto the new token.
func (r *KeaStorkServerReconciler) syncServerToken(ctx context.Context, server *keav1alpha1.KeaStorkServer, adminSecretName, tokenSecretName string) error {
	adminRef := &corev1.LocalObjectReference{Name: adminSecretName}
	username, err := resolveSecretValue(ctx, r.Client, server.Namespace, adminRef, secretKeyUsername)
	if err != nil {
		return err
	}
	password, err := resolveSecretValue(ctx, r.Client, server.Namespace, adminRef, secretKeyPassword)
	if err != nil {
		return err
	}

	fetch := r.FetchServerToken
	if fetch == nil {
		fetch = fetchStorkServerToken
	}
	token, err := fetch(ctx, storkServerInfoFromCR(server).ServerURL, username, password)
	if err != nil {
		return err
	}

	secret := &corev1.Secret{}
	if err := r.Get(ctx, types.NamespacedName{Name: tokenSecretName, Namespace: server.Namespace}, secret); err != nil {
		return fmt.Errorf("fetching token Secret: %w", err)
	}
	if string(secret.Data[secretKeyToken]) == token {
		return nil
	}
	if secret.Data == nil {
		secret.Data = map[string][]byte{}
	}
	secret.Data[secretKeyToken] = []byte(token)
	if err := r.Update(ctx, secret); err != nil {
		return fmt.Errorf("updating token Secret: %w", err)
	}
	log.FromContext(ctx).Info("updated Stork agent token Secret from the Stork server", "secret", tokenSecretName)
	return nil
}

// syncWithStorkServer copies the server's real agent token into the token
// Secret, then keeps the server's machines in step with the agent pods. It
// records the token state in the ServerTokenSynced condition and returns how
// soon to reconcile again.
func (r *KeaStorkServerReconciler) syncWithStorkServer(ctx context.Context, server *keav1alpha1.KeaStorkServer, adminSecretName, tokenSecretName string) time.Duration {
	logger := log.FromContext(ctx)
	if err := r.syncServerToken(ctx, server, adminSecretName, tokenSecretName); err != nil {
		logger.Info("Stork server token not synced yet", "reason", err.Error())
		setCondition(&server.Status.Conditions, keav1alpha1.ConditionTypeServerTokenSynced,
			keav1alpha1.ConditionStatusFalse, "TokenSyncFailed", err.Error())
		return storkTokenRetryInterval
	}
	setCondition(&server.Status.Conditions, keav1alpha1.ConditionTypeServerTokenSynced,
		keav1alpha1.ConditionStatusTrue, "TokenSynced", "Agent token Secret matches the Stork server")

	pending, err := r.syncAgentMachines(ctx, server, adminSecretName)
	if err != nil {
		logger.Info("Stork agent machines not checked", "reason", err.Error())
	}
	if err != nil || pending {
		return storkMachineRecheckInterval
	}
	return storkTokenResyncInterval
}

// syncAgentMachines logs into the Stork server and runs reconcileAgentMachines.
func (r *KeaStorkServerReconciler) syncAgentMachines(ctx context.Context, server *keav1alpha1.KeaStorkServer, adminSecretName string) (bool, error) {
	adminRef := &corev1.LocalObjectReference{Name: adminSecretName}
	username, err := resolveSecretValue(ctx, r.Client, server.Namespace, adminRef, secretKeyUsername)
	if err != nil {
		return false, err
	}
	password, err := resolveSecretValue(ctx, r.Client, server.Namespace, adminRef, secretKeyPassword)
	if err != nil {
		return false, err
	}
	connect := r.ConnectStork
	if connect == nil {
		connect = connectStork
	}
	api, err := connect(ctx, storkServerInfoFromCR(server).ServerURL, username, password)
	if err != nil {
		return false, err
	}
	return reconcileAgentMachines(ctx, r.Client, server, api)
}

// getRouteURL reads the actual hostname from an OpenShift Route and returns the full URL.
func (r *KeaStorkServerReconciler) getRouteURL(ctx context.Context, namespace, name string) string {
	route := resources.BuildStorkServerRoute(namespace, "")
	route.SetName(name)
	route.SetNamespace(namespace)

	key := types.NamespacedName{Name: name, Namespace: namespace}
	if err := r.Get(ctx, key, route); err != nil {
		return ""
	}

	host, found, err := unstructuredNestedString(route.Object, "spec", "host")
	if err != nil || !found || host == "" {
		// Try status.ingress[0].host
		ingress, found, err := unstructuredNestedSlice(route.Object, "status", "ingress")
		if err != nil || !found || len(ingress) == 0 {
			return ""
		}
		if first, ok := ingress[0].(map[string]interface{}); ok {
			if h, ok := first["host"].(string); ok && h != "" {
				host = h
			}
		}
	}

	if host == "" {
		return ""
	}
	return fmt.Sprintf("https://%s", host)
}

// unstructuredNestedString extracts a nested string from an unstructured object.
func unstructuredNestedString(obj map[string]interface{}, fields ...string) (string, bool, error) {
	val, found := nestedField(obj, fields...)
	if !found {
		return "", found, nil
	}
	s, ok := val.(string)
	if !ok {
		return "", true, fmt.Errorf("field is not a string")
	}
	return s, true, nil
}

// unstructuredNestedSlice extracts a nested slice from an unstructured object.
func unstructuredNestedSlice(obj map[string]interface{}, fields ...string) ([]interface{}, bool, error) {
	val, found := nestedField(obj, fields...)
	if !found {
		return nil, found, nil
	}
	s, ok := val.([]interface{})
	if !ok {
		return nil, true, fmt.Errorf("field is not a slice")
	}
	return s, true, nil
}

// nestedField walks a nested map to extract a value.
func nestedField(obj map[string]interface{}, fields ...string) (interface{}, bool) {
	var val interface{} = obj
	for _, field := range fields {
		m, ok := val.(map[string]interface{})
		if !ok {
			return nil, false
		}
		val, ok = m[field]
		if !ok {
			return nil, false
		}
	}
	return val, true
}

// SetupWithManager sets up the controller with the Manager.
func (r *KeaStorkServerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&keav1alpha1.KeaStorkServer{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.ServiceAccount{}).
		Owns(&corev1.Secret{}).
		// Agent pods coming and going changes which machines need a refresh
		// or removal. Only labelled pods are cached (see cmd/main.go).
		Watches(&corev1.Pod{}, handler.EnqueueRequestsFromMapFunc(storkServersForAgentPod)).
		Complete(r)
}
