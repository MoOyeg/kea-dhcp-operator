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
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	keav1alpha1 "github.com/openshift/ocp-kea-dhcp/api/v1alpha1"
	"github.com/openshift/ocp-kea-dhcp/internal/resources"
)

// storkMachine is the part of a Stork machine record the operator uses.
type storkMachine struct {
	ID            int64  `json:"id"`
	Address       string `json:"address"`
	AgentPort     int64  `json:"agentPort"`
	Authorized    bool   `json:"authorized"`
	LastVisitedAt string `json:"lastVisitedAt"`
	Error         string `json:"error"`
}

// hasState reports whether the server has pulled the machine's state at least
// once and the last pull succeeded.
func (m storkMachine) hasState() bool {
	return m.Error == "" && m.LastVisitedAt != "" && !strings.HasPrefix(m.LastVisitedAt, "0001-")
}

// storkMachineAPI is a logged-in Stork session limited to the machine calls the
// operator needs.
type storkMachineAPI interface {
	ListMachines(ctx context.Context) ([]storkMachine, error)
	// RefreshMachineState makes the server pull the machine's state now,
	// like the Refresh button in the Stork UI.
	RefreshMachineState(ctx context.Context, id int64) error
	DeleteMachine(ctx context.Context, id int64) error
}

// storkMachineConnector opens a storkMachineAPI session.
type storkMachineConnector func(ctx context.Context, baseURL, username, password string) (storkMachineAPI, error)

type storkSession struct {
	client *http.Client
	base   string
}

// connectStork logs into the Stork REST API.
func connectStork(ctx context.Context, baseURL, username, password string) (storkMachineAPI, error) {
	c, err := storkLogin(ctx, baseURL, username, password)
	if err != nil {
		return nil, err
	}
	return &storkSession{client: c, base: baseURL}, nil
}

