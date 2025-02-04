package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	ShootDurosProviderResourceName = "extension-duros-provider-shoot"
	SeedDurosProviderResourceName  = "extension-duros-provider"
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ControllerConfiguration configuration resource
type DurosProviderConfig struct {
	metav1.TypeMeta `json:",inline"`

	//IsEncryptionDisabled is a flag to disable encryption
	IsEncryptionDisabled bool `json:"isEncryptionDisabled,omitempty"`

	//IsDefaultStorageClass is a flag to set the storage class as default
	IsDefaultStorageClass bool `json:"isDefaultStorageClass,omitempty"`
}
