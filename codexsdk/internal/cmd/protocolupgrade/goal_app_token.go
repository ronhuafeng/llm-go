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
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// A goal can outlive an installation token. Only the trusted publisher holds
// this App key; each push obtains a fresh, repository-scoped token and revokes
// it afterward. There is no refresh loop or token cache in the model process.
type goalAppCredentials struct {
	clientID, installationID, repository string
	key                                  *rsa.PrivateKey
	client                               *http.Client
	now                                  func() time.Time
	stderr                               io.Writer
}

func newGoalAppCredentials(repository string, stderr io.Writer) (*goalAppCredentials, error) {
	return newRepositoryAppCredentials("PROTOCOL_SYNC", repository, stderr)
}

func newRepositoryAppCredentials(prefix, repository string, stderr io.Writer) (*goalAppCredentials, error) {
	owner, name, ok := strings.Cut(repository, "/")
	clientID := os.Getenv(prefix + "_APP_CLIENT_ID")
	installationID := os.Getenv(prefix + "_APP_INSTALLATION_ID")
	id, err := strconv.ParseInt(installationID, 10, 64)
	if !ok || owner == "" || name == "" || strings.Contains(name, "/") || clientID == "" || err != nil || id <= 0 {
		return nil, errors.New("publication App binding unavailable")
	}
	block, _ := pem.Decode([]byte(strings.ReplaceAll(os.Getenv(prefix+"_APP_PRIVATE_KEY"), `\n`, "\n")))
	if block == nil {
		return nil, errors.New("publication App key unavailable")
	}
	key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		parsed, parseErr := x509.ParsePKCS8PrivateKey(block.Bytes)
		if parseErr != nil {
			return nil, errors.New("publication App key invalid")
		}
		key, ok = parsed.(*rsa.PrivateKey)
		if !ok {
			return nil, errors.New("publication App key must be RSA")
		}
	}
	return &goalAppCredentials{clientID: clientID, installationID: installationID, repository: name,
		key: key, now: time.Now, stderr: stderr, client: &http.Client{Timeout: 30 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

func (a *goalAppCredentials) token(ctx context.Context) (string, func(), error) {
	return a.tokenWithPermissions(ctx, map[string]string{"contents": "write"})
}

func (a *goalAppCredentials) tokenWithPermissions(ctx context.Context, permissions map[string]string) (string, func(), error) {
	now := a.now()
	claims, _ := json.Marshal(map[string]any{"iat": now.Add(-time.Minute).Unix(), "exp": now.Add(5 * time.Minute).Unix(), "iss": a.clientID})
	unsigned := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`)) + "." + base64.RawURLEncoding.EncodeToString(claims)
	digest := sha256.Sum256([]byte(unsigned))
	signature, err := rsa.SignPKCS1v15(rand.Reader, a.key, crypto.SHA256, digest[:])
	if err != nil {
		return "", nil, errors.New("publication App signing failed")
	}
	jwt := unsigned + "." + base64.RawURLEncoding.EncodeToString(signature)
	body, _ := json.Marshal(map[string]any{"repositories": []string{a.repository}, "permissions": permissions})
	response, err := a.request(ctx, http.MethodPost, "/app/installations/"+a.installationID+"/access_tokens", jwt, body)
	if err != nil {
		return "", nil, err
	}
	defer response.Body.Close()
	var value struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	if response.StatusCode != http.StatusCreated || json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&value) != nil || value.Token == "" || !value.ExpiresAt.After(now.Add(time.Minute)) {
		return "", nil, errors.New("fresh publication token unavailable")
	}
	return value.Token, func() {
		// Cleanup still runs after cancellation; the token never enters candidate
		// state or logs. A failed revoke retains GitHub's one-hour expiry.
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		response, err := a.request(cleanup, http.MethodDelete, "/installation/token", value.Token, nil)
		if response != nil {
			response.Body.Close()
		}
		if err != nil || response.StatusCode != http.StatusNoContent {
			fmt.Fprintln(a.stderr, "publication token revocation unconfirmed; token will expire")
		}
	}, nil
}

func (a *goalAppCredentials) request(ctx context.Context, method, path, token string, body []byte) (*http.Response, error) {
	request, err := http.NewRequestWithContext(ctx, method, "https://api.github.com"+path, bytes.NewReader(body))
	if err != nil {
		return nil, errors.New("publication authentication request invalid")
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-GitHub-Api-Version", "2026-03-10")
	response, err := a.client.Do(request)
	if err != nil {
		return nil, errors.New("publication authentication request failed")
	}
	return response, nil
}
