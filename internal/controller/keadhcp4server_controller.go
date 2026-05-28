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
	"net"
	"net/url"

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

	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"

	keav1alpha1 "github.com/openshift/ocp-kea-dhcp/api/v1alpha1"
	"github.com/openshift/ocp-kea-dhcp/internal/kea"
	"github.com/openshift/ocp-kea-dhcp/internal/resources"
)

// KeaDhcp4ServerReconciler reconciles a KeaDhcp4Server object.
type KeaDhcp4ServerReconciler struct {
	client.Client
	Scheme      *runtime.Scheme
	Recorder    record.EventRecorder
	IsOpenShift bool
}

// +kubebuilder:rbac:groups=kea.openshift.io,resources=keadhcp4servers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kea.openshift.io,resources=keadhcp4servers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=kea.openshift.io,resources=keadhcp4servers/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=serviceaccounts,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=monitoring.coreos.com,resources=podmonitors,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=route.openshift.io,resources=routes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kea.openshift.io,resources=keastorkservers,verbs=get;list;watch

// Reconcile reconciles the KeaDhcp4Server custom resource.
func (r *KeaDhcp4ServerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// 1. Fetch the CR.
	server := &keav1alpha1.KeaDhcp4Server{}
	if err := r.Get(ctx, req.NamespacedName, server); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("KeaDhcp4Server resource not found, ignoring")
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	logger.Info("reconciling KeaDhcp4Server", "name", server.Name)

	component := "dhcp4"
	labels := resources.CommonLabels(server.Name, component)
	haEnabled := server.Spec.HighAvailability != nil

	replicas := server.Spec.Replicas
	if replicas == nil {
		one := int32(1)
		replicas = &one
	}

	// 2. Auto-fill empty HA peer URLs from the StatefulSet naming convention.
	if haEnabled {
		haPort := int32(8000)
		if server.Spec.ControlSocket != nil && server.Spec.ControlSocket.SocketPort != nil {
			haPort = *server.Spec.ControlSocket.SocketPort
		}
		fillPeerURLs(server.Spec.HighAvailability, server.Namespace, server.Name, component, haPort)
	}

	// 3. Build env var references for database credentials.
	// Instead of resolving secrets and embedding them in the ConfigMap,
	// we use env var placeholders in the config and SecretKeyRef env vars
	// on the container. The placeholders are expanded at startup.
	renderer := kea.NewDhcp4ConfigRenderer(&server.Spec)
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

	// 4. Render and reconcile configuration.
	var configHash string

	if haEnabled {
		// HA mode: create per-ordinal ConfigMaps with different this-server-name.
		configHash = r.reconcileHAConfigs(ctx, renderer, server, component, labels, *replicas)
	} else {
		// Standard mode: single ConfigMap.
		configJSON, err := renderer.RenderJSON()
		if err != nil {
			setCondition(&server.Status.Conditions, keav1alpha1.ConditionTypeConfigValid, "False", "RenderError", err.Error())
			server.Status.Phase = keav1alpha1.PhaseError
			if statusErr := r.Status().Update(ctx, server); statusErr != nil {
				logger.Error(statusErr, "failed to update status after config render error")
			}
			r.Recorder.Eventf(server, corev1.EventTypeWarning, "ConfigRenderError", "Failed to render configuration: %v", err)
			return ctrl.Result{}, fmt.Errorf("rendering config: %w", err)
		}
		configHash = kea.ComputeHash(configJSON)

		cm := resources.BuildConfigMap(server.Namespace, server.Name, component, configJSON, labels)
		if err := reconcileResource(ctx, r.Client, r.Scheme, server, cm); err != nil {
			return ctrl.Result{}, fmt.Errorf("reconciling ConfigMap: %w", err)
		}
	}

	setCondition(&server.Status.Conditions, keav1alpha1.ConditionTypeConfigValid, "True", "ConfigRendered", "Configuration rendered successfully")

	// 4. Reconcile ServiceAccount.
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

	// 5. Extract common pod parameters.
	pp := r.resolveDhcp4PodParams(server)

	// Resolve Stork agent sidecar params (if enabled).
	storkAgent, err := r.resolveDhcp4StorkAgent(ctx, server)
	if err != nil {
		return ctrl.Result{}, err
	}

	// 6. Reconcile workload (StatefulSet for HA, Deployment otherwise).
	readyReplicas, err := r.reconcileDhcp4Workload(ctx, server, component, pp, replicas, configHash, secretEnvVars, storkAgent, haEnabled)
	if err != nil {
		return ctrl.Result{}, err
	}

	// 7. Reconcile Service if control socket is HTTP.
	if err := r.reconcileDhcp4ControlService(ctx, server, component); err != nil {
		return ctrl.Result{}, err
	}

	// 8. Reconcile PodMonitor and Stork metrics Service (when stork is enabled).
	if err := r.reconcileDhcp4StorkMonitoring(ctx, server, component, storkAgent); err != nil {
		return ctrl.Result{}, err
	}

	// 9. Update status.
	if err := r.updateDhcp4Status(ctx, server, component, configHash, readyReplicas, replicas); err != nil {
		return ctrl.Result{}, err
	}

	r.Recorder.Event(server, corev1.EventTypeNormal, "Reconciled", "Successfully reconciled KeaDhcp4Server")

	return ctrl.Result{}, nil
}

