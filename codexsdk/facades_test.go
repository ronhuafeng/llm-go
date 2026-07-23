package codexsdk

import (
	"strings"
	"testing"

	"github.com/ronhuafeng/llm-go/codexsdk/protocolv2"
)

func TestDecodeProtocolResponseAcceptsAdditionalMembersRecursively(t *testing.T) {
	result := map[string]any{
		"futureResponseMember": true,
		"rateLimits": map[string]any{
			"futureSnapshotMember": true,
		},
		"rateLimitsByLimitId": map[string]any{
			"codex": map[string]any{
				"futureMappedSnapshotMember": true,
				"secondary": map[string]any{
					"futureWindowMember": true,
					"usedPercent":        7,
				},
			},
		},
	}
	var response protocolv2.GetAccountRateLimitsResponse
	if err := decodeProtocolResponse(protocolv2.MethodAccountRateLimitsRead, result, &response); err != nil {
		t.Fatal(err)
	}
	if response.RateLimitsByLimitID == nil || response.RateLimitsByLimitID.Value == nil {
		t.Fatalf("decoded rateLimitsByLimitId = %#v", response.RateLimitsByLimitID)
	}
	snapshot := (*response.RateLimitsByLimitID.Value)["codex"]
	if snapshot.Secondary == nil || snapshot.Secondary.Value == nil || snapshot.Secondary.Value.UsedPercent != 7 {
		t.Fatalf("decoded mapped snapshot = %#v", snapshot)
	}
}

func TestDecodeProtocolResponseKeepsKnownMembersExact(t *testing.T) {
	result := map[string]any{
		"futureResponseMember": true,
		"rateLimits":           "not-an-object",
	}
	var response protocolv2.GetAccountRateLimitsResponse
	err := decodeProtocolResponse(protocolv2.MethodAccountRateLimitsRead, result, &response)
	if err == nil || !strings.Contains(err.Error(), "RateLimitSnapshot: expected object") {
		t.Fatalf("decodeProtocolResponse error = %v, want known-member type error", err)
	}
}
