package protocolsync

import "fmt"

// AppTokenFailure describes only an observed token-preparation failure. An HTTP
// rejection does not establish that an installed App grant is missing.
type AppTokenFailure uint8

const (
	AppTokenSigningFailed AppTokenFailure = iota + 1
	AppTokenRequestInvalid
	AppTokenTransportFailed
	AppTokenHTTPRejected
	AppTokenResponseMalformed
	AppTokenMissing
	AppTokenExpiryInsufficient
)

// AppTokenError retains only a closed failure category and numeric HTTP status.
// Never retain or wrap response bodies, headers, credentials or transport errors.
type AppTokenError struct {
	Failure    AppTokenFailure
	HTTPStatus int // zero when no HTTP response was received
}

func (e *AppTokenError) Error() string {
	category := "unknown"
	switch e.Failure {
	case AppTokenSigningFailed:
		category = "signing_failed"
	case AppTokenRequestInvalid:
		category = "request_invalid"
	case AppTokenTransportFailed:
		category = "transport_failed"
	case AppTokenHTTPRejected:
		category = "http_rejected"
	case AppTokenResponseMalformed:
		category = "response_malformed"
	case AppTokenMissing:
		category = "token_missing"
	case AppTokenExpiryInsufficient:
		category = "expiry_insufficient"
	}
	if e.HTTPStatus >= 100 && e.HTTPStatus <= 599 {
		return fmt.Sprintf("App token preparation failed: %s (HTTP %d)", category, e.HTTPStatus)
	}
	return "App token preparation failed: " + category
}
