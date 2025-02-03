package durosprovider

import (
	"context"
	"fmt"
	"time"

	"github.com/gardener/gardener/extensions/pkg/controller/extension"
	"github.com/metal-stack/metal-go/api/models"
	"github.com/metal-stack/metal-lib/pkg/tag"

	"github.com/gardener/gardener/extensions/pkg/controller"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	"github.com/gardener/gardener/pkg/utils/managedresources"
		extensionscontroller "github.com/gardener/gardener/extensions/pkg/controller"


	apismetal "github.com/metal-stack/gardener-extension-provider-metal/pkg/apis/metal"

	"github.com/go-logr/logr"
	"github.com/metal-stack/gardener-extension-duros-provider/pkg/apis/config"
	"github.com/metal-stack/gardener-extension-duros-provider/pkg/apis/durosprovider/v1alpha1"
	"k8s.io/apimachinery/pkg/api/resource"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	durosv1 "github.com/metal-stack/duros-controller/api/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
)

const (
	pullPolicy corev1.PullPolicy = corev1.PullIfNotPresent
)

// NewActuator returns an actuator responsible for Extension resources.
func NewActuator(mgr manager.Manager, config config.ControllerConfiguration) extension.Actuator {
	return &actuator{
		client:  mgr.GetClient(),
		decoder: serializer.NewCodecFactory(mgr.GetScheme(), serializer.EnableStrict).UniversalDecoder(),
		config:  config,
	}
}

type actuator struct {
	client  client.Client
	decoder runtime.Decoder
	config  config.ControllerConfiguration
}

// Reconcile the Extension resource.
func (a *actuator) Reconcile(ctx context.Context, log logr.Logger, ex *extensionsv1alpha1.Extension) error {
	// get cluster of extension and decode infrastructure config
	cluster, err := controller.GetCluster(ctx, a.client, ex.GetNamespace())
	if err != nil {
		return fmt.Errorf("could not get cluster: %w", err)
	}
	infrastructureConfig := &apismetal.InfrastructureConfig{}
	if _, _, err := a.decoder.Decode(cluster.Shoot.Spec.Provider.InfrastructureConfig.Raw, nil, infrastructureConfig); err != nil {
		return fmt.Errorf("could not decode providerConfig of infrastructure %w", err)
	}

	// decode provider config and check if it is valid
	durosproviderConfig := &v1alpha1.DurosProviderConfig{}
	if ex.Spec.ProviderConfig != nil {
		_, _, err := a.decoder.Decode(ex.Spec.ProviderConfig.Raw, nil, durosproviderConfig)
		if err != nil {
			return fmt.Errorf("failed to decode provider config: %w", err)
		}
	}
	if !durosproviderConfig.IsValid(log) {
		return fmt.Errorf("invalid duros-provider configuration")
	}

	partitionConfig, ok := durosproviderConfig.PartitionConfig[infrastructureConfig.PartitionID]
	if !ok {
		log.Info("skipping duros storage deployment because no storage configuration found for partition", "partition", infrastructureConfig.PartitionID)
		return nil
	}

		var scs []map[string]any
	for _, sc := range partitionConfig.StorageClasses {
		if cp.FeatureGates.DurosStorageEncryption == nil || !*cp.FeatureGates.DurosStorageEncryption {
			if sc.Encryption {
				continue
			}
		}

		isDefaultSC := false
		if cp.CustomDefaultStorageClass != nil && cp.CustomDefaultStorageClass.ClassName == sc.Name {
			isDefaultSC = true
		}

		scs = append(scs, map[string]any{
			"name":        sc.Name,
			"replicas":    sc.ReplicaCount,
			"compression": sc.Compression,
			"encryption":  sc.Encryption,
			"default":     isDefaultSC,
		})
	}

	controllerValues := map[string]any{
		"endpoints":   partitionConfig.Endpoints,
		"adminKey":    partitionConfig.AdminKey,
		"adminToken":  partitionConfig.AdminToken,
		"apiEndpoint": partitionConfig.APIEndpoint,
	}

	if partitionConfig.APICA != "" {
		controllerValues["apiCA"] = partitionConfig.APICA
	}
	if partitionConfig.APICert != "" && partitionConfig.APIKey != "" {
		controllerValues["apiCert"] = partitionConfig.APICert
		controllerValues["apiKey"] = partitionConfig.APIKey
	}

	values := map[string]any{
		"duros": map[string]any{
			"replicas":       extensionscontroller.GetReplicas(cluster, 1),
			"storageClasses": scs,
			"projectID":      infrastructureConfig.ProjectID,
			"controller":     controllerValues,
		},
	}


	// controlPlaneObjects, err := a.controlPlaneObjects(durosproviderConfig)
	// if err != nil {
	// 	return err
	// }
	//
	// pluginObjects, err := a.shootObjects()
	// if err != nil {
	// 	return err
	// }
	//
	// objects := []client.Object{}
	// shootResources, err := managedresources.NewRegistry(kubernetes.ShootScheme, kubernetes.ShootCodec, kubernetes.ShootSerializer).AddAllAndSerialize(objects...)
	// if err != nil {
	// 	return err
	// }
	//
	// err = managedresources.CreateForShoot(ctx, a.client, ex.Namespace, v1alpha1.ShootDurosProviderResourceName, "duros-provider-extension", false, shootResources)
	//
	// if err != nil {
	// 	return err
	// }

	log.Info("managed resource created succesfully", "name", v1alpha1.ShootDurosProviderResourceName)

	return nil
}

