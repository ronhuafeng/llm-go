package codexsdk_test

import (
	"context"

	"github.com/ronhuafeng/codexsdk-go/codexsdk"
	"github.com/ronhuafeng/codexsdk-go/codexsdk/protocolv2"
)

// These consumer-owned interfaces are compile-only examples for the public
// concrete boundary implemented in issue #62. They deliberately describe only
// the methods each consumer needs.
type runStarter interface {
	Start(context.Context, codexsdk.StartThreadRunRequest) (codexsdk.StartedThreadRun, error)
}

type modelLister interface {
	List(context.Context, protocolv2.ModelListParams) (protocolv2.ModelListResponse, error)
}

func compileRunConsumer(ctx context.Context, runner runStarter, req codexsdk.StartThreadRunRequest) error {
	_, err := runner.Start(ctx, req)
	return err
}

func compileModelConsumer(ctx context.Context, models modelLister) error {
	_, err := models.List(ctx, protocolv2.ModelListParams{})
	return err
}

var (
	_ *codexsdk.Client = (*codexsdk.Client)(nil)
	_ runStarter       = (codexsdk.ThreadRunner)(nil)
)

func compileConcreteFacadeType(models codexsdk.Models) {
	var _ modelLister = models
}

func compileConcreteRootLifecycle(root *codexsdk.Client) error {
	return root.Close()
}

func compileConcreteRootFacade(ctx context.Context, root *codexsdk.Client) error {
	return compileModelConsumer(ctx, root.Models())
}
