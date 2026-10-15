package main

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

func TestApplyJSBeforeRequestUsesReturnedCtxBody(t *testing.T) {
	scriptPath := filepath.Join(t.TempDir(), "before.js")
	script := `
function on_before_request(ctx) {
    var req = JSON.parse(ctx.body);
    req.messages[0].content = req.messages[0].content.replace("sensitive_word", "safe_word");
    ctx.body = JSON.stringify(req);
    ctx.headers["X-Plugin"] = "updated";
    return ctx;
}
`
	if errWrite := os.WriteFile(scriptPath, []byte(script), 0600); errWrite != nil {
		t.Fatalf("os.WriteFile() error = %v", errWrite)
	}

	plugin := &jsHandlerPlugin{cfg: defaultJSHandlerConfig()}
	headers := http.Header{"X-Plugin": []string{"original"}}
	processed, _, termination, errApply := plugin.applyJSRequestHook(
		scriptPath,
		"on_before_request",
		[]byte(`{"messages":[{"role":"user","content":"contains sensitive_word"}]}`),
		"gpt-test",
		"openai",
		"",
		headers,
		"",
	)
	if errApply != nil {
		t.Fatalf("applyJSRequestHook() error = %v", errApply)
	}
	if termination != nil {
		t.Fatalf("applyJSRequestHook() termination = %#v, want nil", termination)
	}
	if body := string(processed); !strings.Contains(body, "safe_word") || strings.Contains(body, "sensitive_word") {
		t.Fatalf("processed body = %q, want sensitive word rewritten", body)
	}
	if got := headers.Get("X-Plugin"); got != "updated" {
		t.Fatalf("header X-Plugin = %q, want updated", got)
	}
}

func TestApplyJSAfterAuthRequestReceivesFormats(t *testing.T) {
	scriptPath := filepath.Join(t.TempDir(), "after_auth.js")
	script := `
function on_after_auth_request(ctx) {
    if (ctx.source_format !== "openai" || ctx.to_format !== "codex") {
        throw new Error("unexpected formats: " + ctx.source_format + " -> " + ctx.to_format);
    }
    if (ctx.sourceFormat !== "openai" || ctx.toFormat !== "codex") {
        throw new Error("unexpected camel formats: " + ctx.sourceFormat + " -> " + ctx.toFormat);
    }
    var req = JSON.parse(ctx.body);
    req.after_auth = ctx.source_format + "_to_" + ctx.to_format;
    ctx.headers["X-Protocol"] = req.after_auth;
    ctx.body = JSON.stringify(req);
    return ctx;
}
`
	if errWrite := os.WriteFile(scriptPath, []byte(script), 0600); errWrite != nil {
		t.Fatalf("os.WriteFile() error = %v", errWrite)
	}

	plugin := &jsHandlerPlugin{cfg: defaultJSHandlerConfig()}
	headers := http.Header{}
	processed, _, _, errApply := plugin.applyJSRequestHook(
		scriptPath,
		"on_after_auth_request",
		[]byte(`{"model":"gpt-test"}`),
		"gpt-test",
		"openai",
		"codex",
		headers,
		"",
	)
	if errApply != nil {
		t.Fatalf("applyJSRequestHook() error = %v", errApply)
	}
	if body := string(processed); !strings.Contains(body, `"after_auth":"openai_to_codex"`) {
		t.Fatalf("processed body = %q, want after_auth marker", body)
	}
	if got := headers.Get("X-Protocol"); got != "openai_to_codex" {
		t.Fatalf("header X-Protocol = %q, want openai_to_codex", got)
	}
}