// dhcp4PodParams holds resolved pod-level parameters shared between the HA
// StatefulSet and standard Deployment workload builders.
type dhcp4PodParams struct {
	image          string
	hostNetwork    bool
	nodeSelector   map[string]string
	tolerations    []corev1.Toleration
	affinity       *corev1.Affinity
	podAnnotations map[string]string
	tlsSecretName  string
}

// resolveDhcp4PodParams extracts common pod parameters from the CR spec.
func (r *KeaDhcp4ServerReconciler) resolveDhcp4PodParams(server *keav1alpha1.KeaDhcp4Server) dhcp4PodParams {
	pp := dhcp4PodParams{
		image: server.Spec.Container.Image,
	}
	if server.Spec.HostNetwork != nil {
		pp.hostNetwork = *server.Spec.HostNetwork
	}
	if server.Spec.Placement != nil {
		pp.nodeSelector = server.Spec.Placement.NodeSelector
		pp.tolerations = server.Spec.Placement.Tolerations
		pp.affinity = server.Spec.Placement.Affinity
		pp.podAnnotations = server.Spec.Placement.PodAnnotations
	}
	if server.Spec.HighAvailability != nil && server.Spec.HighAvailability.TLS != nil {
		pp.tlsSecretName = server.Spec.HighAvailability.TLS.SecretRef.Name
	}
	return pp
}

// resolveDhcp4StorkAgent resolves the Stork agent sidecar params (if enabled),
// auto-discovering the Stork server when no explicit serverURL is configured.
func (r *KeaDhcp4ServerReconciler) resolveDhcp4StorkAgent(ctx context.Context, server *keav1alpha1.KeaDhcp4Server) (*resources.StorkSidecarParams, error) {
	if server.Spec.Stork == nil || !server.Spec.Stork.Enabled {
		return nil, nil
	}
	logger := log.FromContext(ctx)
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
	storkAgent, err := resources.ResolveStorkParams(storkCfg, serverToken)
	if err != nil {
		return nil, fmt.Errorf("resolving stork agent params: %w", err)
	}
	return storkAgent, nil
}

// reconcileDhcp4Workload reconciles the StatefulSet (HA) or Deployment (standard)
// workload and returns the number of ready replicas.
func (r *KeaDhcp4ServerReconciler) reconcileDhcp4Workload(
	ctx context.Context,
	server *keav1alpha1.KeaDhcp4Server,
	component string,
	pp dhcp4PodParams,
	replicas *int32,
	configHash string,
	secretEnvVars []corev1.EnvVar,
	storkAgent *resources.StorkSidecarParams,
	haEnabled bool,
) (int32, error) {
	if haEnabled {
		return r.reconcileDhcp4HAWorkload(ctx, server, component, pp, replicas, configHash, secretEnvVars, storkAgent)
	}
	return r.reconcileDhcp4Deployment(ctx, server, component, pp, replicas, configHash, secretEnvVars, storkAgent)
}

