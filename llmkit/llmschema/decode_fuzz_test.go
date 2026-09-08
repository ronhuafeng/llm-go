package llmschema

import (
	"reflect"
	"testing"
)

type fuzzDecodeOutput struct {
	Name   string         `json:"name"`
	Note   *string        `json:"note,omitempty"`
	Scores []int          `json:"scores"`
	Labels map[string]int `json:"labels"`
}

func FuzzContractDecode(f *testing.F) {
	contract, err := Compile[fuzzDecodeOutput]()
	if err != nil {
		f.Fatal(err)
	}
	for _, seed := range [][]byte{
		[]byte(`{"name":"ok","note":null,"scores":[1],"labels":{"a":2}}`),
		[]byte(`{"note":null,"scores":[1],"labels":{"a":2}}`),
		[]byte(`{"name":false,"scores":[1],"labels":{"a":2}}`),
		[]byte(`{"name":"ok","scores":["bad"],"labels":{"a":2}}`),
		[]byte(`true false`),
		[]byte(``),
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		value, err := contract.Decode(data)
		if err != nil {
			if !reflect.DeepEqual(value, fuzzDecodeOutput{}) {
				t.Fatalf("failed decode returned value %#v", value)
			}
			return
		}
		again, againErr := contract.Decode(data)
		if againErr != nil {
			t.Fatalf("successful decode failed on replay: %v", againErr)
		}
		if !reflect.DeepEqual(value, again) {
			t.Fatalf("successful decode is not stable: %#v vs %#v", value, again)
		}
	})
}
