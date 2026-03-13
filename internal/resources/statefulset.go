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

package resources

import (
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// ActiveConfigVolumeName is the emptyDir volume where the init container
	// places the selected per-ordinal configuration file.
	ActiveConfigVolumeName = "kea-active-config"
	// ActiveConfigMountPath is where the active (per-pod) config is mounted.
	ActiveConfigMountPath = "/etc/kea/active"
	// ConfigsBaseMountPath is the base path under which per-ordinal configs are mounted.
	ConfigsBaseMountPath = "/etc/kea/configs"
	// DefaultInitContainerImage is the lightweight image used to select the per-pod config.
	DefaultInitContainerImage = "docker.io/busybox:1.37"
)

// StatefulSetParams holds all parameters needed to build a Kea HA StatefulSet.
type StatefulSetParams struct {
	Namespace          string
	CRName             string
	Component          string
	Command            string
	ConfigFileName     string
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
	// ConfigMapNames maps ordinal index to ConfigMap name (e.g., 0 → "dhcp4-ha-dhcp4-0").
	ConfigMapNames map[int]string
	// PeerHostnameMap maps hostnames found in the config (headless DNS names) to
	// per-pod Service DNS names whose ClusterIPs are stable across pod restarts.
	// The init container resolves each Service DNS name and replaces the corresponding
	// headless hostname in the config with the resolved ClusterIP.
	PeerHostnameMap map[string]string
	// NADInterface is the network-attachment-definition interface name (e.g., "net1").
	// When set, the init container assigns a unique IP per pod based on ordinal.
	NADInterface string
	// NADSubnet is the subnet in CIDR notation for the NAD interface (e.g., "10.200.0.0/24").
	// The pod IP will be NADSubnet base + ordinal + 2 (e.g., 10.200.0.2 for ordinal 0).
	NADSubnet string
	// PeerAddresses maps ordinal index to a static IP address for the NAD interface.
	// When set, these addresses are used instead of the auto-calculated ones.
	PeerAddresses map[int]string
	// InitContainerImage overrides the default init container image.
	InitContainerImage string
	// StorkAgent holds resolved stork-agent sidecar params. Nil means no sidecar.
	StorkAgent *StorkSidecarParams
	// SecretEnvVars are environment variables sourced from Secrets (via ValueFrom.SecretKeyRef).
	// The Kea config uses env var placeholders that are expanded at container startup.
	SecretEnvVars []corev1.EnvVar
}

// StatefulSetName returns the conventional StatefulSet name.
func StatefulSetName(crName, component string) string {
	return fmt.Sprintf("%s-%s", crName, component)
}

// HeadlessServiceName returns the headless Service name for StatefulSet DNS.
func HeadlessServiceName(crName, component string) string {
	return fmt.Sprintf("%s-%s-hl", crName, component)
}

// HAConfigMapName returns the per-ordinal ConfigMap name.
func HAConfigMapName(crName, component string, ordinal int) string {
	return fmt.Sprintf("%s-%s-%d", crName, component, ordinal)
}

