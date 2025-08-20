package duros

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	firewallv1 "github.com/metal-stack/firewall-controller/v2/api/v1"

	"github.com/gardener/gardener/extensions/pkg/controller/extension"
	"github.com/gardener/gardener/pkg/controllerutils/reconciler"

	extensionscontroller "github.com/gardener/gardener/extensions/pkg/controller"
	"github.com/gardener/gardener/pkg/apis/core/v1beta1"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	"github.com/gardener/gardener/pkg/client/kubernetes"
	"github.com/gardener/gardener/pkg/extensions"
	gutil "github.com/gardener/gardener/pkg/utils/gardener"
	"github.com/gardener/gardener/pkg/utils/managedresources"

	"github.com/go-logr/logr"
	"github.com/metal-stack/gardener-extension-duros/pkg/apis/config"
	"github.com/metal-stack/gardener-extension-duros/pkg/apis/duros/v1alpha1"
	"github.com/metal-stack/gardener-extension-duros/pkg/constants"
	"github.com/metal-stack/gardener-extension-duros/pkg/imagevector"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	durosv1 "github.com/metal-stack/duros-controller/api/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	deploymentName string = "duros-controller"
	shootNamespace string = "kube-system"

	pullPolicy corev1.PullPolicy = corev1.PullIfNotPresent

	clusterWideNetworkPolicyCRD string = "clusterwidenetworkpolicies.metal-stack.io"
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
	cluster, err := extensionscontroller.GetCluster(ctx, a.client, ex.GetNamespace())
	if err != nil {
		return fmt.Errorf("unable to get cluster: %w", err)
	}

	durosConfig := &v1alpha1.DurosConfig{}
	if ex.Spec.ProviderConfig != nil {
		_, _, err := a.decoder.Decode(ex.Spec.ProviderConfig.Raw, nil, durosConfig)
		if err != nil {
			return fmt.Errorf("unable to decode provider config: %w", err)
		}
	}

	idx := slices.IndexFunc(cluster.CloudProfile.Spec.Regions, func(region v1beta1.Region) bool {
		return region.Name == cluster.Shoot.Spec.Region
	})
	if idx < 0 {
		return fmt.Errorf("region of shoot not found in cloud profile: %s", cluster.Shoot.Spec.Region)
	}

	regionName := cluster.CloudProfile.Spec.Regions[idx].Name

	regionCfg, ok := a.config.RegionConfig[regionName]
	if !ok {
		return fmt.Errorf("operator provided no duros configuration for this region: %s", regionName)
	}

	cwnpCrd := &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: clusterWideNetworkPolicyCRD,
		},
	}
	if err := a.client.Get(ctx, client.ObjectKeyFromObject(cwnpCrd), cwnpCrd); err == nil {
		log.Info("detected metal-stack cwnp crd, deploying cwnps to seed")

		cwnp := &firewallv1.ClusterwideNetworkPolicy{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "allow-to-storage",
				Namespace: firewallv1.ClusterwideNetworkPolicyNamespace,
			},
		}

		var to []networkv1.IPBlock

		for _, e := range regionCfg.Endpoints {
			withoutPort := strings.Split(e, ":")
			to = append(to, networkv1.IPBlock{
				CIDR: withoutPort[0] + "/32",
			})
		}

		_, err = controllerutil.CreateOrUpdate(ctx, a.client, cwnp, func() error {
			cwnp.Spec.Egress = []firewallv1.EgressRule{
				{
					Ports: []networkv1.NetworkPolicyPort{
						{
							Port:     ptr.To(intstr.FromInt(443)),
							Protocol: ptr.To(corev1.ProtocolTCP),
						},
						{
							Port:     ptr.To(intstr.FromInt(4420)),
							Protocol: ptr.To(corev1.ProtocolTCP),
						},
						{
							Port:     ptr.To(intstr.FromInt(8009)),
							Protocol: ptr.To(corev1.ProtocolTCP),
						},
					},
					To: to,
				},
			}

			return nil
		})

		if err != nil {
			return fmt.Errorf("unable to ensure cwnps: %w", err)
		}
	}

	seedObjects, err := a.getSeedObjects(ctx, cluster, &regionCfg, durosConfig)
	if err != nil {
		return err
	}
	durosObject := a.getSeedDurosObject(durosConfig, &regionCfg, ex.Namespace)
	shootObjects := a.getShootObjects()

	controllerResourcesSeed, err := managedresources.NewRegistry(a.client.Scheme(), serializer.NewCodecFactory(a.client.Scheme()), kubernetes.SeedSerializer).AddAllAndSerialize(seedObjects...)
	if err != nil {
		return err
	}

	err = managedresources.CreateForSeed(ctx, a.client, ex.Namespace, v1alpha1.SeedDurosControllerResourceName, false, controllerResourcesSeed)
	if err != nil {
		return err
	}

	log.Info("managed resource created successfully", "name", v1alpha1.SeedDurosControllerResourceName)

	durosResourcesSeed, err := managedresources.NewRegistry(a.client.Scheme(), serializer.NewCodecFactory(a.client.Scheme()), kubernetes.SeedSerializer).AddAllAndSerialize(durosObject...)
	if err != nil {
		return err
	}

	err = managedresources.CreateForSeed(ctx, a.client, ex.Namespace, v1alpha1.SeedDurosResourceName, false, durosResourcesSeed)
	if err != nil {
		return err
	}

	log.Info("managed resource created successfully", "name", v1alpha1.SeedDurosResourceName)

	shootControlPlaneResources, err := managedresources.NewRegistry(kubernetes.ShootScheme, kubernetes.ShootCodec, kubernetes.ShootSerializer).AddAllAndSerialize(shootObjects...)
	if err != nil {
		return err
	}

	err = managedresources.CreateForShoot(ctx, a.client, ex.Namespace, v1alpha1.ShootDurosResourceName, "duros-extension", false, shootControlPlaneResources)
	if err != nil {
		return err
	}
	log.Info("managed resource created successfully", "name", v1alpha1.ShootDurosResourceName)

	return nil
}