func (s *storkSession) ListMachines(ctx context.Context) ([]storkMachine, error) {
	body, err := storkRequest(ctx, s.client, http.MethodGet, s.base+"/api/machines?start=0&limit=1000", nil)
	if err != nil {
		return nil, fmt.Errorf("listing Stork machines: %w", err)
	}
	var resp struct {
		Items []storkMachine `json:"items"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("decoding Stork machines: %w", err)
	}
	return resp.Items, nil
}

func (s *storkSession) RefreshMachineState(ctx context.Context, id int64) error {
	_, err := storkRequest(ctx, s.client, http.MethodGet, fmt.Sprintf("%s/api/machines/%d/state", s.base, id), nil)
	return err
}

func (s *storkSession) DeleteMachine(ctx context.Context, id int64) error {
	_, err := storkRequest(ctx, s.client, http.MethodDelete, fmt.Sprintf("%s/api/machines/%d", s.base, id), nil)
	return err
}

// storkAgentPods lists the pods whose stork-agent reports to this server. They
// carry resources.LabelStorkServer, set by the KeaDhcp4Server/KeaDhcp6Server
// reconcilers when the agent uses an in-cluster KeaStorkServer.
func storkAgentPods(ctx context.Context, c client.Client, server *keav1alpha1.KeaStorkServer) ([]corev1.Pod, error) {
	pods := &corev1.PodList{}
	if err := c.List(ctx, pods, client.InNamespace(server.Namespace),
		client.MatchingLabels{resources.LabelStorkServer: server.Name}); err != nil {
		return nil, fmt.Errorf("listing stork-agent pods: %w", err)
	}
	return pods.Items, nil
}

// storkAgentAddresses returns the addresses a pod's agent may be registered
// under: its stable per-pod DNS name (StatefulSet pods) and its pod IP.
func storkAgentAddresses(pod *corev1.Pod) []string {
	var out []string
	if pod.Spec.Hostname != "" && pod.Spec.Subdomain != "" {
		out = append(out, fmt.Sprintf("%s.%s.%s.svc.cluster.local", pod.Spec.Hostname, pod.Spec.Subdomain, pod.Namespace))
	}
	if pod.Status.PodIP != "" {
		out = append(out, pod.Status.PodIP)
	}
	return out
}

// storkAgentRunning reports whether the pod's stork-agent container is running.
func storkAgentRunning(pod *corev1.Pod) bool {
	if pod.DeletionTimestamp != nil {
		return false
	}
	for _, cs := range pod.Status.ContainerStatuses {
		if cs.Name == resources.StorkAgentContainerName {
			return cs.State.Running != nil
		}
	}
	return false
}

// operatorAgentPod parses an address of the form
// "<sts>-<ordinal>.<sts>-hl.<namespace>.svc.cluster.local", the name this
// operator registers StatefulSet agents under, and returns the StatefulSet
// name and ordinal. ok is false for any other address, including pod IPs and
// agents the operator did not deploy.
func operatorAgentPod(address, namespace string) (sts string, ordinal int, ok bool) {
	parts := strings.Split(address, ".")
	if len(parts) != 6 || parts[2] != namespace || parts[3] != "svc" || parts[4] != "cluster" || parts[5] != "local" {
		return "", 0, false
	}
	pod, svc := parts[0], parts[1]
	i := strings.LastIndex(pod, "-")
	if i <= 0 {
		return "", 0, false
	}
	sts = pod[:i]
	n, err := strconv.Atoi(pod[i+1:])
	if err != nil || n < 0 || svc != sts+"-hl" {
		return "", 0, false
	}
	return sts, n, true
}

// agentOrdinalWanted reports whether the StatefulSet still wants a pod with
// the given ordinal, i.e. a missing pod is about to be recreated rather than
// scaled away.
func agentOrdinalWanted(ctx context.Context, c client.Client, namespace, sts string, ordinal int) (bool, error) {
	obj := &appsv1.StatefulSet{}
	if err := c.Get(ctx, types.NamespacedName{Name: sts, Namespace: namespace}, obj); err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	replicas := int32(1)
	if obj.Spec.Replicas != nil {
		replicas = *obj.Spec.Replicas
	}
	return obj.DeletionTimestamp == nil && int32(ordinal) < replicas, nil
}

// reconcileAgentMachines keeps the Stork server's machine list in step with the
// agent pods.
//
//   - A running agent whose machine has no state is refreshed. With server-token
//     registration the server's first state pull always fails: it runs while
//     "stork-agent register" still holds the port with a server that only
//     answers Ping, so GetState returns Unimplemented. Refreshing once the real
//     agent is up fixes that without waiting for the state puller.
//   - A machine registered under an operator agent name whose pod is gone for
//     good (scaled down, or its CR deleted) is removed. Dead machines slow the
//     state puller down, because it dials each one in turn.
//
// It returns pending=true while some running agent still has no machine state,
// so the caller rechecks soon.
func reconcileAgentMachines(ctx context.Context, c client.Client, server *keav1alpha1.KeaStorkServer, api storkMachineAPI) (pending bool, err error) {
	logger := log.FromContext(ctx)

	pods, err := storkAgentPods(ctx, c, server)
	if err != nil {
		return false, err
	}
	live := map[string]*corev1.Pod{}
	for i := range pods {
		for _, addr := range storkAgentAddresses(&pods[i]) {
			live[addr] = &pods[i]
		}
	}

	machines, err := api.ListMachines(ctx)
	if err != nil {
		return false, err
	}

	registered := map[string]bool{}
	for _, m := range machines {
		pod, ok := live[m.Address]
		if ok {
			registered[pod.Name] = true
			if !m.hasState() && m.Authorized && storkAgentRunning(pod) {
				pending = true
				if err := api.RefreshMachineState(ctx, m.ID); err != nil {
					logger.Info("Stork machine state refresh failed, will retry", "machine", m.ID, "address", m.Address, "reason", err.Error())
				} else {
					logger.Info("refreshed Stork machine state", "machine", m.ID, "address", m.Address)
				}
			}
			continue
		}

		sts, ordinal, ours := operatorAgentPod(m.Address, server.Namespace)
		if !ours {
			continue
		}
		wanted, err := agentOrdinalWanted(ctx, c, server.Namespace, sts, ordinal)
		if err != nil {
			return pending, err
		}
		if wanted {
			continue
		}
		if err := api.DeleteMachine(ctx, m.ID); err != nil {
			logger.Info("could not remove stale Stork machine", "machine", m.ID, "address", m.Address, "reason", err.Error())
			continue
		}
		logger.Info("removed stale Stork machine", "machine", m.ID, "address", m.Address)
	}

	// A running agent with no machine yet is still registering.
	for i := range pods {
		if storkAgentRunning(&pods[i]) && !registered[pods[i].Name] {
			pending = true
		}
	}
	return pending, nil
}

// storkServersForAgentPod maps an agent pod to the KeaStorkServer it reports
// to, so the server's machines are rechecked when its agents come and go.
func storkServersForAgentPod(_ context.Context, obj client.Object) []reconcile.Request {
	name := obj.GetLabels()[resources.LabelStorkServer]
	if name == "" {
		return nil
	}
	return []reconcile.Request{{NamespacedName: types.NamespacedName{Name: name, Namespace: obj.GetNamespace()}}}
}
