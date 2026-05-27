package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	durosv1 "github.com/metal-stack/duros-controller/api/v1"
	firewallv1 "github.com/metal-stack/firewall-controller/v2/api/v1"
	"github.com/metal-stack/gardener-extension-duros/charts"
	"github.com/metal-stack/gardener-extension-duros/pkg/apis/duros/install"
	duroscmd "github.com/metal-stack/gardener-extension-duros/pkg/cmd"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"

	extensionscontroller "github.com/gardener/gardener/extensions/pkg/controller"
	heartbeatcontroller "github.com/gardener/gardener/extensions/pkg/controller/heartbeat"
	heartbeatcmd "github.com/gardener/gardener/extensions/pkg/controller/heartbeat/cmd"
	controller "github.com/metal-stack/gardener-extension-duros/pkg/controller/duros"
	"github.com/metal-stack/gardener-extension-duros/pkg/controller/healthcheck"

	controllercmd "github.com/gardener/gardener/extensions/pkg/controller/cmd"
	"github.com/gardener/gardener/extensions/pkg/util"
	"github.com/gardener/gardener/pkg/client/kubernetes"
	ghealth "github.com/gardener/gardener/pkg/healthz"
	componentbaseconfigv1alpha1 "k8s.io/component-base/config/v1alpha1"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

const ExtensionName = "extension-duros"

type Options struct {
	generalOptions     *controllercmd.GeneralOptions
	durosOptions       *duroscmd.DurosOptions
	restOptions        *controllercmd.RESTOptions
	managerOptions     *controllercmd.ManagerOptions
	controllerOptions  *controllercmd.ControllerOptions
	heartbeatOptions   *heartbeatcmd.Options
	healthOptions      *controllercmd.ControllerOptions
	controllerSwitches *controllercmd.SwitchOptions
	reconcileOptions   *controllercmd.ReconcilerOptions
	optionAggregator   controllercmd.OptionAggregator
}

func NewOptions() *Options {
	options := &Options{
		generalOptions: &controllercmd.GeneralOptions{},
		durosOptions:   &duroscmd.DurosOptions{},
		restOptions:    &controllercmd.RESTOptions{},
		managerOptions: &controllercmd.ManagerOptions{
			LeaderElection:          true,
			LeaderElectionID:        controllercmd.LeaderElectionNameID(ExtensionName),
			LeaderElectionNamespace: os.Getenv("LEADER_ELECTION_NAMESPACE"),
			MetricsBindAddress:      ":8080",
			HealthBindAddress:       ":8081",
		},

		// options for the controlplane controller
		controllerOptions: &controllercmd.ControllerOptions{
			MaxConcurrentReconciles: 5,
		},

		heartbeatOptions: &heartbeatcmd.Options{
			// This is a default value.
			ExtensionName:        ExtensionName,
			RenewIntervalSeconds: 30,
			Namespace:            os.Getenv("LEADER_ELECTION_NAMESPACE"),
		},
		healthOptions: &controllercmd.ControllerOptions{
			// This is a default value.
			MaxConcurrentReconciles: 5,
		},
		controllerSwitches: duroscmd.ControllerSwitchOptions(),
		reconcileOptions:   &controllercmd.ReconcilerOptions{},
	}

	options.optionAggregator = controllercmd.NewOptionAggregator(
		options.generalOptions,
		options.durosOptions,
		options.restOptions,
		options.managerOptions,
		options.controllerOptions,
		controllercmd.PrefixOption("heartbeat-", options.heartbeatOptions),
		controllercmd.PrefixOption("healthcheck-", options.healthOptions),
		options.controllerSwitches,
		options.reconcileOptions,
	)

	return options
}

// // GroupName is the group name use in this package
// const GroupName = "duros.metal.extensions.gardener.cloud"
//
// // SchemeGroupVersion is group version used to register these objects
// var SchemeGroupVersion = schema.GroupVersion{Group: GroupName, Version: runtime.APIVersionInternal}

func (options *Options) run(ctx context.Context) error {
	log.Info("starting " + ExtensionName)

	util.ApplyClientConnectionConfigurationToRESTConfig(&componentbaseconfigv1alpha1.ClientConnectionConfiguration{
		QPS:   100.0,
		Burst: 130,
	}, options.restOptions.Completed().Config)

	log.Info("applied rest config")

	mgrOpts := options.managerOptions.Completed().Options()

	log.Info("completed mgr-options")

	mgrOpts.Client = client.Options{
		Cache: &client.CacheOptions{
			DisableFor: []client.Object{
				&corev1.Secret{},
				&corev1.ConfigMap{},
			},
		},
	}

	mgr, err := manager.New(options.restOptions.Completed().Config, mgrOpts)
	if err != nil {
		return fmt.Errorf("could not instantiate controller-manager: %w", err)
	}
	log.Info("completed rest-options")

	for _, schemeFn := range []func(*runtime.Scheme) error{
		firewallv1.AddToScheme,
		durosv1.AddToScheme,
		extensionscontroller.AddToScheme,
		install.AddToScheme,
	} {
		if err := schemeFn(mgr.GetScheme()); err != nil {
			return fmt.Errorf("could not add scheme: %w", err)
		}
	}

	ctrlConfig := options.durosOptions.Completed()
	ctrlConfig.Apply(&controller.DefaultAddOptions.Config)

	options.controllerOptions.Completed().Apply(&controller.DefaultAddOptions.ControllerOptions)
	options.reconcileOptions.Completed().Apply(&controller.DefaultAddOptions.IgnoreOperationAnnotation, &controller.DefaultAddOptions.ExtensionClass)
	options.heartbeatOptions.Completed().Apply(&heartbeatcontroller.DefaultAddOptions)

	ca, err := kubernetes.NewChartApplierForConfig(options.restOptions.Completed().Config)
	if err != nil {
		return fmt.Errorf("error creating chart-renderer: %w", err)
	}

	err = ca.ApplyFromEmbeddedFS(ctx, charts.InternalChart, filepath.Join("internal", "crds-storage"), "", "crds-storage")
	if err != nil {
		return fmt.Errorf("error applying crds-storage chart: %w", err)
	}

	log.Info("applied duros-storage crd")

	if err := options.controllerSwitches.Completed().AddToManager(ctx, mgr); err != nil {
		return fmt.Errorf("could not add controllers to manager: %w", err)
	}
	log.Info("added controllers to manager")

	if err := mgr.AddReadyzCheck("informer-sync", ghealth.NewCacheSyncHealthz(mgr.GetCache())); err != nil {
		return fmt.Errorf("could not add ready check for informers: %w", err)
	}
	log.Info("added readyzcheck")

	if err := mgr.AddHealthzCheck("ping", healthz.Ping); err != nil {
		return fmt.Errorf("could not add health check to manager: %w", err)
	}
	log.Info("added healthzcheck")

	if err := healthcheck.AddToManager(ctx, mgr); err != nil {
		return fmt.Errorf("could not add health check controller to manager: %w", err)
	}

	if err := mgr.Start(ctx); err != nil {
		return fmt.Errorf("error running manager: %w", err)
	}

	return nil
}
