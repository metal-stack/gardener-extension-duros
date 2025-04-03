package cmd

import (
	controllercmd "github.com/gardener/gardener/extensions/pkg/controller/cmd"
	extensionsheartbeatcontroller "github.com/gardener/gardener/extensions/pkg/controller/heartbeat"

	duros "github.com/metal-stack/gardener-extension-duros/pkg/controller/duros"
)

// ControllerSwitchOptions are the controllercmd.SwitchOptions for the provider controllers.
func ControllerSwitchOptions() *controllercmd.SwitchOptions {
	return controllercmd.NewSwitchOptions(
		controllercmd.Switch(duros.ControllerName, duros.AddToManager),
		controllercmd.Switch(extensionsheartbeatcontroller.ControllerName, extensionsheartbeatcontroller.AddToManager),
	)
}