// reconcileDhcp4HAWorkload reconciles the headless Service, per-pod Services and
// StatefulSet for HA mode, returning the ready replica count.
func (r *KeaDhcp4ServerReconciler) reconcileDhcp4HAWorkload(
	ctx context.Context,
	server *keav1alpha1.KeaDhcp4Server,
	component string,
	pp dhcp4PodParams,
	replicas *int32,
	configHash string,
	secretEnvVars []corev1.EnvVar,
	storkAgent *resources.StorkSidecarParams,
) (int32, error) {
	var readyReplicas int32

	// HA mode: create headless Service + StatefulSet.
	haPort := int32(8000)
	if server.Spec.ControlSocket != nil && server.Spec.ControlSocket.SocketPort != nil {
		haPort = *server.Spec.ControlSocket.SocketPort
	}

	// Headless Service for StatefulSet DNS.
	hlSvc := resources.BuildHeadlessService(resources.ServiceParams{
		Namespace: server.Namespace,
		CRName:    server.Name,
		Component: component,
		Port:      haPort,
		Protocol:  corev1.ProtocolTCP,
	})
	if err := reconcileResource(ctx, r.Client, r.Scheme, server, hlSvc); err != nil {
		return readyReplicas, fmt.Errorf("reconciling headless Service: %w", err)
	}

	// Build per-ordinal ConfigMap name map.
	cmNames := make(map[int]string, int(*replicas))
	for i := int32(0); i < *replicas; i++ {
		cmNames[int(i)] = resources.HAConfigMapName(server.Name, component, int(i))
	}

	// Create per-pod Services with stable ClusterIPs for HA peer communication.
	// The headless Service resolves to ephemeral pod IPs, but per-pod Services
	// have stable ClusterIPs that survive pod restarts.
	svcParams := resources.ServiceParams{
		Namespace: server.Namespace,
		CRName:    server.Name,
		Component: component,
		Port:      haPort,
		Protocol:  corev1.ProtocolTCP,
	}
	for i := int32(0); i < *replicas; i++ {
		perPodSvc := resources.BuildPerPodService(svcParams, int(i))
		if err := reconcileResource(ctx, r.Client, r.Scheme, server, perPodSvc); err != nil {
			return readyReplicas, fmt.Errorf("reconciling per-pod Service for ordinal %d: %w", i, err)
		}
	}

	// Build a mapping from headless hostnames (in the config) to per-pod Service
	// hostnames (with stable ClusterIPs). The init container resolves the Service
	// DNS and replaces the headless hostname in the config.
	peerHostnameMap := make(map[string]string)
	for i, peer := range server.Spec.HighAvailability.Peers {
		u, err := url.Parse(peer.URL)
		if err != nil {
			continue
		}
		headlessHostname := u.Hostname()
		if err := resources.ValidateHostname(headlessHostname); err != nil {
			return readyReplicas, fmt.Errorf("HA peer %d URL: %w", i, err)
		}
		perPodSvcName := resources.PerPodServiceName(server.Name, component, i)
		perPodSvcHostname := fmt.Sprintf("%s.%s.svc.cluster.local", perPodSvcName, server.Namespace)
		peerHostnameMap[headlessHostname] = perPodSvcHostname
	}

	// Build peer address map from HA peer specs.
	peerAddresses := make(map[int]string)
	for i, peer := range server.Spec.HighAvailability.Peers {
		if peer.Address != "" {
			peerAddresses[i] = peer.Address
		}
	}

	sp := resources.StatefulSetParams{
		Namespace:          server.Namespace,
		CRName:             server.Name,
		Component:          component,
		Command:            resources.DefaultCommandForComponent(component),
		ConfigFileName:     resources.ConfigFileName(component),
		Image:              pp.image,
		ImagePullPolicy:    server.Spec.Container.ImagePullPolicy,
		Replicas:           replicas,
		Resources:          server.Spec.Container.Resources,
		HostNetwork:        pp.hostNetwork,
		NodeSelector:       pp.nodeSelector,
		Tolerations:        pp.tolerations,
		Affinity:           pp.affinity,
		TLSSecretName:      pp.tlsSecretName,
		ServiceAccountName: resources.ServiceAccountName(server.Name),
		ConfigHash:         configHash,
		ImagePullSecrets:   server.Spec.Container.ImagePullSecrets,
		PodAnnotations:     pp.podAnnotations,
		ConfigMapNames:     cmNames,
		PeerHostnameMap:    peerHostnameMap,
		NADInterface:       nadInterface(server),
		NADSubnet:          nadSubnet(server),
		PeerAddresses:      peerAddresses,
		StorkAgent:         storkAgent,
		SecretEnvVars:      secretEnvVars,
	}
	sts := resources.BuildStatefulSet(sp)
	if err := reconcileResource(ctx, r.Client, r.Scheme, server, sts); err != nil {
		return readyReplicas, fmt.Errorf("reconciling StatefulSet: %w", err)
	}

	// Get ready replicas from StatefulSet.
	currentSTS := &appsv1.StatefulSet{}
	stsKey := types.NamespacedName{
		Name:      resources.StatefulSetName(server.Name, component),
		Namespace: server.Namespace,
	}
	if err := r.Get(ctx, stsKey, currentSTS); err == nil {
		readyReplicas = currentSTS.Status.ReadyReplicas
	}
	return readyReplicas, nil
}

