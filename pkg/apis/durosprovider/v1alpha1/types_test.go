package v1alpha1

import (
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
)

var log = logr.New(logr.Discard().GetSink())

func TestConfig(t *testing.T) {

	tt := []struct {
		desc       string
		customData *DurosProviderConfig
		valid      bool
	}{}

	for _, tc := range tt {
		t.Run(tc.desc, func(t *testing.T) {
			println(tc.desc)
			isConfigValid := tc.customData.IsValid(log)
			assert.Equal(t, tc.valid, isConfigValid)
		})
	}
}
