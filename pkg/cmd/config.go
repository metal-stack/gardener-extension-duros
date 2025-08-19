package cmd

import (
	"errors"
	"os"

	healthcheckconfig "github.com/gardener/gardener/extensions/pkg/apis/config/v1alpha1"
	configapi "github.com/metal-stack/gardener-extension-duros/pkg/apis/config"
	"github.com/metal-stack/gardener-extension-duros/pkg/apis/config/v1alpha1"

	"github.com/spf13/pflag"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
)

var (
	scheme  *runtime.Scheme
	decoder runtime.Decoder
)

func init() {
	scheme = runtime.NewScheme()
	utilruntime.Must(configapi.AddToScheme(scheme))
	utilruntime.Must(v1alpha1.AddToScheme(scheme))

	decoder = serializer.NewCodecFactory(scheme).UniversalDecoder()
}

// DurosOptions holds options related to the duros service.
type DurosOptions struct {
	ConfigLocation string
	config         *DurosServiceConfig
}

// AddFlags implements Flagger.AddFlags.
func (o *DurosOptions) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&o.ConfigLocation, "config", "", "Path to duros service configuration")
}

// Complete implements Completer.Complete.
func (o *DurosOptions) Complete() error {
	if o.ConfigLocation == "" {
		return errors.New("config location is not set")
	}
	data, err := os.ReadFile(o.ConfigLocation)
	if err != nil {
		return err
	}

	config := configapi.ControllerConfiguration{}
	_, _, err = decoder.Decode(data, nil, &config)
	if err != nil {
		return err
	}

	// if errs := validation.ValidateConfiguration(&config); len(errs) > 0 {
	// 	return errs.ToAggregate()
	// }

	o.config = &DurosServiceConfig{
		config: config,
	}

	return nil
}

// Completed returns the decoded DurosServiceConfig instance. Only call this if `Complete` was successful.
func (o *DurosOptions) Completed() *DurosServiceConfig {
	return o.config
}

// RegistryServiceConfig contains configuration information about the registry service.
type DurosServiceConfig struct {
	config configapi.ControllerConfiguration
}

// Apply applies the RegistryOptions to the passed ControllerOptions instance.
func (c *DurosServiceConfig) Apply(config *configapi.ControllerConfiguration) {
	*config = c.config
}

// ApplyHealthCheckConfig applies the HealthCheckConfig.
func (c *DurosServiceConfig) ApplyHealthCheckConfig(config *healthcheckconfig.HealthCheckConfig) {
	if c.config.HealthCheckConfig != nil {
		*config = *c.config.HealthCheckConfig
	}
}
