package v1alpha1

import (
	"github.com/go-logr/logr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	ShootDurosProviderResourceName = "extension-duros-provider"
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ControllerConfiguration configuration resource
type DurosProviderConfig struct {
	metav1.TypeMeta `json:",inline"`
}

func (config *DurosProviderConfig) ConfigureDefaults() {
}

func (config *DurosProviderConfig) IsValid(log logr.Logger) bool {
	return true
}
