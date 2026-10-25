package codexsdk

import (
    "encoding/json"
    "strings"
    "testing"

    "github.com/ronhuafeng/llm-go/codexsdk/protocolv2"
)

// Transport ID acceptance must equal the independently generated upstream
// RequestId union at the current fixed baseline, apart from stable diagnostics.
func TestTransportRequestIDFollowsGeneratedUnion(t *testing.T) {
    for _, token := range []string{
        `"go-sdk-1"`, `""`, `0`, `-0`, `-9223372036854775808`, `9223372036854775807`,
        `null`, `true`, `[]`, `["x"]`, `{}`, `1.5`, `1e0`, `9223372036854775808`, `-9223372036854775809`,
    } {
        t.Run(token, func(t *testing.T) {
            var generated protocolv2.RequestId
            generatedErr := json.Unmarshal([]byte(token), &generated)
            wantValid := generatedErr == nil && generated.IsValid()
            if token == "null" { wantValid = false }
            for _, envelope := range []string{
                `{"id":`+token+`,"method":"turn/start"}`,
                `{"id":`+token+`,"result":{}}`,
                `{"id":`+token+`,"error":{"code":-32603,"message":"fail"}}`,
            } {
                err := validateJSONRPCEnvelope([]byte(envelope))
                if gotValid := err == nil; gotValid != wantValid {
                    t.Fatalf("id=%s valid=%t want=%t: %v", token, gotValid, wantValid, err)
                }
                if token == "null" && (err == nil || !strings.Contains(err.Error(), "null is not allowed")) {
                    t.Fatalf("ID-null diagnostic changed: %v", err)
                }
            }
        })
    }
}
