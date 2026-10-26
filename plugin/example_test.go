// SPDX-License-Identifier: Apache-2.0

package plugin_test

import (
	"context"
	"os"

	"github.com/arvel-dev/arvel-sdk/plugin"
	"github.com/arvel-dev/arvel-sdk/protocol"
)

// exampleProvider is the smallest provider: it cannot run a turn.
type exampleProvider struct{}

func (exampleProvider) Initialize(context.Context, protocol.InitMeta) (plugin.Features, error) {
	return plugin.Features{}, nil
}
func (exampleProvider) Status(context.Context) (protocol.Status, error) {
	return protocol.Status{State: protocol.StateReady}, nil
}
func (exampleProvider) Models(context.Context, bool) (protocol.ModelsResult, error) {
	return protocol.ModelsResult{}, nil
}
func (exampleProvider) Usage(context.Context) (protocol.Usage, error) {
	return protocol.Usage{Source: protocol.UsageSourceUnknown, Status: protocol.UsageStatusOK}, nil
}
func (exampleProvider) Action(context.Context, string) (protocol.ActionResult, error) {
	return protocol.ActionResult{Kind: protocol.ActionKindDone}, nil
}
func (exampleProvider) NewSession(context.Context, plugin.SessionParams) (plugin.Session, error) {
	return nil, protocol.NewError(protocol.ErrInternal, "not implemented", nil)
}
func (exampleProvider) LoadSession(context.Context, plugin.LoadParams) (plugin.Session, error) {
	return nil, protocol.NewError(protocol.ErrInternal, "not implemented", nil)
}
func (exampleProvider) Shutdown(context.Context) error { return nil }

// ExampleRun shows the whole main function of a provider plugin.
func ExampleRun() {
	if err := plugin.Run(context.Background(), exampleProvider{}, plugin.WithVersion("arvel-plugin-example", "0.1.0")); err != nil {
		os.Exit(1)
	}
}

type exampleRouter struct{}

func (exampleRouter) Initialize(context.Context, protocol.InitMeta) (plugin.RouterFeatures, error) {
	return plugin.RouterFeatures{}, nil
}
func (exampleRouter) Status(context.Context) (protocol.Status, error) {
	return protocol.Status{State: protocol.StateReady}, nil
}
func (exampleRouter) Shutdown(context.Context) error { return nil }
func (exampleRouter) Effort(_ context.Context, p protocol.RouteEffortParams) (protocol.RouteEffortResult, error) {
	return protocol.RouteEffortResult{Effort: p.Levels[0], Confidence: 0.5}, nil
}

// ExampleRunRouter shows the whole main function of a router plugin.
func ExampleRunRouter() {
	if err := plugin.RunRouter(context.Background(), exampleRouter{}, plugin.WithVersion("arvel-plugin-example-router", "0.1.0")); err != nil {
		os.Exit(1)
	}
}
