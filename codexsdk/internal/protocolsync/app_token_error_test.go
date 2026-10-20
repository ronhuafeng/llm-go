package protocolsync

import "testing"

func TestAppTokenErrorRendersOnlyClosedCategoryAndHTTPStatus(t *testing.T) {
	for _, tc := range []struct {
		failure AppTokenFailure
		status  int
		want    string
	}{
		{AppTokenSigningFailed, 0, "signing_failed"},
		{AppTokenRequestInvalid, 0, "request_invalid"},
		{AppTokenTransportFailed, 0, "transport_failed"},
		{AppTokenHTTPRejected, 403, "http_rejected (HTTP 403)"},
		{AppTokenResponseMalformed, 201, "response_malformed (HTTP 201)"},
		{AppTokenMissing, 201, "token_missing (HTTP 201)"},
		{AppTokenExpiryInsufficient, 201, "expiry_insufficient (HTTP 201)"},
		{AppTokenFailure(255), 0, "unknown"},
		{AppTokenFailure(255), 503, "unknown (HTTP 503)"},
		{AppTokenHTTPRejected, -123456789, "http_rejected"},
		{AppTokenHTTPRejected, 99, "http_rejected"},
		{AppTokenHTTPRejected, 600, "http_rejected"},
		{AppTokenHTTPRejected, 123456789, "http_rejected"},
	} {
		err := &AppTokenError{Failure: tc.failure, HTTPStatus: tc.status}
		if got := err.Error(); got != "App token preparation failed: "+tc.want {
			t.Fatalf("unbounded or incorrect token diagnostic: %q", got)
		}
	}
}
