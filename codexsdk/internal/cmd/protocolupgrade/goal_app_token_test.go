package main

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type goalTokenTransport func(*http.Request) (*http.Response, error)

func (f goalTokenTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func goalAppFixture(t *testing.T) *goalAppCredentials {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("PROTOCOL_SYNC_APP_CLIENT_ID", "Iv1.fixture")
	t.Setenv("PROTOCOL_SYNC_APP_INSTALLATION_ID", "123")
	t.Setenv("PROTOCOL_SYNC_APP_PRIVATE_KEY", string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})))
	app, err := newGoalAppCredentials("owner/repo", io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	return app
}

func TestGoalAppTokenRenewsAtPublicationAfterEarlierExpiry(t *testing.T) {
	app := goalAppFixture(t)
	now := time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC)
	app.now = func() time.Time { return now }
	issued, revoked := 0, 0
	app.client.Transport = goalTokenTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Scheme != "https" || r.URL.Host != "api.github.com" {
			t.Fatal("authentication escaped GitHub")
		}
		status, body := http.StatusNoContent, ""
		switch r.Method + " " + r.URL.Path {
		case "POST /app/installations/123/access_tokens":
			parts := strings.Split(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "), ".")
			if len(parts) != 3 {
				t.Fatal("missing App JWT")
			}
			signature, err := base64.RawURLEncoding.DecodeString(parts[2])
			digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
			if err != nil || rsa.VerifyPKCS1v15(&app.key.PublicKey, crypto.SHA256, digest[:], signature) != nil {
				t.Fatal("App JWT signature invalid")
			}
			claims, _ := base64.RawURLEncoding.DecodeString(parts[1])
			var payload struct {
				Iat, Exp int64
				Iss      string
			}
			if json.Unmarshal(claims, &payload) != nil || payload.Iss != "Iv1.fixture" || payload.Iat != now.Add(-time.Minute).Unix() || payload.Exp <= now.Unix() || payload.Exp > now.Add(10*time.Minute).Unix() {
				t.Fatal("JWT did not use current publication time and App identity")
			}
			var grant struct {
				Repositories []string
				Permissions  map[string]string
			}
			if json.NewDecoder(r.Body).Decode(&grant) != nil || len(grant.Repositories) != 1 || grant.Repositories[0] != "repo" || len(grant.Permissions) != 1 || grant.Permissions["contents"] != "write" {
				t.Fatal("publication grant was not restricted to the bound repository push")
			}
			issued++
			status = http.StatusCreated
			body = fmt.Sprintf(`{"token":"fresh-%d","expires_at":%q}`, issued, now.Add(time.Hour).Format(time.RFC3339))
		case "DELETE /installation/token":
			if r.Context().Err() != nil || r.Header.Get("Authorization") != fmt.Sprintf("Bearer fresh-%d", issued) {
				t.Fatal("cleanup lost its own token or inherited cancellation")
			}
			revoked++
		default:
			t.Fatalf("unexpected authentication request: %s %s", r.Method, r.URL.Path)
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})
	for i := 1; i <= 2; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		token, release, err := app.token(ctx)
		if err != nil || token != fmt.Sprintf("fresh-%d", i) {
			t.Fatalf("publication %d did not get a fresh token: %v", i, err)
		}
		cancel()
		release()
		now = now.Add(80 * time.Minute)
	}
	if issued != 2 || revoked != 2 {
		t.Fatalf("issued=%d revoked=%d", issued, revoked)
	}
}

func TestGoalAppTokenFailureDoesNotDiscloseAuthentication(t *testing.T) {
	app := goalAppFixture(t)
	for _, body := range []string{`{"token":"private-response-token","expires_at":"2000-01-01T00:00:00Z"}`, `private-response-token`} {
		app.client.Transport = goalTokenTransport(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusCreated, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
		})
		token, release, err := app.token(context.Background())
		if err == nil || token != "" || release != nil || strings.Contains(err.Error(), "private-response-token") {
			t.Fatal("invalid token accepted or response disclosed")
		}
	}
	var output bytes.Buffer
	app.stderr = &output
	app.client.Transport = goalTokenTransport(func(r *http.Request) (*http.Response, error) {
		status, body := http.StatusForbidden, "private-response-token"
		if r.Method == http.MethodPost {
			status = http.StatusCreated
			body = fmt.Sprintf(`{"token":"private-response-token","expires_at":%q}`, time.Now().Add(time.Hour).Format(time.RFC3339))
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})
	_, release, err := app.token(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	release()
	if output.Len() == 0 || strings.Contains(output.String(), "private-response-token") {
		t.Fatal("revocation warning missing or disclosed a token")
	}
}
