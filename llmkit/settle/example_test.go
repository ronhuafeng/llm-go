package settle_test

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ronhuafeng/llm-go/llmkit/settle"
)

type keywords struct {
	name string
	goal string
}

type draft struct {
	text string
}

type draftOp struct {
	latch latch
}

type phase int

const (
	phaseInitial phase = iota
	phaseReduce
	phasePatch
)

type latch struct {
	phase phase
	data  string
}

func (op *draftOp) Run(ctx context.Context, input keywords) (draft, error) {
	switch op.latch.phase {
	case phaseInitial:
		return draft{text: "a careful draft that is too long and still missing the required keyword"}, nil
	case phaseReduce:
		return draft{text: "small draft"}, nil
	case phasePatch:
		return draft{text: op.latch.data + " small"}, nil
	default:
		return draft{}, fmt.Errorf("unknown phase %d", op.latch.phase)
	}
}

func (op *draftOp) Validate(ctx context.Context, input keywords, result draft) (bool, error) {
	text := strings.TrimSpace(result.text)
	if text == "" {
		return false, errors.New("draft is empty")
	}
	if len(text) > 24 {
		op.latch = latch{phase: phaseReduce}
		return false, nil
	}
	if !strings.Contains(text, "ship") {
		op.latch = latch{phase: phasePatch, data: "ship"}
		return false, nil
	}
	return true, nil
}

func ExampleRun() {
	op := &draftOp{}
	input := keywords{
		name: "release.tagline",
		goal: "Write a short release tagline that includes the word ship.",
	}

	result, err := settle.Run(context.Background(), op, input, 3)
	if err != nil {
		if errors.Is(err, settle.ErrUnsettled) {
			fmt.Println("output remains unsettled at max iterations")
			return
		}
		panic(err)
	}

	fmt.Println(result.text)

	// Output:
	// ship small
}
