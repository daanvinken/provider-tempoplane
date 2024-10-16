package utils

import (
	"github.com/pkg/errors"
	"go.temporal.io/sdk/client"
	"os"
)

// NewTemporalClient initializes a Temporal client with configuration from environment variables.
func NewTemporalClient() (client.Client, error) {
	tc, err := client.Dial(client.Options{
		HostPort:  os.Getenv("TEMPORAL_ADDRESS"),
		Namespace: os.Getenv("TEMPORAL_NS"),
	})
	if err != nil {
		return nil, errors.Wrap(err, "failed to create Temporal client")
	}
	return tc, nil
}
