package codexsdk

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

func FuzzReadInboundJSONRPCFrame(f *testing.F) {
	const limit = 256
	for _, seed := range [][]byte{
		[]byte("{\"id\":1,\"result\":{}}\n"),
		[]byte("{\"method\":\"thread/started\"}\n"),
		[]byte(strings.Repeat("x", 63) + "\n"),
		[]byte(strings.Repeat("stdout_secret", 32)),
		[]byte(`{"value":"stdout_secret"}`),
		[]byte("{not-json stdout_secret transcript\n"),
		[]byte(""),
		[]byte("{\"id\":\"go-sdk-1\",\"method\":\"secret/method\",\"result\":{\"value\":\"stdout_secret\"}}\n"),
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		frame, err := readFrame(bufio.NewReader(bytes.NewReader(data)), limit)
		if err != nil {
			switch {
			case errors.Is(err, io.EOF),
				errors.Is(err, errInboundFrameTooLarge),
				errors.Is(err, errInboundFrameUnterminated):
			default:
				if !strings.Contains(err.Error(), "codexsdk:") {
					t.Fatalf("unexpected frame error class: %v", err)
				}
			}
			if bytes.Contains(data, []byte("stdout_secret")) && strings.Contains(err.Error(), "stdout_secret") {
				t.Fatalf("frame error leaked raw contents: %v", err)
			}
			if errors.Is(err, errInboundFrameTooLarge) || errors.Is(err, errInboundFrameUnterminated) {
				if !strings.Contains(err.Error(), "sha256=") {
					t.Fatalf("bounded frame error missing digest: %v", err)
				}
			}
			return
		}
		if !bytes.HasSuffix(frame, []byte("\n")) {
			t.Fatalf("successful frame missing newline: %q", frame)
		}
		if len(frame) > limit {
			t.Fatalf("successful frame exceeded limit: %d", len(frame))
		}
		if envErr := validateJSONRPCEnvelope(frame); envErr != nil {
			if !strings.Contains(envErr.Error(), "decode JSONRPCMessage") {
				t.Fatalf("envelope error missing stable class: %v", envErr)
			}
			if bytes.Contains(frame, []byte("stdout_secret")) && strings.Contains(envErr.Error(), "stdout_secret") {
				t.Fatalf("envelope error leaked raw contents: %v", envErr)
			}
		}
	})
}
