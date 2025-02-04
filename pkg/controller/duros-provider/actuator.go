package durosprovider

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/gardener/gardener/extensions/pkg/controller/extension"

	"github.com/gardener/gardener/extensions/pkg/controller"
	extensionscontroller "github.com/gardener/gardener/extensions/pkg/controller"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	"github.com/gardener/gardener/pkg/client/kubernetes"
	"github.com/gardener/gardener/pkg/extensions"
	"github.com/gardener/gardener/pkg/utils/managedresources"

	apisdurosprovider "github.com/metal-stack/gardener-extension-duros-provider/pkg/apis/durosprovider"

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
	infrastructureConfig := &apisdurosprovider.InfrastructureConfig{}
	if _, _, err := a.decoder.Decode(cluster.Shoot.Spec.Provider.InfrastructureConfig.Raw, nil, infrastructureConfig); err != nil {
		return fmt.Errorf("could not decode providerConfig of infrastructure %w", err)
	}

	// decode provider config
	durosproviderConfig := &apisdurosprovider.DurosProviderConfig{}
	if ex.Spec.ProviderConfig != nil {
		_, _, err := a.decoder.Decode(ex.Spec.ProviderConfig.Raw, nil, durosproviderConfig)
		if err != nil {
			return fmt.Errorf("failed to decode provider config: %w", err)
		}
	}

	partitionConfig, ok := a.config.PartitionConfig[infrastructureConfig.PartitionID]
	if !ok {

		log.Info("skipping duros storage deployment because no storage configuration found for partition", "partition", infrastructureConfig.PartitionID)
		return nil
	}

	controlPlaneObjects, err := a.controlPlaneObjects(infrastructureConfig.PartitionID, cluster, partitionConfig, v1alpha1.DurosProviderConfig(*durosproviderConfig))
	if err != nil {
		return err
	}
	shootControlPlaneObjects := a.shootControlPlaneObjects()

	controlPlaneResources, err := managedresources.NewRegistry(kubernetes.SeedScheme, kubernetes.SeedCodec, kubernetes.SeedSerializer).AddAllAndSerialize(controlPlaneObjects...)
	if err != nil {
		return err
	}
	shootControlPlaneResources, err := managedresources.NewRegistry(kubernetes.SeedScheme, kubernetes.SeedCodec, kubernetes.SeedSerializer).AddAllAndSerialize(shootControlPlaneObjects...)
	if err != nil {
		return err
	}

	err = managedresources.CreateForSeed(ctx, a.client, ex.Namespace, v1alpha1.SeedDurosProviderResourceName, false, controlPlaneResources)
	if err != nil {
		return err
	}
	log.Info("managed resource created succesfully", "name", v1alpha1.SeedDurosProviderResourceName)

	err = managedresources.CreateForSeed(ctx, a.client, cluster.Shoot.GetNamespace(), v1alpha1.ShootDurosProviderResourceName, false, shootControlPlaneResources)
	if err != nil {
		return err
	}
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

