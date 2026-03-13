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
	"fmt"

	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
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
	"github.com/openshift/ocp-kea-dhcp/internal/kea"
	"github.com/openshift/ocp-kea-dhcp/internal/resources"
)

// KeaDhcp6ServerReconciler reconciles a KeaDhcp6Server object.
type KeaDhcp6ServerReconciler struct {
	client.Client
	Scheme      *runtime.Scheme
	IsOpenShift bool
	Recorder    record.EventRecorder
}

// +kubebuilder:rbac:groups=kea.openshift.io,resources=keadhcp6servers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kea.openshift.io,resources=keadhcp6servers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=kea.openshift.io,resources=keadhcp6servers/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=serviceaccounts,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=monitoring.coreos.com,resources=podmonitors,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=route.openshift.io,resources=routes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kea.openshift.io,resources=keastorkservers,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// Reconcile reconciles the KeaDhcp6Server custom resource.
func (r *KeaDhcp6ServerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// 1. Fetch the CR.
	server := &keav1alpha1.KeaDhcp6Server{}
	if err := r.Get(ctx, req.NamespacedName, server); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("KeaDhcp6Server resource not found, ignoring")
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	logger.Info("reconciling KeaDhcp6Server", "name", server.Name)

	// 2. Auto-fill empty HA peer URLs from the StatefulSet naming convention.
	component := "dhcp6"
	if server.Spec.HighAvailability != nil {
		haPort := int32(8000)
		if server.Spec.ControlSocket != nil && server.Spec.ControlSocket.SocketPort != nil {
			haPort = *server.Spec.ControlSocket.SocketPort
		}
		fillPeerURLs(server.Spec.HighAvailability, server.Namespace, server.Name, component, haPort)
	}

	// 3. Build env var references for database credentials.
	renderer := kea.NewDhcp6ConfigRenderer(&server.Spec)
	var secretEnvVars []corev1.EnvVar

	if server.Spec.LeaseDatabase != nil && server.Spec.LeaseDatabase.CredentialsSecretRef != nil {
		envs, userPh, passPh := dbCredentialEnvVars("KEA_LEASE_DB", server.Spec.LeaseDatabase)
		secretEnvVars = append(secretEnvVars, envs...)
		renderer.LeaseDBCreds = kea.DBCredentials{User: userPh, Password: passPh}
	}
	if server.Spec.HostsDatabase != nil && server.Spec.HostsDatabase.CredentialsSecretRef != nil {
		envs, userPh, passPh := dbCredentialEnvVars("KEA_HOSTS_DB", server.Spec.HostsDatabase)
		secretEnvVars = append(secretEnvVars, envs...)
		renderer.HostsDBCreds = kea.DBCredentials{User: userPh, Password: passPh}
	}
	for i := range server.Spec.HostsDatabases {
		if server.Spec.HostsDatabases[i].CredentialsSecretRef != nil {
			prefix := fmt.Sprintf("KEA_HOSTS_DBS_%d", i)
			envs, userPh, passPh := dbCredentialEnvVars(prefix, &server.Spec.HostsDatabases[i])
			secretEnvVars = append(secretEnvVars, envs...)
			for len(renderer.HostsDBsCreds) <= i {
				renderer.HostsDBsCreds = append(renderer.HostsDBsCreds, kea.DBCredentials{})
			}
			renderer.HostsDBsCreds[i] = kea.DBCredentials{User: userPh, Password: passPh}
		}
	}

	configJSON, err := renderer.RenderJSON()
	if err != nil {
		setCondition(&server.Status.Conditions, keav1alpha1.ConditionTypeConfigValid, "False", "RenderError", err.Error())
		server.Status.Phase = "Error"
		if statusErr := r.Status().Update(ctx, server); statusErr != nil {
			logger.Error(statusErr, "failed to update status after config render error")
		}
		r.Recorder.Eventf(server, corev1.EventTypeWarning, "ConfigRenderError", "Failed to render configuration: %v", err)
		return ctrl.Result{}, fmt.Errorf("rendering config: %w", err)
	}

	// 3. Compute config hash.
	configHash := kea.ComputeHash(configJSON)

	setCondition(&server.Status.Conditions, keav1alpha1.ConditionTypeConfigValid, "True", "ConfigRendered", "Configuration rendered successfully")

	labels := resources.CommonLabels(server.Name, component)

	// 4. Reconcile ConfigMap.
	cm := resources.BuildConfigMap(server.Namespace, server.Name, component, configJSON, labels)
	if err := reconcileResource(ctx, r.Client, r.Scheme, server, cm); err != nil {
		return ctrl.Result{}, fmt.Errorf("reconciling ConfigMap: %w", err)
	}

	// 5. Reconcile ServiceAccount.
	sa := resources.BuildServiceAccount(server.Namespace, server.Name, labels)
	if err := reconcileResource(ctx, r.Client, r.Scheme, server, sa); err != nil {
		return ctrl.Result{}, fmt.Errorf("reconciling ServiceAccount: %w", err)
	}

	// Auto-bind the kea-dhcp SCC to the service account in the default namespace.
	if r.IsOpenShift && server.Namespace == DefaultNamespace {
		if err := ensureSCCRoleBinding(ctx, r.Client, server.Namespace, server.Name); err != nil {
			logger.Error(err, "failed to ensure SCC RoleBinding")
		}
	}

	// 6. Build and reconcile Deployment.
	replicas := server.Spec.Replicas
	if replicas == nil {
		one := int32(1)
		replicas = &one
	}

	image := server.Spec.Container.Image
	hostNetwork := false
	if server.Spec.HostNetwork != nil {
		hostNetwork = *server.Spec.HostNetwork
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

	var tlsSecretName string
	if server.Spec.HighAvailability != nil && server.Spec.HighAvailability.TLS != nil {
		tlsSecretName = server.Spec.HighAvailability.TLS.SecretRef.Name
	}

	// Resolve Stork agent sidecar params (if enabled).
	var storkAgent *resources.StorkSidecarParams
	if server.Spec.Stork != nil && server.Spec.Stork.Enabled {
		storkCfg := server.Spec.Stork.DeepCopy()

		// Auto-discover Stork server if no explicit serverURL is set.
		if storkCfg.ServerURL == "" {
			if info := resolveStorkServer(ctx, r.Client, server.Namespace, storkCfg.StorkServerRef); info != nil {
				storkCfg.ServerURL = info.ServerURL
				storkCfg.ServerTokenSecretRef = &corev1.LocalObjectReference{Name: info.TokenSecretName}
				logger.Info("auto-discovered Stork server for agent registration", "serverURL", info.ServerURL)
			}
		}

		var serverToken string
		if storkCfg.ServerTokenSecretRef != nil {
			serverToken, _ = resolveSecretValue(ctx, r.Client, server.Namespace, storkCfg.ServerTokenSecretRef, "token")
		}
		var err error
		storkAgent, err = resources.ResolveStorkParams(storkCfg, serverToken)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("resolving stork agent params: %w", err)
		}
	}

	dp := resources.DeploymentParams{
		Namespace:          server.Namespace,
		CRName:             server.Name,
		Component:          component,
		Command:            resources.DefaultCommandForComponent(component),
		ConfigFileName:     resources.ConfigFileName(component),
		ConfigMapName:      resources.ConfigMapName(server.Name, component),
		Image:              image,
		ImagePullPolicy:    server.Spec.Container.ImagePullPolicy,
		Replicas:           replicas,
		Resources:          server.Spec.Container.Resources,
		HostNetwork:        hostNetwork,
		NodeSelector:       nodeSelector,
		Tolerations:        tolerations,
		Affinity:           affinity,
		TLSSecretName:      tlsSecretName,
		ServiceAccountName: resources.ServiceAccountName(server.Name),
		ConfigHash:         configHash,
		ImagePullSecrets:   server.Spec.Container.ImagePullSecrets,
		PodAnnotations:     podAnnotations,
		StorkAgent:         storkAgent,
		SecretEnvVars:      secretEnvVars,
	}
	deploy := resources.BuildDeployment(dp)
	if err := reconcileResource(ctx, r.Client, r.Scheme, server, deploy); err != nil {
		return ctrl.Result{}, fmt.Errorf("reconciling Deployment: %w", err)
	}

	// 7. Reconcile Service if control socket is HTTP.
	if server.Spec.ControlSocket != nil && server.Spec.ControlSocket.SocketType == "http" {
		port := int32(8000)
		if server.Spec.ControlSocket.SocketPort != nil {
			port = *server.Spec.ControlSocket.SocketPort
		}
		svc := resources.BuildService(resources.ServiceParams{
			Namespace: server.Namespace,
			CRName:    server.Name,
			Component: component,
			Port:      port,
			Protocol:  corev1.ProtocolTCP,
		})
		if err := reconcileResource(ctx, r.Client, r.Scheme, server, svc); err != nil {
			return ctrl.Result{}, fmt.Errorf("reconciling Service: %w", err)
		}
	}

	// 8. Reconcile PodMonitor and Stork metrics Service (when stork is enabled).
	if storkAgent != nil {
		pm := resources.BuildPodMonitor(server.Namespace, server.Name, component)
		if err := reconcileResource(ctx, r.Client, r.Scheme, server, pm); err != nil {
			logger.Error(err, "failed to reconcile PodMonitor (monitoring.coreos.com CRD may not be installed)")
		}

		storkSvc := resources.BuildStorkMetricsService(resources.StorkMetricsServiceParams{
			Namespace:      server.Namespace,
			CRName:         server.Name,
			Component:      component,
			AgentPort:      storkAgent.Port,
			PrometheusPort: storkAgent.PrometheusPort,
		})
		if err := reconcileResource(ctx, r.Client, r.Scheme, server, storkSvc); err != nil {
			return ctrl.Result{}, fmt.Errorf("reconciling Stork metrics Service: %w", err)
		}

		// On OpenShift, create a Route for the Stork Prometheus metrics endpoint.
		if r.IsOpenShift {
			route := resources.BuildStorkMetricsRoute(resources.StorkRouteParams{
				Namespace:      server.Namespace,
				CRName:         server.Name,
				Component:      component,
				PrometheusPort: storkAgent.PrometheusPort,
			})
			if err := reconcileRoute(ctx, r.Client, r.Scheme, server, route); err != nil {
				logger.Error(err, "failed to reconcile Stork metrics Route")
			}
		}
	}

	// 9. Update status.
	currentDeploy := &appsv1.Deployment{}
	var readyReplicas int32
	deployKey := types.NamespacedName{
		Name:      resources.DeploymentName(server.Name, component),
		Namespace: server.Namespace,
	}
	if err := r.Get(ctx, deployKey, currentDeploy); err == nil {
		readyReplicas = currentDeploy.Status.ReadyReplicas
	}

	server.Status.ReadyReplicas = readyReplicas
	server.Status.ConfigHash = configHash
	server.Status.ConfigMapRef = resources.ConfigMapName(server.Name, component)
	server.Status.ObservedGeneration = server.Generation

	if readyReplicas > 0 && readyReplicas == *replicas {
		server.Status.Phase = "Running"
		setCondition(&server.Status.Conditions, keav1alpha1.ConditionTypeReady, "True", "DeploymentReady", "All replicas are ready")
		setCondition(&server.Status.Conditions, keav1alpha1.ConditionTypeProgressing, "False", "DeploymentComplete", "Deployment is complete")
	} else {
		server.Status.Phase = "Progressing"
		setCondition(&server.Status.Conditions, keav1alpha1.ConditionTypeReady, "False", "DeploymentProgressing",
			fmt.Sprintf("%d/%d replicas ready", readyReplicas, *replicas))
		setCondition(&server.Status.Conditions, keav1alpha1.ConditionTypeProgressing, "True", "DeploymentProgressing", "Waiting for replicas")
	}

	if err := r.Status().Update(ctx, server); err != nil {
		return ctrl.Result{}, fmt.Errorf("updating status: %w", err)
	}

	r.Recorder.Event(server, corev1.EventTypeNormal, "Reconciled", "Successfully reconciled KeaDhcp6Server")
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *KeaDhcp6ServerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&keav1alpha1.KeaDhcp6Server{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.ServiceAccount{}).
		Owns(&monitoringv1.PodMonitor{}).
		Watches(&keav1alpha1.KeaStorkServer{}, handler.EnqueueRequestsFromMapFunc(enqueueDhcp6ServersInNamespace(mgr.GetClient()))).
		Complete(r)
}

// enqueueDhcp6ServersInNamespace returns a handler that enqueues all KeaDhcp6Servers
// in the same namespace when a KeaStorkServer is created, updated, or deleted.
func enqueueDhcp6ServersInNamespace(c client.Reader) handler.MapFunc {
	return func(ctx context.Context, obj client.Object) []ctrl.Request {
		serverList := &keav1alpha1.KeaDhcp6ServerList{}
		if err := c.List(ctx, serverList, client.InNamespace(obj.GetNamespace())); err != nil {
			return nil
		}
		var requests []ctrl.Request
		for _, s := range serverList.Items {
			if s.Spec.Stork != nil && s.Spec.Stork.Enabled {
				requests = append(requests, ctrl.Request{
					NamespacedName: types.NamespacedName{Name: s.Name, Namespace: s.Namespace},
				})
			}
		}
		return requests
	}
}
