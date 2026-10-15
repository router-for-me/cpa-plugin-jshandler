package main

import (
	"encoding/json"
	"testing"
)

func TestABIRegistrationUsesHostStreamCapabilityField(t *testing.T) {
	raw, errMarshal := abiOKEnvelope(abiRegistration{
		Capabilities: abiCapabilities{
			RequestInterceptor:     true,
			ResponseInterceptor:    true,
			StreamChunkInterceptor: true,
		},
	})
	if errMarshal != nil {
		t.Fatalf("abiOKEnvelope() error = %v", errMarshal)
	}

	var envelope abiEnvelope
	if errUnmarshal := json.Unmarshal(raw, &envelope); errUnmarshal != nil {
		t.Fatalf("json.Unmarshal(envelope) error = %v", errUnmarshal)
	}
	var result struct {
		Capabilities map[string]bool `json:"capabilities"`
	}
	if errUnmarshal := json.Unmarshal(envelope.Result, &result); errUnmarshal != nil {
		t.Fatalf("json.Unmarshal(result) error = %v", errUnmarshal)
	}
	if !result.Capabilities["response_stream_interceptor"] {
		t.Fatalf("response_stream_interceptor capability was not advertised: %v", result.Capabilities)
	}
	if _, exists := result.Capabilities["stream_chunk_interceptor"]; exists {
		t.Fatalf("legacy stream_chunk_interceptor field should not be advertised: %v", result.Capabilities)
	}
}

func TestABIRegistrationPinsSchemaVersion(t *testing.T) {
	t.Cleanup(func() {
		jsHandlerABIState.Lock()
		jsHandlerABIState.plugin = nil
		jsHandlerABIState.Unlock()
	})

	raw, errRegister := handleJSHandlerRegister([]byte(`{"config_yaml":""}`))
	if errRegister != nil {
		t.Fatalf("handleJSHandlerRegister() error = %v", errRegister)
	}
	var envelope abiEnvelope
	if errUnmarshal := json.Unmarshal(raw, &envelope); errUnmarshal != nil {
		t.Fatalf("json.Unmarshal(envelope) error = %v", errUnmarshal)
	}
	var result struct {
		SchemaVersion uint32 `json:"schema_version"`
	}
	if errUnmarshal := json.Unmarshal(envelope.Result, &result); errUnmarshal != nil {
		t.Fatalf("json.Unmarshal(result) error = %v", errUnmarshal)
	}
	// Newer schema versions drop per-chunk request bodies and history chunks used by stream scripts.
	if result.SchemaVersion != 1 {
		t.Fatalf("registration schema_version = %d, want 1", result.SchemaVersion)
	}
}
