package discovery

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
)

// AgentEndpoint represents a discovered Kea Control Agent service endpoint.
type AgentEndpoint struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	URL       string `json:"url"`
	Port      int32  `json:"port"`
}

// ServiceDiscovery discovers Kea Control Agent endpoints in the cluster.
type ServiceDiscovery struct {
	client dynamic.Interface
}

// NewServiceDiscovery creates a new ServiceDiscovery using in-cluster configuration.
func NewServiceDiscovery() (*ServiceDiscovery, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to get in-cluster config: %w", err)
	}

	dynClient, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create dynamic client: %w", err)
	}

	return &ServiceDiscovery{
		client: dynClient,
	}, nil
}

// DiscoverAgents lists all KeaControlAgent custom resources and returns their
// corresponding service endpoints.
func (sd *ServiceDiscovery) DiscoverAgents(ctx context.Context) ([]AgentEndpoint, error) {
	gvr := schema.GroupVersionResource{
		Group:    "kea.openshift.io",
		Version:  "v1alpha1",
		Resource: "keacontrolagents",
	}

	list, err := sd.client.Resource(gvr).Namespace("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list KeaControlAgent resources: %w", err)
	}

	var agents []AgentEndpoint
	for _, item := range list.Items {
		name := item.GetName()
		namespace := item.GetNamespace()

		var port int32 = 8000
		spec, ok := item.Object["spec"].(map[string]interface{})
		if ok {
			if httpPort, exists := spec["http-port"]; exists {
				switch v := httpPort.(type) {
				case int64:
					port = int32(v)
				case float64:
					port = int32(v)
				}
			}
		}

		serviceName := fmt.Sprintf("%s-ctrl-agent", name)
		url := fmt.Sprintf("http://%s.%s.svc.cluster.local:%d/", serviceName, namespace, port)

		agents = append(agents, AgentEndpoint{
			Name:      name,
			Namespace: namespace,
			URL:       url,
			Port:      port,
		})
	}

	return agents, nil
}