// Delete the Extension resource.
func (a *actuator) Delete(ctx context.Context, log logr.Logger, ex *extensionsv1alpha1.Extension) error {
	log.Info("deleting managed resource")
	err := managedresources.Delete(ctx, a.client, ex.Namespace, v1alpha1.ShootDurosProviderResourceName, false)

	if err != nil {
		return err
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	err = managedresources.WaitUntilDeleted(timeoutCtx, a.client, ex.Namespace, v1alpha1.ShootDurosProviderResourceName)
	if err != nil {
		return err
	}

	log.Info("successfully deleted managed resource")

	return nil
}

// ForceDelete the Extension resource
func (a *actuator) ForceDelete(_ context.Context, _ logr.Logger, _ *extensionsv1alpha1.Extension) error {
	return nil
}

// Restore the Extension resource.
func (a *actuator) Restore(ctx context.Context, log logr.Logger, ex *extensionsv1alpha1.Extension) error {
	return a.Reconcile(ctx, log, ex)
}

// Migrate the Extension resource.
func (a *actuator) Migrate(ctx context.Context, log logr.Logger, ex *extensionsv1alpha1.Extension) error {
	return nil
}

func (a *actuator) controlPlaneObjects(cfg *v1alpha1.DurosProviderConfig) ([]client.Object, error) {

	serviceAccount := corev1.ServiceAccount{
		ObjectMeta: v1.ObjectMeta{
			Name: "duros-controller",
		},
	}

	role := rbacv1.ClusterRole{
		ObjectMeta: v1.ObjectMeta{
			Name: "duros-controller",
		},
		Rules: []rbacv1.PolicyRule{
			rbacv1.PolicyRule{
				APIGroups: []string{"storage.metal-stack.io"},
				Resources: []string{"duros", "duros/status"},
				Verbs:     []string{"create", "delete", "get", "list", "patch", "update", "watch"},
			},
			rbacv1.PolicyRule{
				APIGroups: []string{"", "coordination.k8s.io"},
				Resources: []string{"configmaps", "leases", "events"},
				Verbs:     []string{"create", "get", "patch", "update", "watch"},
			},
		},
	}

	roleBinding := rbacv1.ClusterRoleBinding{
		ObjectMeta: v1.ObjectMeta{
			Name: "duros-controller",
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "Role",
			Name:     "duros-controller",
		},
		Subjects: []rbacv1.Subject{
			rbacv1.Subject{
				Kind: "ServiceAccount",
				Name: "duros-controller",
			},
		},
	}

	secret := corev1.Secret{
		ObjectMeta: v1.ObjectMeta{
			Name: "duros-admin",
			Labels: map[string]string{
				"app": "duros-controller",
			},
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"admin-key":   nil,
			"admin-token": nil,
			"api-ca":      nil,
			"api-key":     nil,
		},
	}

	durosControllerArgs := []string{
		"-endpoints=",
		"-namespace=",
		"-enable-leader-election",
		"-admin-token=/duros/admin-token",
		"-admin-key=/duros/admin-key",
		"-shoot-kubeconfig=/var/run/secrets/gardener.cloud/shoot/generic-kubeconfig/kubeconfig",
		"-api-endpoint=",
	}
	// TODO check custom variables (api-ca, api-cert, api-key)

	deployment := appsv1.Deployment{
		ObjectMeta: v1.ObjectMeta{
			Name: "duros-controller",
			Labels: map[string]string{
				"app": "duros-controller",
			},
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &v1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "duros-controller",
				},
			},
			Replicas: nil,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: v1.ObjectMeta{
					Name: "duros-controller",
					Labels: map[string]string{
						"networking.gardener.cloud/from-prometheus":                     "allowed",
						"networking.gardener.cloud/to-dns":                              "allowed",
						"networking.gardener.cloud/to-shoot-apiserver":                  "allowed",
						"networking.gardener.cloud/to-private-networks":                 "allowed",
						"networking.gardener.cloud/to-public-networks":                  "allowed",
						"networking.gardener.cloud/to-runtime-apiserver":                "allowed",
						"networking.resources.gardener.cloud/to-kube-apiserver-tcp-443": "allowed",
					},
				},
				Spec: corev1.PodSpec{
					AutomountServiceAccountToken: ptr.To[bool](true),
					ServiceAccountName:           "duros-controller",
					Containers: []corev1.Container{
						corev1.Container{
							Name:            "duros-controller",
							Args:            durosControllerArgs,
							ImagePullPolicy: pullPolicy,
							Resources: corev1.ResourceRequirements{
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("400m"),
									corev1.ResourceMemory: resource.MustParse("100Mi"),
								},
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("100m"),
									corev1.ResourceMemory: resource.MustParse("20Mi"),
								},
							},
							VolumeMounts: []corev1.VolumeMount{
								corev1.VolumeMount{
									Name:      "duros-admin",
									MountPath: "/duros",
								},
								corev1.VolumeMount{
									Name:      "kubeconfig",
									MountPath: "/var/run/secrets/gardener.cloud/shoot/generice-kubeconfig",
									ReadOnly:  true,
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						corev1.Volume{
							Name: "duros-admin",
							VolumeSource: corev1.VolumeSource{
								Secret: &corev1.SecretVolumeSource{
									SecretName: "duros-admin",
								},
							},
						},
						corev1.Volume{
							Name: "kubeconfig",
							VolumeSource: corev1.VolumeSource{
								Projected: &corev1.ProjectedVolumeSource{
									DefaultMode: ptr.To[int32](432),
									Sources: []corev1.VolumeProjection{
										corev1.VolumeProjection{
											Secret: &corev1.SecretProjection{
												Items: []corev1.KeyToPath{
													corev1.KeyToPath{
														Key:  "kubeconfig",
														Path: "kubeconfig",
													},
												},
												//NAME ??? not available
												Optional: ptr.To[bool](false),
											},
										},
										corev1.VolumeProjection{
											Secret: &corev1.SecretProjection{
												Items: []corev1.KeyToPath{
													corev1.KeyToPath{
														Key:  "token",
														Path: "token",
													},
												},
												//NAME ??? not available
												Optional: ptr.To[bool](false),
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	networkPolicy := networkv1.NetworkPolicy{
		ObjectMeta: v1.ObjectMeta{
			Name: "egress-from-duros-controller-to-storage",
		},
		Spec: networkv1.NetworkPolicySpec{
			PodSelector: v1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "duros-controller",
				},
			},
			PolicyTypes: []networkv1.PolicyType{"Egress"},
			Egress: []networkv1.NetworkPolicyEgressRule{
				networkv1.NetworkPolicyEgressRule{
					To: []networkv1.NetworkPolicyPeer{
						networkv1.NetworkPolicyPeer{
							IPBlock: &networkv1.IPBlock{
								CIDR: "0.0.0.0/0",
							},
						},
					},
					Ports: []networkv1.NetworkPolicyPort{
						networkv1.NetworkPolicyPort{
							Protocol: ptr.To(corev1.ProtocolTCP),
							Port: &intstr.IntOrString{
								Type:   intstr.Int,
								IntVal: int32(443),
							},
						},
						networkv1.NetworkPolicyPort{
							Protocol: ptr.To(corev1.ProtocolTCP),
							Port: &intstr.IntOrString{
								Type:   intstr.Int,
								IntVal: int32(25005),
							},
						},
					},
				},
			},
		},
	}

	durosStorage := durosv1.Duros{
		ObjectMeta: v1.ObjectMeta{
			Name: "duros-controller",
		},
		Spec: durosv1.DurosSpec{
			// get projectId and storage class from config
			MetalProjectID: nil,
			StorageClasses: []durosv1.StorageClass{},
		},
	}

	objects := []client.Object{}

	return objects, nil
}

func (a *actuator) shootObjects() ([]client.Object, error) {

	role := rbacv1.ClusterRole{
		ObjectMeta: v1.ObjectMeta{
			Name: "duros-controller",
		},
		Rules: []rbacv1.PolicyRule{
			rbacv1.PolicyRule{
				APIGroups: []string{"shanpshot.storage.k8s.io"},
				Resources: []string{"volumesnapshotclasses", "volumesnapshotcontents", "volumesnapshotcontents/status", "volumesnapshots", "volumesnapshots/status"},
				Verbs:     []string{"create", "delete", "get", "list", "patch", "update", "watch"},
			},
			rbacv1.PolicyRule{
				APIGroups: []string{""},
				Resources: []string{"secrets"},
				Verbs:     []string{"create", "delete", "get", "list", "patch", "update", "watch"},
			},
			rbacv1.PolicyRule{
				APIGroups: []string{"storage.k8s.io"},
				Resources: []string{"csidrivers", "csinodes", "volumeattachments", "volumeattachments/status", "storageclasses"},
				Verbs:     []string{"create", "delete", "get", "list", "patch", "update", "watch"},
			},
			rbacv1.PolicyRule{
				APIGroups: []string{"rbac.authorization.k8s.io"},
				Resources: []string{"clusterroles", "clusterrolebindings"},
				Verbs:     []string{"create", "delete", "get", "list", "patch", "update", "watch"},
			},
			rbacv1.PolicyRule{
				APIGroups: []string{"apps"},
				Resources: []string{"statefulsets", "daemonsets"},
				Verbs:     []string{"create", "delete", "get", "list", "patch", "update", "watch"},
			},
			rbacv1.PolicyRule{
				APIGroups: []string{""},
				Resources: []string{"configmaps", "events", "secrets", "serviceaccounts", "nodes", "persistentvolumes", "persistentvoleclaims", "persistentvolumeclaims/status", "pods"},
				Verbs:     []string{"create", "delete", "get", "list", "patch", "update", "watch"},
			},
		},
	}

	roleBinding := rbacv1.ClusterRoleBinding{
		ObjectMeta: v1.ObjectMeta{
			Name: "duros-controller",
		},
		Subjects: []rbacv1.Subject{
			rbacv1.Subject{
				Kind: "serviceaccount",
				Name: "duros-controllerS",
			},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Name:     "duros-controller",
		},
	}

	objects := []client.Object{
		&role,
		&roleBinding,
	}
	return objects, nil
}

type networkMap map[string]*models.V1NetworkResponse

func hasDurosStorageNetwork(infrastructure *apismetal.InfrastructureConfig, nws networkMap) (bool, error) {
	for _, networkID := range infrastructure.Firewall.Networks {
		nw, ok := nws[networkID]
		if !ok {
			return false, fmt.Errorf("network defined in firewall networks does not exist in metal-api")
		}
		if nw.Partitionid != infrastructure.PartitionID {
			continue
		}
		for k := range nw.Labels {
			if k == tag.NetworkPartitionStorage {
				return true, nil
			}
		}
	}
	return false, nil
}
