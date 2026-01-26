package slack

import (
	"bytes"
	"fmt"
	"net/http"
	"strconv"
	"testing"
	"time"
)

func TestVerifySlackRequest(t *testing.T) {
	signingSecret := "test_secret"
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	body := []byte(`{"type":"url_verification","challenge":"test"}`)

	// Compute valid signature
	baseString := fmt.Sprintf("%s:%s:%s", slackSignatureVersion, timestamp, string(body))
	validSig := computeSignature(baseString, signingSecret)

	tests := []struct {
		name      string
		timestamp string
		signature string
		wantErr   bool
	}{
		{
			name:      "valid request",
			timestamp: timestamp,
			signature: validSig,
			wantErr:   false,
		},
		{
			name:      "missing timestamp",
			timestamp: "",
			signature: validSig,
			wantErr:   true,
		},
		{
			name:      "missing signature",
			timestamp: timestamp,
			signature: "",
			wantErr:   true,
		},
		{
			name:      "invalid signature",
			timestamp: timestamp,
			signature: "v0=invalid",
			wantErr:   true,
		},
		{
			name:      "old timestamp",
			timestamp: strconv.FormatInt(time.Now().Unix()-400, 10),
			signature: validSig,
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequest("POST", "/test", bytes.NewReader(body))
			if err != nil {
				t.Fatalf("Failed to create test request: %v", err)
			}
			req.Header.Set(slackRequestTimestampHeader, tt.timestamp)
			req.Header.Set(slackSignatureHeader, tt.signature)

			err = VerifySlackRequest(req, signingSecret)
			if (err != nil) != tt.wantErr {
				t.Errorf("VerifySlackRequest() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestComputeSignature(t *testing.T) {
	baseString := "v0:123456:test_body"
	secret := "test_secret"

	sig1 := computeSignature(baseString, secret)
	sig2 := computeSignature(baseString, secret)

	// Same input should produce same signature
	if sig1 != sig2 {
		t.Errorf("Signatures don't match: %s != %s", sig1, sig2)
	}

	// Different input should produce different signature
	sig3 := computeSignature("different", secret)
	if sig1 == sig3 {
		t.Error("Different inputs produced same signature")
	}
}
