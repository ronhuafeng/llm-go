package llmschema_test

import (
	"fmt"

	"github.com/ronhuafeng/llm-go/llmkit/llmschema"
)

type exampleVerdict struct {
	Status string `json:"status"`
	Score  int    `json:"score"`
}

func ExampleContract() {
	contract, err := llmschema.Compile[exampleVerdict]()
	if err != nil {
		panic(err)
	}
	value, err := contract.Decode([]byte(`{"status":"pass","score":2}`))
	if err != nil {
		panic(err)
	}
	fmt.Println(value.Status, value.Score)
	// Output: pass 2
}
