package durosprovider

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// DurosProviderConfig configuration resource
type DurosProviderConfig struct {
	metav1.TypeMeta
}