func TestApplyJSRequestHookReturnsTermination(t *testing.T) {
	scriptPath := filepath.Join(t.TempDir(), "terminate.js")
	script := `
function on_before_request(ctx) {
    ctx.terminate = true;
    ctx.status_code = 429;
    ctx.response_headers = { "Content-Type": "application/json", "Retry-After": "30" };
    ctx.response_body = JSON.stringify({ error: { message: "blocked by jshandler" } });
    return ctx;
}
`
	if errWrite := os.WriteFile(scriptPath, []byte(script), 0600); errWrite != nil {
		t.Fatalf("os.WriteFile() error = %v", errWrite)
	}

	plugin := &jsHandlerPlugin{cfg: defaultJSHandlerConfig()}
	_, _, termination, errApply := plugin.applyJSRequestHook(
		scriptPath,
		"on_before_request",
		[]byte(`{"model":"gpt-test"}`),
		"gpt-test",
		"openai",
		"",
		http.Header{},
		"",
	)
	if errApply != nil {
		t.Fatalf("applyJSRequestHook() error = %v", errApply)
	}
	if termination == nil {
		t.Fatal("applyJSRequestHook() termination = nil, want termination")
	}
	if termination.statusCode != http.StatusTooManyRequests {
		t.Fatalf("termination status = %d, want %d", termination.statusCode, http.StatusTooManyRequests)
	}
	if got := termination.headers.Get("Content-Type"); got != "application/json" {
		t.Fatalf("termination Content-Type = %q, want application/json", got)
	}
	if got := termination.headers.Get("Retry-After"); got != "30" {
		t.Fatalf("termination Retry-After = %q, want 30", got)
	}
	if got := string(termination.body); got != `{"error":{"message":"blocked by jshandler"}}` {
		t.Fatalf("termination body = %q", got)
	}
}

func TestApplyJSRequestHookIgnoresInvalidTerminationFields(t *testing.T) {
	tests := []struct {
		name            string
		result          string
		wantTermination bool
	}{
		{name: "non-boolean terminate", result: `{ terminate: "true", status_code: 429 }`, wantTermination: false},
		{name: "non-integer status", result: `{ terminate: true, status_code: 599.9 }`, wantTermination: true},
		{name: "string status", result: `{ terminate: true, status_code: "429" }`, wantTermination: true},
		{name: "invalid response fields", result: `{ terminate: true, response_headers: null, response_body: { error: "blocked" } }`, wantTermination: true},
	}

	plugin := &jsHandlerPlugin{cfg: defaultJSHandlerConfig()}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scriptPath := filepath.Join(t.TempDir(), "terminate.js")
			script := "function on_before_request(ctx) { return " + tt.result + "; }\n"
			if errWrite := os.WriteFile(scriptPath, []byte(script), 0600); errWrite != nil {
				t.Fatalf("os.WriteFile() error = %v", errWrite)
			}

			_, _, termination, errApply := plugin.applyJSRequestHook(scriptPath, "on_before_request", nil, "gpt-test", "openai", "", http.Header{}, "")
			if errApply != nil {
				t.Fatalf("applyJSRequestHook() error = %v", errApply)
			}
			if (termination != nil) != tt.wantTermination {
				t.Fatalf("applyJSRequestHook() termination = %#v, want termination %v", termination, tt.wantTermination)
			}
			if termination == nil {
				return
			}
			if termination.statusCode != 0 {
				t.Fatalf("termination status = %d, want 0 so the host defaults to 403", termination.statusCode)
			}
			if len(termination.headers) != 0 || termination.body != nil {
				t.Fatalf("termination headers = %v, body = %q, want empty", termination.headers, termination.body)
			}
		})
	}
}

func TestInterceptRequestStopsScriptChainOnTermination(t *testing.T) {
	dir := t.TempDir()
	firstPath := filepath.Join(dir, "first.js")
	firstScript := `
function on_after_auth_request(ctx) {
    return { terminate: true, status_code: 403, response_body: "denied" };
}
`
	secondPath := filepath.Join(dir, "second.js")
	secondScript := `
function on_after_auth_request(ctx) {
    ctx.headers["X-Second-Script"] = "ran";
    return ctx;
}
`
	if errWrite := os.WriteFile(firstPath, []byte(firstScript), 0600); errWrite != nil {
		t.Fatalf("os.WriteFile() error = %v", errWrite)
	}
	if errWrite := os.WriteFile(secondPath, []byte(secondScript), 0600); errWrite != nil {
		t.Fatalf("os.WriteFile() error = %v", errWrite)
	}

	plugin := &jsHandlerPlugin{cfg: jsHandlerConfig{
		ScriptPaths: []string{firstPath, secondPath},
		Timeout:     time.Second,
	}}
	resp, errIntercept := plugin.InterceptRequestAfterAuth(context.Background(), pluginapi.RequestInterceptRequest{
		SourceFormat: "openai",
		ToFormat:     "codex",
		Model:        "gpt-test",
		Headers:      http.Header{},
		Body:         []byte(`{"model":"gpt-test"}`),
	})
	if errIntercept != nil {
		t.Fatalf("InterceptRequestAfterAuth() error = %v", errIntercept)
	}
	if !resp.Terminate {
		t.Fatal("InterceptRequestAfterAuth() Terminate = false, want true")
	}
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("InterceptRequestAfterAuth() StatusCode = %d, want %d", resp.StatusCode, http.StatusForbidden)
	}
	if got := string(resp.ResponseBody); got != "denied" {
		t.Fatalf("InterceptRequestAfterAuth() ResponseBody = %q, want denied", got)
	}
	if got := resp.Headers.Get("X-Second-Script"); got != "" {
		t.Fatalf("second script ran after termination, X-Second-Script = %q", got)
	}
}

