package duros

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// DurosConfig configuration resource
type DurosConfig struct {
	metav1.TypeMeta

	// ProjectID is the id of the project
	ProjectID string

	// StorageClasses contain information on the storage classes that the duros-controller creates in the shoot cluster
	StorageClasses []DurosSeedStorageClass
}

type DurosSeedStorageClass struct {
	// Name is the name of the storage class
	Name string
	// ReplicaCount is the amount of replicas in the storage backend for this storage class
	ReplicaCount int
	// Compression enables compression for this storage class
	Compression bool
	// Encryption defines a SC with client side encryption enabled
	Encryption bool
	// Default is a flag to det the storage class as default
	Default bool
	// QoS is the qualtiy of service policy which is used by the storageclass
	QoS string
}