// BuildStatefulSet constructs a StatefulSet for HA Kea deployments.
// Each pod gets its own config via an init container that selects the
// correct per-ordinal ConfigMap based on the pod's hostname suffix.
func BuildStatefulSet(p StatefulSetParams) *appsv1.StatefulSet {
	labels := CommonLabels(p.CRName, p.Component)

	image := p.Image
	if image == "" {
		image = DefaultImageForComponent(p.Component)
	}

	replicas := int32(2)
	if p.Replicas != nil {
		replicas = *p.Replicas
	}

	// Build volumes: per-ordinal config volumes + active config (emptyDir) + run + leases
	volumes := []corev1.Volume{
		{
			Name: ActiveConfigVolumeName,
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		},
		{
			Name: RunVolumeName,
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		},
		{
			Name: LeaseVolumeName,
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		},
	}

	// Init container volume mounts: one per ordinal + the active config target + run dir.
	initVolumeMounts := []corev1.VolumeMount{
		{
			Name:      ActiveConfigVolumeName,
			MountPath: ActiveConfigMountPath,
		},
		{
			Name:      RunVolumeName,
			MountPath: RunMountPath,
		},
	}

	// Add a volume and init mount per ordinal.
	for i := int32(0); i < replicas; i++ {
		volName := fmt.Sprintf("kea-config-%d", i)
		cmName, ok := p.ConfigMapNames[int(i)]
		if !ok {
			cmName = HAConfigMapName(p.CRName, p.Component, int(i))
		}

		volumes = append(volumes, corev1.Volume{
			Name: volName,
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: cmName,
					},
				},
			},
		})

		initVolumeMounts = append(initVolumeMounts, corev1.VolumeMount{
			Name:      volName,
			MountPath: fmt.Sprintf("%s/%d", ConfigsBaseMountPath, i),
			ReadOnly:  true,
		})
	}

	// Main container volume mounts.
	mainVolumeMounts := []corev1.VolumeMount{
		{
			Name:      ActiveConfigVolumeName,
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
		mainVolumeMounts = append(mainVolumeMounts, corev1.VolumeMount{
			Name:      TLSVolumeName,
			MountPath: TLSMountPath,
			ReadOnly:  true,
		})
	}

	// Security context for the main container.
	addCaps := []corev1.Capability{
		"NET_RAW",
		"NET_BIND_SERVICE",
	}
	if p.NADInterface != "" {
		addCaps = append(addCaps, "NET_ADMIN")
	}
	securityContext := &corev1.SecurityContext{
		Capabilities: &corev1.Capabilities{
			Drop: []corev1.Capability{"ALL"},
			Add:  addCaps,
		},
	}

	// Pod annotations: merge user annotations with config hash.
	podAnnotations := make(map[string]string, len(p.PodAnnotations)+1)
	for k, v := range p.PodAnnotations {
		podAnnotations[k] = v
	}
	podAnnotations[ConfigHashAnnotation] = p.ConfigHash

	// Init container: selects the correct config based on hostname ordinal,
	// then resolves any HA peer hostnames to IPs (Kea can't resolve DNS in peer URLs).
	configDst := fmt.Sprintf("%s/%s", ActiveConfigMountPath, p.ConfigFileName)
	initScript := fmt.Sprintf(
		`chmod 750 %s; ordinal=${HOSTNAME##*-}; cp %s/${ordinal}/%s %s; echo "Selected config for ordinal ${ordinal}"`,
		RunMountPath, ConfigsBaseMountPath, p.ConfigFileName, configDst,
	)
	// Append DNS resolution commands for each peer.
	// PeerHostnameMap maps config hostnames (headless DNS) → per-pod Service DNS names.
	// For the current pod ("this server"), use the pod's own IP to avoid EADDRNOTAVAIL
	// when Kea tries to bind the dedicated listener to the peer URL address.
	// For other peers, resolve the per-pod Service DNS (stable ClusterIP).
	stsName := StatefulSetName(p.CRName, p.Component)
	for configHostname, serviceHostname := range p.PeerHostnameMap {
		initScript += fmt.Sprintf(`
if echo "%s" | grep -q "^%s-${ordinal}\."; then
  pod_ip=$(hostname -i | awk '{print $1}')
  sed -i "s|%s|$pod_ip|g" %s
  echo "This server: %s -> $pod_ip (pod IP)"
else
  resolved=""
  for i in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15; do
    resolved=$(nslookup %s 2>/dev/null | tail -n +3 | awk '/^Address/{print $NF}' | grep -v ':' | head -1)
    if [ -n "$resolved" ]; then break; fi
    echo "Waiting for DNS resolution of %s (attempt $i)..."
    sleep 2
  done
  if [ -n "$resolved" ]; then
    sed -i "s|%s|$resolved|g" %s
    echo "Resolved %s -> $resolved (via %s)"
  else
    echo "WARNING: Could not resolve %s, keeping hostname"
  fi
fi`, configHostname, stsName, configHostname, configDst, configHostname,
			serviceHostname, serviceHostname, configHostname, configDst, configHostname, serviceHostname, serviceHostname)
	}

	// Expand secret env var placeholders in the config.
	for _, ev := range p.SecretEnvVars {
		initScript += fmt.Sprintf(`; sed -i "s|\$%s|${%s}|g" %s`, ev.Name, ev.Name, configDst)
	}

	// Dump final config for debugging.
	initScript += fmt.Sprintf("\necho '=== Final config ==='; cat %s", configDst)

	initImage := DefaultInitContainerImage
	if p.InitContainerImage != "" {
		initImage = p.InitContainerImage
	}

	initContainer := corev1.Container{
		Name:         "config-selector",
		Image:        initImage,
		Command:      []string{"sh", "-c", initScript},
		Env:          p.SecretEnvVars,
		VolumeMounts: initVolumeMounts,
		SecurityContext: &corev1.SecurityContext{
			Capabilities: &corev1.Capabilities{
				Drop: []corev1.Capability{"ALL"},
			},
		},
	}

	// Main Kea container.
	mainCmd := []string{p.Command, "-c", ConfigMountPath + "/" + p.ConfigFileName}
	if p.NADInterface != "" && p.NADSubnet != "" &&
		ValidateNADShellInputs(p.NADInterface, p.NADSubnet, p.Command) == nil {
		if len(p.PeerAddresses) > 0 {
			// Use static peer addresses specified in the CR.
			// Build a case statement mapping ordinal to address.
			caseScript := `ordinal=${HOSTNAME##*-}; prefix=$(echo ` + p.NADSubnet + ` | cut -d/ -f2); case $ordinal in `
			for ord, addr := range p.PeerAddresses {
				caseScript += fmt.Sprintf(`%d) ip_addr="%s/${prefix}" ;; `, ord, addr)
			}
			caseScript += fmt.Sprintf(
				`*) base=$(echo %s | cut -d/ -f1); IFS=. read -r a b c d <<EOF
$base
EOF
ip_last=$((d + ordinal + 2)); ip_addr="${a}.${b}.${c}.${ip_last}/${prefix}" ;; esac; `+
					`ip addr add $ip_addr dev %s 2>/dev/null || true; `+
					`echo "Assigned $ip_addr to %s"; `+
					`exec %s -c %s/%s`,
				p.NADSubnet, p.NADInterface, p.NADInterface,
				p.Command, ConfigMountPath, p.ConfigFileName,
			)
			mainCmd = []string{"sh", "-c", caseScript}
		} else {
			// Auto-assign IP: subnet base + ordinal + 2 (e.g., 10.200.0.2 for ordinal 0).
			mainCmd = []string{"sh", "-c", fmt.Sprintf(
				`ordinal=${HOSTNAME##*-}; `+
					`base=$(echo %s | cut -d/ -f1); `+
					`prefix=$(echo %s | cut -d/ -f2); `+
					`IFS=. read -r a b c d <<EOF
$base
EOF
ip_last=$((d + ordinal + 2)); `+
					`ip addr add ${a}.${b}.${c}.${ip_last}/${prefix} dev %s 2>/dev/null || true; `+
					`echo "Assigned ${a}.${b}.${c}.${ip_last}/${prefix} to %s"; `+
					`exec %s -c %s/%s`,
				p.NADSubnet, p.NADSubnet, p.NADInterface, p.NADInterface,
				p.Command, ConfigMountPath, p.ConfigFileName,
			)}
		}
	}
	mainContainer := corev1.Container{
		Name:            p.Component,
		Image:           image,
		ImagePullPolicy: p.ImagePullPolicy,
		Command:         mainCmd,
		Resources:       p.Resources,
		VolumeMounts:    mainVolumeMounts,
		SecurityContext: securityContext,
	}

	containers := []corev1.Container{mainContainer}

	// Stork agent sidecar: shares PID namespace and reads active config.
	var shareProcessNamespace *bool
	if p.StorkAgent != nil {
		storkContainer := buildStorkAgentContainer(p.StorkAgent, ActiveConfigVolumeName)
		containers = append(containers, storkContainer)
		t := true
		shareProcessNamespace = &t
	}

	// Inject pod anti-affinity so HA peers land on different nodes.
	// Uses PreferredDuringScheduling so it doesn't block scheduling
	// when fewer nodes than replicas are available.
	haAntiAffinity := &corev1.Affinity{
		PodAntiAffinity: &corev1.PodAntiAffinity{
			PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{
				{
					Weight: 100,
					PodAffinityTerm: corev1.PodAffinityTerm{
						LabelSelector: &metav1.LabelSelector{
							MatchLabels: labels,
						},
						TopologyKey: "kubernetes.io/hostname",
					},
				},
			},
		},
	}
	if p.Affinity != nil {
		// Merge: preserve user-provided node affinity and pod affinity,
		// but always set the HA pod anti-affinity.
		haAntiAffinity.NodeAffinity = p.Affinity.NodeAffinity
		haAntiAffinity.PodAffinity = p.Affinity.PodAffinity
	}
	p.Affinity = haAntiAffinity

	headlessSvcName := HeadlessServiceName(p.CRName, p.Component)

	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      StatefulSetName(p.CRName, p.Component),
			Namespace: p.Namespace,
			Labels:    labels,
		},
		Spec: appsv1.StatefulSetSpec{
			ServiceName:         headlessSvcName,
			PodManagementPolicy: appsv1.ParallelPodManagement,
			Replicas:            &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      labels,
					Annotations: podAnnotations,
				},
				Spec: corev1.PodSpec{
					ServiceAccountName:    p.ServiceAccountName,
					HostNetwork:           p.HostNetwork,
					ShareProcessNamespace: shareProcessNamespace,
					InitContainers:        []corev1.Container{initContainer},
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

	return sts
}
