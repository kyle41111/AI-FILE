package main

import (
	"bytes"
	"testing"
)

func TestEnvelopeRoundTrip(t *testing.T) {
	testEnvelopeRoundTrip(t, envelopeAlgorithm)
}

func TestLegacyGCMEnvelopeRoundTrip(t *testing.T) {
	testEnvelopeRoundTrip(t, envelopeAlgorithmGCM)
}

func testEnvelopeRoundTrip(t *testing.T, algorithm string) {
	t.Helper()
	key, err := deriveTransportKey("unit-test-shared-key")
	if err != nil {
		t.Fatalf("deriveTransportKey failed: %v", err)
	}
	plaintext := []byte("mythic message bytes")
	env, err := encryptEnvelopeWithAlgorithm(directionRequest, "test-channel", "req-1", key, plaintext, algorithm)
	if err != nil {
		t.Fatalf("encryptEnvelope failed: %v", err)
	}
	jsonl, err := envelopeJSONL(env)
	if err != nil {
		t.Fatalf("envelopeJSONL failed: %v", err)
	}
	parsed, err := parseJSONLEnvelopes(jsonl, 1<<20)
	if err != nil {
		t.Fatalf("parseJSONLEnvelopes failed: %v", err)
	}
	if len(parsed) != 1 {
		t.Fatalf("expected 1 envelope, got %d", len(parsed))
	}
	got, err := decryptEnvelope(parsed[0], key)
	if err != nil {
		t.Fatalf("decryptEnvelope failed: %v", err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Fatalf("round trip mismatch: got %q want %q", got, plaintext)
	}
}

func TestFilenameHelpers(t *testing.T) {
	cfg := instanceConfig{
		ChannelID:      "red team/one",
		RequestPrefix:  "mythic_to_server_",
		ResponsePrefix: "mythic_to_agent_",
	}
	if !isRequestFilename(cfg, requestFilename(cfg, "req/1")) {
		t.Fatal("request filename did not match request filter")
	}
	if isRequestFilename(cfg, responseFilename(cfg, "req/1")) {
		t.Fatal("response filename matched request filter")
	}
}
