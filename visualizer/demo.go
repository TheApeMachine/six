package visualizer

import (
	"context"

	"github.com/theapemachine/six/pkg/console"
	"github.com/theapemachine/six/pkg/provider"
	"github.com/theapemachine/six/test/integration"
)

/*
RunAliceDemo boots the full system with the given dataset and blocks until ctx
is cancelled. All tokenization, LSM insertion, fold telemetry, and Cortex events
flow through the real system components automatically. The caller owns dataset
construction so this package stays free of embed/cmd import cycles.
*/
func RunAliceDemo(ctx context.Context, dataset provider.Dataset) error {
	console.Info("Starting Alice demo")

	helper := integration.NewIntegrationHelper(
		context.Background(),
		dataset,
	)
	defer helper.Teardown()

	return nil
}