// Delete the Extension resource.
func (a *actuator) Delete(ctx context.Context, log logr.Logger, ex *extensionsv1alpha1.Extension) error {
	log.Info("deleting duros managed resource")

	err := deleteDurosCustomResource(ctx, a.client, ex.Namespace)
	if err != nil {
		return &reconciler.RequeueAfterError{
			Cause:        err,
			RequeueAfter: 30 * time.Second,
		}
	}

	log.Info("deleting shoot managed resource")

	err = managedresources.DeleteForShoot(ctx, a.client, ex.Namespace, v1alpha1.ShootDurosResourceName)
	if err != nil {
		return err
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	err = managedresources.WaitUntilDeleted(timeoutCtx, a.client, ex.Namespace, v1alpha1.ShootDurosResourceName)
	if err != nil {
		return err
	}

	log.Info("deleting seed managed resource")

	err = managedresources.DeleteForSeed(ctx, a.client, ex.Namespace, v1alpha1.SeedDurosControllerResourceName)
	if err != nil {
		return err
	}

	timeoutCtx2, cancel2 := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel2()

	err = managedresources.WaitUntilDeleted(timeoutCtx2, a.client, ex.Namespace, v1alpha1.SeedDurosControllerResourceName)
	if err != nil {
		return err
	}

	return nil
}

func deleteDurosCustomResource(ctx context.Context, c client.Client, namespace string) error {
	durosResource := durosv1.Duros{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "duros-controller",
			Namespace: namespace,
		},
	}
	err := c.Get(ctx, client.ObjectKeyFromObject(&durosResource), &durosResource)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}

		return fmt.Errorf("unable to get duros cr: %w", err)
	}

	if !durosResource.DeletionTimestamp.IsZero() {
		return fmt.Errorf("duros-controller still cleaning up, requeue")
	}

	err = managedresources.DeleteForSeed(ctx, c, namespace, v1alpha1.SeedDurosResourceName)
	if err != nil {
		return fmt.Errorf("unable to delete duros managed resource: %w", err)
	}

	return fmt.Errorf("initializing deletion process of duros cr, requeue")

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

