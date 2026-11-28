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

package resources

import (
	"fmt"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Default per-component images for Kea deployments.
const (
	DefaultKeaDhcp4Image    = "docker.cloudsmith.io/isc/docker/kea-dhcp4:3.0.2"
	DefaultKeaDhcp6Image    = "docker.cloudsmith.io/isc/docker/kea-dhcp6:3.0.2"
	DefaultKeaDhcpDdnsImage = "docker.cloudsmith.io/isc/docker/kea-dhcp-ddns:3.0.2"
)

// Volume/mount constants for Kea deployments.
const (
	ConfigVolumeName     = "kea-config"
	ConfigMountPath      = "/etc/kea"
	RunVolumeName        = "kea-run"
	RunMountPath         = "/var/run/kea"
	LeaseVolumeName      = "kea-leases"
	LeaseMountPath       = "/var/lib/kea"
	TLSVolumeName        = "kea-tls"
	TLSMountPath         = "/etc/kea/tls"
	ConfigHashAnnotation = "kea.openshift.io/config-hash"
	// CredentialsRevisionAnnotation carries a token derived from the names,
	// UIDs and resourceVersions of the credential Secrets a pod references
	// (never their data), so updating one of them rolls the pods.
	CredentialsRevisionAnnotation = "kea.openshift.io/credentials-revision"
	// AppliedCredentialsRevisionAnnotation sits on the Deployment or
	// StatefulSet object (not its pod template) and records the credentials
	// revision its pods run. It lets the operator adopt running pods after an
	// upgrade, and hold back a rollout, without changing the pod template.
	AppliedCredentialsRevisionAnnotation = "kea.openshift.io/applied-credentials-revision"
	// ApproveCredentialRolloutAnnotation goes on a KeaDhcp4Server or
	// KeaDhcp6Server. Setting it to the revision a CredentialsRolloutPending
	// condition names restarts the pods with that revision even though their
	// memfile leases live in pod-local storage.
	ApproveCredentialRolloutAnnotation = "kea.openshift.io/approve-credential-rollout"
	// TemplateRevisionAnnotation sits on the Deployment or StatefulSet object
	// and records which TemplateRevision rendered its pod template.
	TemplateRevisionAnnotation = "kea.openshift.io/template-revision"
	// AppliedGenerationAnnotation sits on the Deployment or StatefulSet object
	// and records the CR generation its pod template was last rendered from.
	AppliedGenerationAnnotation = "kea.openshift.io/applied-generation"
	// ApproveRestartAnnotation goes on a KeaDhcp4Server or KeaDhcp6Server.
	// Setting it to the revision a RestartPending condition names restarts
	// pods that keep memfile leases in pod-local storage with the current
	// operator's pod template.
	ApproveRestartAnnotation = "kea.openshift.io/approve-restart"
	// ConfigRevisionLabel marks a staged per-ordinal ConfigMap or Secret with
	// the configuration revision it belongs to.
	ConfigRevisionLabel = "kea.openshift.io/config-revision"
	// ConfigRevisionAnnotation and PreviousConfigRevisionAnnotation sit on an
	// HA StatefulSet and record the revision its template references and the
	// one before it, which is kept for rollback.
	ConfigRevisionAnnotation         = "kea.openshift.io/config-revision"
	PreviousConfigRevisionAnnotation = "kea.openshift.io/previous-config-revision"
	// OperatorAnnotationPrefix is the prefix of the annotations the operator
	// owns on the objects it manages.
	OperatorAnnotationPrefix = "kea.openshift.io/"
)

// defaultComponentImage maps a component name to its default container image.
var defaultComponentImage = map[string]string{
	ComponentDhcp4: DefaultKeaDhcp4Image,
	ComponentDhcp6: DefaultKeaDhcp6Image,
	// No Control Agent entry: ISC publishes no Kea Control Agent image, so a
	// KeaControlAgent must name one (the CRD and the controller enforce it).
	ComponentDdns: DefaultKeaDhcpDdnsImage,
}

// defaultComponentCommand maps a component name to the absolute path of the
// Kea binary inside the ISC container images. Using absolute paths allows
// the stork-agent sidecar to detect Kea daemons via /proc without needing
// to resolve the process's current working directory (which requires SYS_PTRACE).
var defaultComponentCommand = map[string]string{
	ComponentDhcp4:     "/usr/sbin/kea-dhcp4",
	ComponentDhcp6:     "/usr/sbin/kea-dhcp6",
	ComponentCtrlAgent: "/usr/sbin/kea-ctrl-agent",
	ComponentDdns:      "/usr/sbin/kea-dhcp-ddns",
}

// DefaultImageForComponent returns the default container image for the given
// Kea component. If the component is not recognized, it returns an empty string.
func DefaultImageForComponent(component string) string {
	return defaultComponentImage[component]
}

// DefaultCommandForComponent returns the absolute path to the Kea binary for
// the given component. If the component is not recognized, it returns an empty string.
func DefaultCommandForComponent(component string) string {
	return defaultComponentCommand[component]
}

// DeploymentParams holds all parameters needed to build a Kea component
// Deployment.
type DeploymentParams struct {
	Namespace          string
	CRName             string
	Component          string
	Command            string // e.g. kea-dhcp4, kea-dhcp6, kea-ctrl-agent, kea-dhcp-ddns
	ConfigFileName     string
	ConfigMapName      string
	Image              string
	ImagePullPolicy    corev1.PullPolicy
	Replicas           *int32
	Resources          corev1.ResourceRequirements
	HostNetwork        bool
	NodeSelector       map[string]string
	Tolerations        []corev1.Toleration
	Affinity           *corev1.Affinity
	TLSSecretName      string
	ServiceAccountName string
	ConfigHash         string
	ImagePullSecrets   []corev1.LocalObjectReference
	PodAnnotations     map[string]string
	// NADInterface is the network-attachment-definition interface name (e.g., "net1").
	// When set, the container command assigns an IP on this interface before starting Kea.
	NADInterface string
	// NADSubnet is the subnet in CIDR notation for the NAD interface (e.g., "10.200.0.0/24").
	// The pod IP will be NADSubnet base + 2 (e.g., 10.200.0.2).
	NADSubnet string
	// StorkAgent holds resolved stork-agent sidecar params. Nil means no sidecar.
	StorkAgent *StorkSidecarParams
	// ConfigSecretName, when set, names an operator-owned Secret holding the
	// configuration with its credentials already rendered in. It is mounted
	// instead of ConfigMapName, so nothing is substituted at startup.
	ConfigSecretName string
	// LeaseClaimName, when set, mounts this PersistentVolumeClaim for the
	// memfile lease file instead of an emptyDir, and switches the Deployment
	// to the Recreate strategy so two pods never write the file at once.
	LeaseClaimName string
}

// BuildDeployment constructs a Kubernetes Deployment for a Kea component based
// on the provided parameters.
func BuildDeployment(p DeploymentParams) *appsv1.Deployment {
	labels := CommonLabels(p.CRName, p.Component)

	image := p.Image
	if image == "" {
		image = DefaultImageForComponent(p.Component)
	}

	// Volumes: config (a ConfigMap, or the configuration Secret when
	// credentials are rendered in), run (emptyDir), leases (emptyDir).
	configSource := corev1.VolumeSource{
		ConfigMap: &corev1.ConfigMapVolumeSource{
			LocalObjectReference: corev1.LocalObjectReference{Name: p.ConfigMapName},
		},
	}
	if p.ConfigSecretName != "" {
		mode := ConfigSecretMode
		configSource = corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{SecretName: p.ConfigSecretName, DefaultMode: &mode},
		}
	}

	volumes := []corev1.Volume{
		{Name: ConfigVolumeName, VolumeSource: configSource},
		{
			Name: RunVolumeName,
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		},
		{Name: LeaseVolumeName, VolumeSource: leaseVolumeSource(p.LeaseClaimName)},
	}

	// Volume mounts for the Kea container.
	volumeMounts := []corev1.VolumeMount{
		{
			Name:      ConfigVolumeName,
			MountPath: ConfigMountPath,
			ReadOnly:  true,
		},
		{
			Name:      RunVolumeName,
			MountPath: RunMountPath,
		},
		{
			Name:      LeaseVolumeName,
			MountPath: LeaseMountPath,
		},
	}

	// Optional TLS secret volume.
	if p.TLSSecretName != "" {
		volumes = append(volumes, corev1.Volume{
			Name: TLSVolumeName,
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: p.TLSSecretName,
				},
			},
		})
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      TLSVolumeName,
			MountPath: TLSMountPath,
			ReadOnly:  true,
		})
	}

	// Security context: drop ALL, add NET_RAW and NET_BIND_SERVICE.
	addCaps := []corev1.Capability{
		"NET_RAW",
		"NET_BIND_SERVICE",
	}
	if p.NADInterface != "" {
		addCaps = append(addCaps, "NET_ADMIN")
	}
	securityContext := &corev1.SecurityContext{
		Capabilities: &corev1.Capabilities{
			Drop: []corev1.Capability{capabilityAll},
			Add:  addCaps,
		},
	}

	// Pod template annotations: merge user-supplied annotations with config hash.
	podAnnotations := make(map[string]string, len(p.PodAnnotations)+1)
	for k, v := range p.PodAnnotations {
		podAnnotations[k] = v
	}
	podAnnotations[ConfigHashAnnotation] = p.ConfigHash

	mainCmd := []string{p.Command, "-c", ConfigMountPath + "/" + p.ConfigFileName}
	if p.NADInterface != "" && p.NADSubnet != "" &&
		ValidateNADShellInputs(p.NADInterface, p.NADSubnet, p.Command) == nil {
		// Assign a static IP on the NAD interface before starting Kea.
		// For a single-replica Deployment the IP is subnet base + 2.
		mainCmd = []string{"sh", "-c", fmt.Sprintf(
			`IFS='./' read a b c d prefix <<EOF
%s
EOF
ip_last=$((d + 2)); `+
				`ip addr add ${a}.${b}.${c}.${ip_last}/${prefix} dev %s 2>/dev/null || true; `+
				`echo "Assigned ${a}.${b}.${c}.${ip_last}/${prefix} to %s"; `+
				`exec %s -c %s/%s`,
			p.NADSubnet, p.NADInterface, p.NADInterface,
			p.Command, ConfigMountPath, p.ConfigFileName,
		)}
	}

	// Kea 3.0 rejects a world-writable control-socket directory, and a fresh
	// emptyDir is 0777. The StatefulSet path fixes this in its init container;
	// the Deployment has none, so tighten the permissions before exec'ing Kea.
	mainCmd = withRunDirPermissions(mainCmd)

	container := corev1.Container{
		Name:            p.Component,
		Image:           image,
		ImagePullPolicy: p.ImagePullPolicy,
		Command:         mainCmd,
		Resources:       p.Resources,
		VolumeMounts:    volumeMounts,
		SecurityContext: securityContext,
	}

	containers := []corev1.Container{container}

	// Stork agent sidecar: shares PID namespace and config volume.
	var shareProcessNamespace *bool
	if p.StorkAgent != nil {
		storkContainer := buildStorkAgentContainer(p.StorkAgent, ConfigVolumeName, addCaps)
		containers = append(containers, storkContainer)
		t := true
		shareProcessNamespace = &t
	}

	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      DeploymentName(p.CRName, p.Component),
			Namespace: p.Namespace,
			Labels:    labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: p.Replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      podTemplateLabels(labels, p.StorkAgent),
					Annotations: podAnnotations,
				},
				Spec: corev1.PodSpec{
					ServiceAccountName:    p.ServiceAccountName,
					HostNetwork:           p.HostNetwork,
					ShareProcessNamespace: shareProcessNamespace,
					Containers:            containers,
					Volumes:               volumes,
					NodeSelector:          p.NodeSelector,
					Tolerations:           p.Tolerations,
					Affinity:              p.Affinity,
					ImagePullSecrets:      p.ImagePullSecrets,
				},
			},
		},
	}

	if p.LeaseClaimName != "" {
		deploy.Spec.Strategy = appsv1.DeploymentStrategy{Type: appsv1.RecreateDeploymentStrategyType}
	}
	return deploy
}

// leaseVolumeSource returns the lease volume: the claim when one is named,
// otherwise an emptyDir.
func leaseVolumeSource(claimName string) corev1.VolumeSource {
	if claimName == "" {
		return corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}
	}
	return corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: claimName}}
}

// withRunDirPermissions prefixes the container command with a chmod of the
// control-socket directory. An existing "sh -c" script is prefixed in place;
// any other command is wrapped in "sh -c" and exec'd so the Kea binary
// replaces the shell (stork-agent detects Kea by its absolute binary path).
func withRunDirPermissions(cmd []string) []string {
	chmod := fmt.Sprintf("chmod 750 %s; ", RunMountPath)
	if len(cmd) == 3 && cmd[0] == "sh" && cmd[1] == "-c" {
		return []string{"sh", "-c", chmod + cmd[2]}
	}
	return []string{"sh", "-c", chmod + "exec " + strings.Join(cmd, " ")}
}