func (a *actuator) controlPlaneObjects(projectId string, cluster *extensions.Cluster, partitionCfg config.DurosPartitionConfiguration, durosproviderCfg v1alpha1.DurosProviderConfig) ([]client.Object, error) {
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
			{
				APIGroups: []string{"storage.metal-stack.io"},
				Resources: []string{"duros", "duros/status"},
				Verbs:     []string{"create", "delete", "get", "list", "patch", "update", "watch"},
			},
			{
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
			{
				Kind: "ServiceAccount",
				Name: "duros-controller",
			},
		},
	}

	secretMap := map[string][]byte{
		"admin-key":   []byte(partitionCfg.AdminKey),
		"admin-token": []byte(partitionCfg.AdminToken),
	}
	if partitionCfg.APICA != "" {
		secretMap["api-ca"] = []byte(partitionCfg.APICA)
	}
	if partitionCfg.APIKey != "" {
		secretMap["api-key"] = []byte(partitionCfg.APIKey)
	}
	secret := corev1.Secret{
		ObjectMeta: v1.ObjectMeta{
			Name: "duros-admin",
			Labels: map[string]string{
				"app": "duros-controller",
			},
		},
		Type: corev1.SecretTypeOpaque,
		Data: secretMap,
	}

	durosControllerArgs := []string{
		"-endpoints=" + strings.Join(partitionCfg.Endpoints, ","),
		"-namespace=",
		"-enable-leader-election",
		"-admin-token=/duros/admin-token",
		"-admin-key=/duros/admin-key",
		"-shoot-kubeconfig=/var/run/secrets/gardener.cloud/shoot/generic-kubeconfig/kubeconfig",
		"-api-endpoint=" + partitionCfg.APIEndpoint,
	}
	if partitionCfg.APICA != "" {
		durosControllerArgs = append(durosControllerArgs, "-api-ca=/duros/api-ca")
	}
	if partitionCfg.APICert != "" && partitionCfg.APIKey != "" {
		durosControllerArgs = append(durosControllerArgs, "-api-cert=/duros/api-cert")
		durosControllerArgs = append(durosControllerArgs, "-api-key=/duros/api-key")
	}

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
					AutomountServiceAccountToken: ptr.To(true),
					ServiceAccountName:           "duros-controller",
					Containers: []corev1.Container{
						{
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
								{
									Name:      "duros-admin",
									MountPath: "/duros",
								},
								{
									Name:      "kubeconfig",
									MountPath: "/var/run/secrets/gardener.cloud/shoot/generice-kubeconfig",
									ReadOnly:  true,
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "duros-admin",
							VolumeSource: corev1.VolumeSource{
								Secret: &corev1.SecretVolumeSource{
									SecretName: "duros-admin",
								},
							},
						},
						{
							Name: "kubeconfig",
							VolumeSource: corev1.VolumeSource{
								Projected: &corev1.ProjectedVolumeSource{
									DefaultMode: ptr.To[int32](420),
									Sources: []corev1.VolumeProjection{
										{
											Secret: &corev1.SecretProjection{
												Items: []corev1.KeyToPath{
													{
														Key:  "kubeconfig",
														Path: "kubeconfig",
													},
												},
												LocalObjectReference: corev1.LocalObjectReference{
													Name: extensionscontroller.GenericTokenKubeconfigSecretNameFromCluster(cluster),
												},
												Optional: ptr.To(false),
											},
										},
										{
											Secret: &corev1.SecretProjection{
												Items: []corev1.KeyToPath{
													{
														Key:  "token",
														Path: "token",
													},
												},
												LocalObjectReference: corev1.LocalObjectReference{
													Name: "shoot-access-duros-controller",
												},
												Optional: ptr.To(false),
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
				{
					To: []networkv1.NetworkPolicyPeer{
						{
							IPBlock: &networkv1.IPBlock{
								CIDR: "0.0.0.0/0",
							},
						},
					},
					Ports: []networkv1.NetworkPolicyPort{
						{
							Protocol: ptr.To(corev1.ProtocolTCP),
							Port: &intstr.IntOrString{
								Type:   intstr.Int,
								IntVal: int32(443),
							},
						},
						{
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

	var scs []durosv1.StorageClass
	for _, sc := range partitionCfg.StorageClasses {
		if !durosproviderCfg.IsEncryptionDisabled {
			if sc.Encryption {
				continue
			}
		}

		scs = append(scs, durosv1.StorageClass{
			Name:         sc.Name,
			ReplicaCount: sc.ReplicaCount,
			Compression:  sc.Compression,
			Encryption:   sc.Encryption,
			Default:      durosproviderCfg.IsDefaultStorageClass,
		})
	}
	durosStorage := durosv1.Duros{
		ObjectMeta: v1.ObjectMeta{
			Name: "duros-controller",
		},
		Spec: durosv1.DurosSpec{
			MetalProjectID: projectId,
			StorageClasses: scs,
		},
	}

	objects := []client.Object{
		&serviceAccount,
		&role,
		&roleBinding,
		&secret,
		&deployment,
		&networkPolicy,
		&durosStorage,
	}

	return objects, nil
}

func (a *actuator) shootControlPlaneObjects() []client.Object {

	role := rbacv1.ClusterRole{
		ObjectMeta: v1.ObjectMeta{
			Name: "duros-controller",
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{"shanpshot.storage.k8s.io"},
				Resources: []string{"volumesnapshotclasses", "volumesnapshotcontents", "volumesnapshotcontents/status", "volumesnapshots", "volumesnapshots/status"},
				Verbs:     []string{"create", "delete", "get", "list", "patch", "update", "watch"},
			},
			{
				APIGroups: []string{""},
				Resources: []string{"secrets"},
				Verbs:     []string{"create", "delete", "get", "list", "patch", "update", "watch"},
			},
			{
				APIGroups: []string{"storage.k8s.io"},
				Resources: []string{"csidrivers", "csinodes", "volumeattachments", "volumeattachments/status", "storageclasses"},
				Verbs:     []string{"create", "delete", "get", "list", "patch", "update", "watch"},
			},
			{
				APIGroups: []string{"rbac.authorization.k8s.io"},
				Resources: []string{"clusterroles", "clusterrolebindings"},
				Verbs:     []string{"create", "delete", "get", "list", "patch", "update", "watch"},
			},
			{
				APIGroups: []string{"apps"},
				Resources: []string{"statefulsets", "daemonsets"},
				Verbs:     []string{"create", "delete", "get", "list", "patch", "update", "watch"},
			},
			{
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
			{
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
	return objects
}
