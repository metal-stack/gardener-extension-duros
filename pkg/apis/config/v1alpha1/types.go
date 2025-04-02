package v1alpha1

import (
	healthcheckconfigv1alpha1 "github.com/gardener/gardener/extensions/pkg/apis/config/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ControllerConfiguration configuration resource
type ControllerConfiguration struct {
	metav1.TypeMeta `json:",inline"`

	// PartitionConfig is a map of a partition id to the duros partition configuration
	PartitionConfig map[string]DurosPartitionConfiguration `json:"partitionConfig"`

	// HealthCheckConfig is the config for the health check controller
	// +optional
	HealthCheckConfig *healthcheckconfigv1alpha1.HealthCheckConfig `json:"healthCheckConfig,omitempty"`
}

// DurosPartitionConfiguration is the configuration for duros for a particular partition
type DurosPartitionConfiguration struct {
	// Endpoints is the list of endpoints for the storage data plane and control plane communication
	Endpoints []string `json:"endpoints"`
	// AdminKey is the key used for generating storage credentials
	AdminKey string `json:"adminKey"`
	// AdminToken is the token used by the duros-controller to authenticate against the duros API
	AdminToken string `json:"adminToken"`

	// APIEndpoint is the endpoint used for control plane network communication.
	APIEndpoint string `json:"apiEndpoint"`
	// APICA is the ca of the client cert to access the api endpoint
	APICA string `json:"apiCA,omitempty"`
	// APICert is the cert of the client cert to access the api endpoint
	APICert string `json:"apiCert,omitempty"`
	// APIKey is the key of the client cert to access the api endpoint
	APIKey string `json:"apiKey,omitempty"`
}
