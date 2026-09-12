package generatedproof

import (
	"strings"
	"testing"

	"github.com/ronhuafeng/llm-go/codexsdk/internal/protocolgen"
)

func TestGenerateSDKSurfaceRendersExactFacade(t *testing.T) {
	manifest := protocolgen.Manifest{
		Entries: []protocolgen.ManifestEntry{{
			Direction:             "client_to_server",
			Kind:                  "request",
			Method:                "thread/start",
			FacadeTarget:          "Threads().Start",
			FacadeStatus:          facadeStatusGenerated,
			ParamsOrPayloadSchema: "ThreadStartParams",
			ResponseType:          "ThreadStartResponse",
			Family:                "thread",
			Stability:             "stable",
		}},
	}
	got, err := GenerateSDKSurface(manifest, []byte("\tMethodThreadStart = \"thread/start\"\n"), []byte("type ThreadStartParams struct{}\ntype ThreadStartResponse struct{}\n"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(got)
	for _, want := range []string{
		"func (c *Client) Threads() Threads",
		"func (f Threads) Start(ctx context.Context, params protocolv2.ThreadStartParams) (protocolv2.ThreadStartResponse, error)",
		"protocolv2.MethodThreadStart",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("generated surface missing %q:\n%s", want, text)
		}
	}
}

func TestGenerateSDKSurfaceRejectsMissingGeneratedType(t *testing.T) {
	manifest := protocolgen.Manifest{
		Entries: []protocolgen.ManifestEntry{{
			Direction:             "client_to_server",
			Kind:                  "request",
			Method:                "thread/start",
			FacadeTarget:          "Threads().Start",
			FacadeStatus:          facadeStatusGenerated,
			ParamsOrPayloadSchema: "ThreadStartParams",
			ResponseType:          "ThreadStartResponse",
			Family:                "thread",
			Stability:             "stable",
		}},
	}
	_, err := GenerateSDKSurface(manifest, []byte("\tMethodThreadStart = \"thread/start\"\n"), []byte("type ThreadStartParams struct{}\n"))
	if err == nil || !strings.Contains(err.Error(), "response type ThreadStartResponse") {
		t.Fatalf("missing response type error = %v", err)
	}
}
