package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	ShootDurosResourceName          = "extension-duros-shoot"
	SeedDurosControllerResourceName = "extension-duros-controller-seed"
	SeedDurosResourceName           = "extension-duros-resource-seed"
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// DurosProviderConfiguration configuration resource
type DurosProviderConfig struct {
	metav1.TypeMeta `json:",inline"`

	// ProjectID is the id of the project
	ProjectID string `json:"projectID"`

	// PartitionID is the id of the partition
	PartitionID string `json:"partitionID"`

	// StorageClasses contain information on the storage classes that the duros-controller creates in the shoot cluster
	StorageClasses []DurosSeedStorageClass `json:"storageClasses"`
}

type DurosSeedStorageClass struct {
	// Name is the name of the storage class
	Name string `json:"name"`
	// ReplicaCount is the amount of replicas in the storage backend for this storage class
	ReplicaCount int `json:"replicaCount"`
	// Compression enables compression for this storage class
	Compression bool `json:"compression"`
	// Encryption defines a SC with client side encryption enabled
	Encryption bool `json:"encryption"`
	// Default is a flag to set the storage class as default
	Default bool `json:"default"`
}
