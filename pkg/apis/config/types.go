package config

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	healthcheckconfig "github.com/gardener/gardener/extensions/pkg/apis/config"
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ControllerConfiguration defines the configuration for the duros-provider controller.
type ControllerConfiguration struct {
	metav1.TypeMeta

	// PartitionConfig is a map of a partition id to the duros partition configuration
	PartitionConfig map[string]DurosPartitionConfiguration

	// HealthCheckConfig is the config for the health check controller
	HealthCheckConfig *healthcheckconfig.HealthCheckConfig
}

// DurosPartitionConfiguration is the configuration for duros for a particular partition
type DurosPartitionConfiguration struct {
	// Endpoints is the list of endpoints for the storage data plane and control plane communication
	Endpoints []string
	// AdminKey is the key used for generating storage credentials
	AdminKey string
	// AdminToken is the token used by the duros-controller to authenticate against the duros API
	AdminToken string

	// APIEndpoint is the endpoint used for control plane network communication.
	APIEndpoint string
	// APICA is the ca of the client cert to access the api endpoint
	APICA string
	// APICert is the cert of the client cert to access the api endpoint
	APICert string
	// APIKey is the key of the client cert to access the api endpoint
	APIKey string
}

