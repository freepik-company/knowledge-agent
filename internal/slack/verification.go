package slack

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	slackRequestTimestampHeader = "X-Slack-Request-Timestamp"
	slackSignatureHeader        = "X-Slack-Signature"
	slackSignatureVersion       = "v0"
)

// VerifySlackRequest verifies the authenticity of a Slack request
func VerifySlackRequest(r *http.Request, signingSecret string) error {
	// Get timestamp from headers
	timestamp := r.Header.Get(slackRequestTimestampHeader)
	if timestamp == "" {
		return fmt.Errorf("missing %s header", slackRequestTimestampHeader)
	}

	// Check if timestamp is recent (within 5 minutes) to prevent replay attacks
	ts, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid timestamp: %w", err)
	}

	now := time.Now().Unix()
	if abs(now-ts) > 300 {
		return fmt.Errorf("timestamp too old or too far in future")
	}

	// Get signature from headers
	receivedSignature := r.Header.Get(slackSignatureHeader)
	if receivedSignature == "" {
		return fmt.Errorf("missing %s header", slackSignatureHeader)
	}

	// Read body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return fmt.Errorf("failed to read body: %w", err)
	}

	// Compute expected signature
	baseString := fmt.Sprintf("%s:%s:%s", slackSignatureVersion, timestamp, string(body))
	expectedSignature := computeSignature(baseString, signingSecret)

	// Compare signatures (constant time comparison)
	if !hmac.Equal([]byte(receivedSignature), []byte(expectedSignature)) {
		return fmt.Errorf("signature verification failed")
	}

	// Restore body for subsequent reads
	r.Body = io.NopCloser(strings.NewReader(string(body)))

	return nil
}

// computeSignature computes HMAC-SHA256 signature
func computeSignature(baseString, secret string) string {
	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte(baseString))
	signature := hex.EncodeToString(h.Sum(nil))
	return fmt.Sprintf("%s=%s", slackSignatureVersion, signature)
}

// abs returns absolute value of an int64
func abs(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}