// reconcileDhcp4Deployment reconciles the standard (non-HA) Deployment workload,
// returning the ready replica count.
func (r *KeaDhcp4ServerReconciler) reconcileDhcp4Deployment(
	ctx context.Context,
	server *keav1alpha1.KeaDhcp4Server,
	component string,
	pp dhcp4PodParams,
	replicas *int32,
	configHash string,
	secretEnvVars []corev1.EnvVar,
	storkAgent *resources.StorkSidecarParams,
) (int32, error) {
	var readyReplicas int32

	// Standard mode: Deployment.
	dp := resources.DeploymentParams{
		Namespace:          server.Namespace,
		CRName:             server.Name,
		Component:          component,
		Command:            resources.DefaultCommandForComponent(component),
		ConfigFileName:     resources.ConfigFileName(component),
		ConfigMapName:      resources.ConfigMapName(server.Name, component),
		Image:              pp.image,
		ImagePullPolicy:    server.Spec.Container.ImagePullPolicy,
		Replicas:           replicas,
		Resources:          server.Spec.Container.Resources,
		HostNetwork:        pp.hostNetwork,
		NodeSelector:       pp.nodeSelector,
		Tolerations:        pp.tolerations,
		Affinity:           pp.affinity,
		TLSSecretName:      pp.tlsSecretName,
		ServiceAccountName: resources.ServiceAccountName(server.Name),
		ConfigHash:         configHash,
		ImagePullSecrets:   server.Spec.Container.ImagePullSecrets,
		PodAnnotations:     pp.podAnnotations,
		NADInterface:       nadInterface(server),
		NADSubnet:          nadSubnet(server),
		StorkAgent:         storkAgent,
		SecretEnvVars:      secretEnvVars,
	}
	deploy := resources.BuildDeployment(dp)
	if err := reconcileResource(ctx, r.Client, r.Scheme, server, deploy); err != nil {
		return readyReplicas, fmt.Errorf("reconciling Deployment: %w", err)
	}

	currentDeploy := &appsv1.Deployment{}
	deployKey := types.NamespacedName{
		Name:      resources.DeploymentName(server.Name, component),
		Namespace: server.Namespace,
	}
	if err := r.Get(ctx, deployKey, currentDeploy); err == nil {
		readyReplicas = currentDeploy.Status.ReadyReplicas
	}
	return readyReplicas, nil
}

// reconcileDhcp4ControlService reconciles the control-socket Service when the
// control socket uses HTTP.
func (r *KeaDhcp4ServerReconciler) reconcileDhcp4ControlService(ctx context.Context, server *keav1alpha1.KeaDhcp4Server, component string) error {
	if server.Spec.ControlSocket == nil || server.Spec.ControlSocket.SocketType != "http" {
		return nil
	}
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
		return fmt.Errorf("reconciling Service: %w", err)
	}
	return nil
}

// reconcileDhcp4StorkMonitoring reconciles the PodMonitor, Stork metrics Service
// and (on OpenShift) the metrics Route when the Stork agent is enabled.
func (r *KeaDhcp4ServerReconciler) reconcileDhcp4StorkMonitoring(ctx context.Context, server *keav1alpha1.KeaDhcp4Server, component string, storkAgent *resources.StorkSidecarParams) error {
	if storkAgent == nil {
		return nil
	}
	logger := log.FromContext(ctx)

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
		return fmt.Errorf("reconciling Stork metrics Service: %w", err)
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
	return nil
}

// updateDhcp4Status updates the CR status (replicas, hash, conditions, phase).
func (r *KeaDhcp4ServerReconciler) updateDhcp4Status(ctx context.Context, server *keav1alpha1.KeaDhcp4Server, component, configHash string, readyReplicas int32, replicas *int32) error {
	server.Status.ReadyReplicas = readyReplicas
	server.Status.ConfigHash = configHash
	server.Status.ConfigMapRef = resources.ConfigMapName(server.Name, component)
	server.Status.ObservedGeneration = server.Generation

	// Compute NAD addresses from subnet + replica count.
	server.Status.NADAddresses = computeNADAddresses(nadSubnet(server), int(*replicas))

	if readyReplicas > 0 && readyReplicas == *replicas {
		server.Status.Phase = keav1alpha1.PhaseRunning
		setCondition(&server.Status.Conditions, keav1alpha1.ConditionTypeReady, "True", "DeploymentReady", "All replicas are ready")
		setCondition(&server.Status.Conditions, keav1alpha1.ConditionTypeProgressing, "False", "DeploymentComplete", "Deployment is complete")
	} else {
		server.Status.Phase = keav1alpha1.PhaseProgressing
		setCondition(&server.Status.Conditions, keav1alpha1.ConditionTypeReady, "False", "DeploymentProgressing",
			fmt.Sprintf("%d/%d replicas ready", readyReplicas, *replicas))
		setCondition(&server.Status.Conditions, keav1alpha1.ConditionTypeProgressing, "True", "DeploymentProgressing", "Waiting for replicas")
	}

	if err := r.Status().Update(ctx, server); err != nil {
		return fmt.Errorf("updating status: %w", err)
	}
	return nil
}

