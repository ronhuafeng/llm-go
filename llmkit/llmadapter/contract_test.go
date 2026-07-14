package llmadapter_test

import (
	"context"

	"github.com/ronhuafeng/llmkit-go/llmadapter"
)

// downstreamCaller proves the mirrored consumer contract without access to
// package internals.
type downstreamCaller struct{}

func (downstreamCaller) Call(context.Context, llmadapter.Request) (llmadapter.Response, error) {
	return llmadapter.Response{}, nil
}

var _ llmadapter.Caller = downstreamCaller{}