func TestApplyJSAfterResponseUsesFrozenNativeHistoryChunks(t *testing.T) {
	scriptPath := filepath.Join(t.TempDir(), "stream.js")
	script := `
function on_after_stream_response(ctx) {
    if (!Object.isFrozen(ctx.history_chunks)) {
        throw new Error("history_chunks is not frozen");
    }
    var original = ctx.history_chunks[0];
    try {
        ctx.history_chunks[0] = "changed";
    } catch (e) {
    }
    if (ctx.history_chunks[0] !== original) {
        throw new Error("history_chunks item was changed");
    }
    try {
        ctx.history_chunks = ["changed"];
    } catch (e) {
    }
    if (ctx.history_chunks[0] !== original) {
        throw new Error("history_chunks property was replaced");
    }
    return { chunk: ctx.chunk + "|ok" };
}
`
	if errWrite := os.WriteFile(scriptPath, []byte(script), 0600); errWrite != nil {
		t.Fatalf("os.WriteFile() error = %v", errWrite)
	}

	plugin := &jsHandlerPlugin{cfg: defaultJSHandlerConfig()}
	chunk := `data: {"choices":[{"delta":{},"finish_reason":null}]}`
	processedBody, _, changed, errApply := plugin.applyJSAfterResponse(
		scriptPath,
		"gpt-test",
		"openai",
		nil,
		nil,
		"",
		&chunk,
		http.Header{},
		true,
		[]string{`data: {"choices":[{"delta":{"tool_calls":[{"index":0}]}}]}`},
		"",
	)
	if errApply != nil {
		t.Fatalf("applyJSAfterResponse() error = %v", errApply)
	}
	if !changed {
		t.Fatal("applyJSAfterResponse() changed = false, want true")
	}
	if processedBody != chunk+"|ok" {
		t.Fatalf("applyJSAfterResponse() body = %q, want %q", processedBody, chunk+"|ok")
	}
}

func TestApplyJSAfterResponseDispatchesNonStreamHook(t *testing.T) {
	scriptPath := filepath.Join(t.TempDir(), "nonstream.js")
	script := `
function on_after_stream_response(ctx) {
    throw new Error("stream hook should not run");
}
function on_after_nonstream_response(ctx) {
    return { body: ctx.body + "|nonstream" };
}
`
	if errWrite := os.WriteFile(scriptPath, []byte(script), 0600); errWrite != nil {
		t.Fatalf("os.WriteFile() error = %v", errWrite)
	}

	plugin := &jsHandlerPlugin{cfg: defaultJSHandlerConfig()}
	processedBody, _, changed, errApply := plugin.applyJSAfterResponse(
		scriptPath,
		"gpt-test",
		"openai",
		nil,
		nil,
		`{"ok":true}`,
		nil,
		http.Header{},
		false,
		nil,
		"",
	)
	if errApply != nil {
		t.Fatalf("applyJSAfterResponse() error = %v", errApply)
	}
	if !changed {
		t.Fatal("applyJSAfterResponse() changed = false, want true")
	}
	if processedBody != `{"ok":true}|nonstream` {
		t.Fatalf("applyJSAfterResponse() body = %q", processedBody)
	}
}