// reconcileHAConfigs creates per-ordinal ConfigMaps for HA mode and returns
// a combined config hash. Each ConfigMap has the full Kea config but with a
// different this-server-name matching the HA peer at that ordinal.
func (r *KeaDhcp4ServerReconciler) reconcileHAConfigs(
	ctx context.Context,
	renderer *kea.Dhcp4ConfigRenderer,
	server *keav1alpha1.KeaDhcp4Server,
	component string,
	labels map[string]string,
	replicas int32,
) string {
	logger := log.FromContext(ctx)
	ha := server.Spec.HighAvailability
	var combinedHash string

	for i := int32(0); i < replicas; i++ {
		// Determine server name for this ordinal from the peers list.
		serverName := fmt.Sprintf("server%d", i+1)
		if int(i) < len(ha.Peers) {
			serverName = ha.Peers[i].Name
		}

		configJSON, err := renderer.RenderJSONWithServerName(serverName)
		if err != nil {
			logger.Error(err, "failed to render HA config", "ordinal", i)
			continue
		}

		combinedHash += kea.ComputeHash(configJSON)

		cmName := resources.HAConfigMapName(server.Name, component, int(i))
		cm := &corev1.ConfigMap{
			ObjectMeta: resources.ConfigMapMeta(cmName, server.Namespace, labels),
			Data: map[string]string{
				resources.ConfigFileName(component): string(configJSON),
			},
		}
		if err := reconcileResource(ctx, r.Client, r.Scheme, server, cm); err != nil {
			logger.Error(err, "failed to reconcile HA ConfigMap", "ordinal", i)
		}
	}

	return kea.ComputeHash([]byte(combinedHash))
}

// nadInterface returns the first non-standard network interface from the CR's
// interfaces-config. Standard interfaces like "eth0" and "*" are skipped since
// they don't require NAD IP assignment.
func nadInterface(server *keav1alpha1.KeaDhcp4Server) string {
	for _, iface := range server.Spec.InterfacesConfig.Interfaces {
		if iface != "*" && iface != "eth0" && iface != "lo" {
			return iface
		}
	}
	return ""
}

// nadSubnet returns the first subnet CIDR from the CR's subnet4 list.
// This is used to derive per-pod IP addresses on the NAD interface.
func nadSubnet(server *keav1alpha1.KeaDhcp4Server) string {
	if len(server.Spec.Subnet4) > 0 {
		return server.Spec.Subnet4[0].Subnet
	}
	return ""
}

// SetupWithManager sets up the controller with the Manager.
func (r *KeaDhcp4ServerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&keav1alpha1.KeaDhcp4Server{}).
		Owns(&appsv1.Deployment{}).
		Owns(&appsv1.StatefulSet{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.ServiceAccount{}).
		Owns(&monitoringv1.PodMonitor{}).
		Watches(&keav1alpha1.KeaStorkServer{}, handler.EnqueueRequestsFromMapFunc(
			enqueueStorkDependents(listStorkEnabledDhcp4, mgr.GetClient()))).
		Complete(r)
}

// computeNADAddresses derives per-pod NAD IP addresses from a subnet CIDR and
// replica count. The formula matches the StatefulSet init script:
// IP = subnet_base + ordinal + 2 (e.g., for 192.168.50.0/24: pod 0 = .2, pod 1 = .3).
// Returns nil if the subnet is empty or unparseable.
func computeNADAddresses(subnetCIDR string, replicas int) []string {
	if subnetCIDR == "" || replicas == 0 {
		return nil
	}
	ip, _, err := net.ParseCIDR(subnetCIDR)
	if err != nil {
		return nil
	}
	ip = ip.To4()
	if ip == nil {
		return nil
	}
	addrs := make([]string, replicas)
	for i := 0; i < replicas; i++ {
		addr := make(net.IP, 4)
		copy(addr, ip)
		addr[3] += byte(i + 2)
		addrs[i] = addr.String()
	}
	return addrs
}
