package durosprovider

import (
	"context"
	"fmt"
	"time"

	"github.com/gardener/gardener/extensions/pkg/controller/extension"

	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	"github.com/gardener/gardener/pkg/client/kubernetes"
	"github.com/gardener/gardener/pkg/utils/managedresources"

	"github.com/go-logr/logr"
	"github.com/metal-stack/gardener-extension-duros-provider/pkg/apis/config"
	"github.com/metal-stack/gardener-extension-duros-provider/pkg/apis/durosprovider/v1alpha1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	corev1 "k8s.io/api/core/v1"
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
	durosproviderConfig := &v1alpha1.DurosProviderConfig{}
	if ex.Spec.ProviderConfig != nil {
		_, _, err := a.decoder.Decode(ex.Spec.ProviderConfig.Raw, nil, durosproviderConfig)
		if err != nil {
			return fmt.Errorf("failed to decode provider config: %w", err)
		}
	}

	durosproviderConfig.ConfigureDefaults()
	if !durosproviderConfig.IsValid(log) {
		return fmt.Errorf("invalid duros-provider configuration")
	}

	controllerObjects, err := a.controllerObjects()
	if err != nil {
		return err
	}

	pluginObjects, err := a.pluginObjects(durosproviderConfig)
	if err != nil {
		return err
	}

	objects := []client.Object{}
	objects = append(objects, controllerObjects...)
	objects = append(objects, pluginObjects...)

	shootResources, err := managedresources.NewRegistry(kubernetes.ShootScheme, kubernetes.ShootCodec, kubernetes.ShootSerializer).AddAllAndSerialize(objects...)
	if err != nil {
		return err
	}

	err = managedresources.CreateForShoot(ctx, a.client, ex.Namespace, v1alpha1.ShootDurosProviderResourceName, "duros-provider-extension", false, shootResources)

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

func (a *actuator) controllerObjects() ([]client.Object, error) {
	objects := []client.Object{}

	return objects, nil
}

func (a *actuator) pluginObjects(durosproviderConfig *v1alpha1.DurosProviderConfig) ([]client.Object, error) {
	objects := []client.Object{}

	return objects, nil
}
