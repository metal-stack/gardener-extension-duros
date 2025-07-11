package config

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	healthcheckconfig "github.com/gardener/gardener/extensions/pkg/apis/config/v1alpha1"
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ControllerConfiguration defines the configuration for the duros controller.
type ControllerConfiguration struct {
	metav1.TypeMeta

	// RegionConfig is a map of a region id to the duros region configuration.
	RegionConfig map[string]DurosRegionConfiguration

	// HealthCheckConfig is the config for the health check controller.
	HealthCheckConfig *healthcheckconfig.HealthCheckConfig
}

// DurosRegionConfiguration is the configuration for duros for a particular region.
type DurosRegionConfiguration struct {
	// Endpoints is the list of endpoints for the storage data plane and control plane communication.
	Endpoints []string
	// AdminKey is the key used for generating storage credentials.
	AdminKey string
	// AdminToken is the token used by the duros-controller to authenticate against the duros API.
	AdminToken string

	// APIEndpoint is the endpoint used for control plane network communication.
	APIEndpoint string
	// APICA is the ca of the client cert to access the api endpoint.
	APICA string
	// APICert is the cert of the client cert to access the api endpoint.
	APICert string
	// APIKey is the key of the client cert to access the api endpoint.
	APIKey string

	// QoSPolicies define quality of service for the duros storage and can be referenced by storageclasses.
	QoSPolicies []QoSPolicy
}

// QoSPolicy defines a quality of service for the duros storage and can be reference by storageclasses.
type QoSPolicy struct {
	// Name is the name of the policy.
	Name string
	// Type is the type of the policy. Different types have different units in the limit.
	Type QoSType
	// Limit is the limit of the policy.
	//
	// Limit of 0 means no rate limit.
	// IOPS in increments of 256 IOPS and Bandwidth in increments of 1 MB/s.
	Limit ReadWriteLimit
}

// ReadWriteLimit defines limits of the policy.
type ReadWriteLimit struct {
	// ReadLimit defines the read operation limit.
	ReadLimit string
	// WriteLimit defines the write operation limit.
	WriteLimit string
}

// QoSType defines the type of the policy. Can be Bandwidth, IOPS or IOPSPerGB.
type QoSType int

const (
	Bandwidth QoSType = iota
	IOPS
	IOPSPerGB
)
