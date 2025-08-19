//go:generate sh -c "bash $GARDENER_HACK_DIR/generate-controller-registration.sh duros . v0.0.1 ../../example/controller-registration.yaml Extension:duros"

// Package chart enables go:generate support for generating the correct controller registration.
package chart
