package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginabi"
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

func TestEmptyInstallationReconfiguresWhenScriptsAdded(t *testing.T) {
	dir := t.TempDir()
	for _, step := range []struct {
		name    string
		config  string
		builtin bool
		want    bool
	}{
		{"empty", "", false, false},
		{"explicit-missing-path", "script_paths: [repair-later.js]", false, true},
		{"builtin-added", "", true, true},
		{"builtin-removed", "", false, false},
	} {
		t.Run(step.name, func(t *testing.T) {
			scripts := filepath.Join(dir, "scripts")
			if err := os.MkdirAll(scripts, 0700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(scripts, "handler.js")
			if step.builtin {
				if err := os.WriteFile(path, []byte("function on_after_stream_response(ctx) { return ctx; }"), 0600); err != nil {
					t.Fatal(err)
				}
			} else if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
			req, err := json.Marshal(abiLifecycleRequest{ConfigYAML: []byte(step.config), PluginDir: dir})
			if err != nil {
				t.Fatal(err)
			}
			raw, err := handleJSHandlerRegister(req)
			if err != nil {
				t.Fatal(err)
			}
			var envelope abiEnvelope
			if err := json.Unmarshal(raw, &envelope); err != nil {
				t.Fatal(err)
			}
			var reg abiRegistration
			if err := json.Unmarshal(envelope.Result, &reg); err != nil {
				t.Fatal(err)
			}
			if reg.SchemaVersion != pluginabi.SchemaVersion {
				t.Fatal("script payload compatibility changed")
			}
			if !reg.Capabilities.RequestInterceptor || reg.Capabilities.ResponseInterceptor != step.want || reg.Capabilities.StreamChunkInterceptor != step.want {
				t.Fatalf("capabilities = %+v; want request=true, response/stream=%v", reg.Capabilities, step.want)
			}
		})
	}
}