func (a *actuator) getSeedObjects(ctx context.Context, cluster *extensions.Cluster, regionCfg *config.DurosRegionConfiguration, durosCfg *v1alpha1.DurosConfig) ([]client.Object, error) {
	serviceAccount := corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "duros-controller",
			Namespace: cluster.ObjectMeta.Name,
		},
	}

	role := rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "duros-controller",
			Namespace: cluster.ObjectMeta.Name,
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

	roleBinding := rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "duros-controller",
			Namespace: cluster.ObjectMeta.Name,
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "Role",
			Name:     "duros-controller",
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      "duros-controller",
				Namespace: cluster.ObjectMeta.Name,
			},
		},
	}

	secretMap := map[string][]byte{
		"admin-key":   []byte(regionCfg.AdminKey),
		"admin-token": []byte(regionCfg.AdminToken),
	}
	if regionCfg.APICA != "" {
		secretMap["api-ca"] = []byte(regionCfg.APICA)
	}
	if regionCfg.APIKey != "" {
		secretMap["api-key"] = []byte(regionCfg.APIKey)
	}
	if regionCfg.APICert != "" {
		secretMap["api-cert"] = []byte(regionCfg.APICert)
	}
	secret := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "duros-admin",
			Namespace: cluster.ObjectMeta.Name,
			Labels: map[string]string{
				"app": "duros-controller",
			},
		},
		Type: corev1.SecretTypeOpaque,
		Data: secretMap,
	}

	durosControllerArgs := []string{
		fmt.Sprintf("-endpoints=%s", strings.Join(regionCfg.Endpoints, ",")),
		fmt.Sprintf("-namespace=%s", cluster.ObjectMeta.Name),
		"-enable-leader-election",
		"-admin-token=/duros/admin-token",
		"-admin-key=/duros/admin-key",
		"-shoot-kubeconfig=/var/run/secrets/gardener.cloud/shoot/generic-kubeconfig/kubeconfig",
		"-api-endpoint=" + regionCfg.APIEndpoint,
	}
	if regionCfg.APICA != "" {
		durosControllerArgs = append(durosControllerArgs, "-api-ca=/duros/api-ca")
	}
	if regionCfg.APICert != "" && regionCfg.APIKey != "" {
		durosControllerArgs = append(durosControllerArgs, "-api-cert=/duros/api-cert")
		durosControllerArgs = append(durosControllerArgs, "-api-key=/duros/api-key")
	}

	durosControllerImage, err := imagevector.ImageVector().FindImage("duros-controller")
	if err != nil {
		return nil, fmt.Errorf("unable to find duros-controller image: %w", err)
	}

	accessSecret := gutil.NewShootAccessSecret(deploymentName, cluster.ObjectMeta.Name)
	err = accessSecret.Reconcile(ctx, a.client)
	if err != nil {
		return nil, fmt.Errorf("unable to reconcile shoot-access secret: %w", err)
	}

	deployment := appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "duros-controller",
			Namespace: cluster.ObjectMeta.Name,
			Labels: map[string]string{
				"app":                        "duros-controller",
				"app.kubernetes.io/instance": "gardener-extension-duros",
			},
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app":                        "duros-controller",
					"app.kubernetes.io/instance": "gardener-extension-duros",
				},
			},
			Replicas: nil,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Name: "duros-controller",
					Labels: map[string]string{
						"networking.gardener.cloud/from-prometheus":                     "allowed",
						"networking.gardener.cloud/to-dns":                              "allowed",
						"networking.gardener.cloud/to-shoot-apiserver":                  "allowed",
						"networking.gardener.cloud/to-private-networks":                 "allowed",
						"networking.gardener.cloud/to-public-networks":                  "allowed",
						"networking.gardener.cloud/to-runtime-apiserver":                "allowed",
						"networking.resources.gardener.cloud/to-kube-apiserver-tcp-443": "allowed",
						"app.kubernetes.io/instance":                                    "gardener-extension-duros",
						"app":                                                           "duros-controller",
					},
				},
				Spec: corev1.PodSpec{
					AutomountServiceAccountToken: ptr.To(true),
					ServiceAccountName:           "duros-controller",
					Containers: []corev1.Container{
						{
							Name:            "duros-controller",
							Args:            durosControllerArgs,
							Image:           durosControllerImage.String(),
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
									MountPath: "/var/run/secrets/gardener.cloud/shoot/generic-kubeconfig",
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
													Name: accessSecret.Secret.Name,
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
		ObjectMeta: metav1.ObjectMeta{
			Name:      "egress-from-duros-controller-to-storage",
			Namespace: cluster.ObjectMeta.Name,
		},
		Spec: networkv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{
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

	objects := []client.Object{
		&serviceAccount,
		&role,
		&roleBinding,
		&secret,
		&deployment,
		&networkPolicy,
	}

	return objects, nil
}

func (a *actuator) getSeedDurosObject(durosCfg *v1alpha1.DurosConfig, regionCfg *config.DurosRegionConfiguration, namespace string) []client.Object {
	var scs []durosv1.StorageClass

	for _, sc := range durosCfg.StorageClasses {
		scs = append(scs, durosv1.StorageClass{
			Name:         sc.Name,
			ReplicaCount: sc.ReplicaCount,
			Compression:  sc.Compression,
			Encryption:   sc.Encryption,
			Default:      sc.Default,
		})
	}

	durosStorage := durosv1.Duros{
		ObjectMeta: metav1.ObjectMeta{
			Name:      constants.DurosResourceName,
			Namespace: namespace,
		},
		Spec: durosv1.DurosSpec{
			MetalProjectID: durosCfg.ProjectID,
			StorageClasses: scs,
		},
	}

	objects := []client.Object{
		&durosStorage,
	}

	return objects
}

func (a *actuator) getShootObjects() []client.Object {
	role := rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "duros-controller",
			Namespace: shootNamespace,
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{"snapshot.storage.k8s.io"},
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
		ObjectMeta: metav1.ObjectMeta{
			Name:      "duros-controller",
			Namespace: shootNamespace,
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      "duros-controller",
				Namespace: shootNamespace,
			},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Name:     "duros-controller",
			Kind:     "ClusterRole",
		},
	}

	objects := []client.Object{
		&role,
		&roleBinding,
	}
	return objects
}
