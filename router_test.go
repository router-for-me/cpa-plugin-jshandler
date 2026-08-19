package main

import (
	"context"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
)

func writeRouteScript(t *testing.T, name, script string) string {
	t.Helper()
	scriptPath := filepath.Join(t.TempDir(), name)
	if errWrite := os.WriteFile(scriptPath, []byte(script), 0600); errWrite != nil {
		t.Fatalf("os.WriteFile() error = %v", errWrite)
	}
	return scriptPath
}

func TestApplyJSRouteHookMapsProviderDecision(t *testing.T) {
	scriptPath := writeRouteScript(t, "route.js", `
function on_route_model(ctx) {
    if (ctx.model !== "deepseek-flash") {
        throw new Error("unexpected model: " + ctx.model);
    }
    if (ctx.requested_model !== "deepseek-flash") {
        throw new Error("unexpected requested_model: " + ctx.requested_model);
    }
    if (ctx.source_format !== "openai" || ctx.sourceFormat !== "openai" || ctx.protocol !== "openai") {
        throw new Error("unexpected source format fields");
    }
    if (ctx.to_format !== "" || ctx.toFormat !== "") {
        throw new Error("to_format must be empty before routing");
    }
    if (ctx.stream !== true) {
        throw new Error("stream flag not propagated");
    }
    if (ctx.headers["X-Stainless-Timeout"] !== "60") {
        throw new Error("header not visible: " + JSON.stringify(ctx.headers));
    }
    if (ctx.query["alt"] !== "sse") {
        throw new Error("query not visible: " + JSON.stringify(ctx.query));
    }
    var body = JSON.parse(ctx.body);
    if (body.max_tokens !== 2112) {
        throw new Error("body not visible");
    }
    if (ctx.available_providers.indexOf("codex") === -1) {
        throw new Error("available_providers not visible");
    }
    ctx.handled = true;
    ctx.target_kind = "provider";
    ctx.target = "codex";
    ctx.target_model = "deepseek-flash-resp";
    ctx.reason = "json_schema reroute";
    return ctx;
}
`)

	plugin := &jsHandlerPlugin{cfg: defaultJSHandlerConfig()}
	resp, handled, errApply := plugin.applyJSRouteHook(scriptPath, pluginapi.ModelRouteRequest{
		SourceFormat:       "openai",
		RequestedModel:     "deepseek-flash",
		Stream:             true,
		Headers:            http.Header{"X-Stainless-Timeout": []string{"60"}},
		Query:              url.Values{"alt": []string{"sse"}},
		Body:               []byte(`{"model":"deepseek-flash","max_tokens":2112}`),
		AvailableProviders: []string{"codex", "openai-compatibility"},
	}, "")
	if errApply != nil {
		t.Fatalf("applyJSRouteHook() error = %v", errApply)
	}
	if !handled {
		t.Fatal("applyJSRouteHook() handled = false, want true")
	}
	if !resp.Handled {
		t.Fatal("response Handled = false, want true")
	}
	if resp.TargetKind != pluginapi.ModelRouteTargetProvider {
		t.Fatalf("TargetKind = %q, want %q", resp.TargetKind, pluginapi.ModelRouteTargetProvider)
	}
	if resp.Target != "codex" {
		t.Fatalf("Target = %q, want codex", resp.Target)
	}
	if resp.TargetModel != "deepseek-flash-resp" {
		t.Fatalf("TargetModel = %q, want deepseek-flash-resp", resp.TargetModel)
	}
	if resp.Reason != "json_schema reroute" {
		t.Fatalf("Reason = %q, want json_schema reroute", resp.Reason)
	}
}

func TestApplyJSRouteHookMissingFunctionPassesThrough(t *testing.T) {
	scriptPath := writeRouteScript(t, "noop.js", `
function on_before_request(ctx) { return ctx; }
`)

	plugin := &jsHandlerPlugin{cfg: defaultJSHandlerConfig()}
	_, handled, errApply := plugin.applyJSRouteHook(scriptPath, pluginapi.ModelRouteRequest{RequestedModel: "gpt-test"}, "")
	if errApply != nil {
		t.Fatalf("applyJSRouteHook() error = %v", errApply)
	}
	if handled {
		t.Fatal("applyJSRouteHook() handled = true, want false for script without on_route_model")
	}
}

func TestApplyJSRouteHookUnhandledCtxPassesThrough(t *testing.T) {
	scriptPath := writeRouteScript(t, "pass.js", `
function on_route_model(ctx) { return ctx; }
`)

	plugin := &jsHandlerPlugin{cfg: defaultJSHandlerConfig()}
	_, handled, errApply := plugin.applyJSRouteHook(scriptPath, pluginapi.ModelRouteRequest{RequestedModel: "gpt-test"}, "")
	if errApply != nil {
		t.Fatalf("applyJSRouteHook() error = %v", errApply)
	}
	if handled {
		t.Fatal("applyJSRouteHook() handled = true, want false when script leaves handled unset")
	}
}

func TestApplyJSRouteHookRejectsUnknownTargetKind(t *testing.T) {
	scriptPath := writeRouteScript(t, "bad.js", `
function on_route_model(ctx) {
    ctx.handled = true;
    ctx.target_kind = "upstream";
    return ctx;
}
`)

	plugin := &jsHandlerPlugin{cfg: defaultJSHandlerConfig()}
	_, handled, errApply := plugin.applyJSRouteHook(scriptPath, pluginapi.ModelRouteRequest{RequestedModel: "gpt-test"}, "")
	if errApply == nil {
		t.Fatal("applyJSRouteHook() error = nil, want unsupported target_kind error")
	}
	if handled {
		t.Fatal("applyJSRouteHook() handled = true, want false on invalid target_kind")
	}
}

func TestRouteModelFirstHandledScriptWins(t *testing.T) {
	first := writeRouteScript(t, "first.js", `
function on_route_model(ctx) {
    ctx.handled = true;
    ctx.target_kind = "provider";
    ctx.target = "codex";
    ctx.target_model = "deepseek-flash-resp";
    return ctx;
}
`)
	second := writeRouteScript(t, "second.js", `
function on_route_model(ctx) {
    throw new Error("second script must not run after a handled decision");
}
`)

	plugin := &jsHandlerPlugin{cfg: defaultJSHandlerConfig()}
	plugin.cfg.ScriptPaths = []string{first, second}
	resp, errRoute := plugin.RouteModel(context.Background(), pluginapi.ModelRouteRequest{RequestedModel: "deepseek-flash"})
	if errRoute != nil {
		t.Fatalf("RouteModel() error = %v", errRoute)
	}
	if !resp.Handled || resp.Target != "codex" || resp.TargetModel != "deepseek-flash-resp" {
		t.Fatalf("RouteModel() = %+v, want handled provider decision from the first script", resp)
	}
}

func TestRouteModelSkipsUnhandledAndScriptErrors(t *testing.T) {
	first := writeRouteScript(t, "pass.js", `
function on_route_model(ctx) { return ctx; }
`)
	broken := writeRouteScript(t, "broken.js", `
function on_route_model(ctx) { throw new Error("boom"); }
`)
	last := writeRouteScript(t, "last.js", `
function on_route_model(ctx) {
    return { handled: true, target_kind: "provider", target: "codex", targetModel: "deepseek-pro-resp" };
}
`)

	plugin := &jsHandlerPlugin{cfg: defaultJSHandlerConfig()}
	plugin.cfg.ScriptPaths = []string{first, broken, last}
	resp, errRoute := plugin.RouteModel(context.Background(), pluginapi.ModelRouteRequest{RequestedModel: "deepseek-pro"})
	if errRoute != nil {
		t.Fatalf("RouteModel() error = %v", errRoute)
	}
	if !resp.Handled || resp.TargetModel != "deepseek-pro-resp" {
		t.Fatalf("RouteModel() = %+v, want decision from the last script", resp)
	}
}

func TestRouteModelWithoutScriptsIsNotHandled(t *testing.T) {
	plugin := &jsHandlerPlugin{cfg: defaultJSHandlerConfig()}
	resp, errRoute := plugin.RouteModel(context.Background(), pluginapi.ModelRouteRequest{RequestedModel: "gpt-test"})
	if errRoute != nil {
		t.Fatalf("RouteModel() error = %v", errRoute)
	}
	if resp.Handled {
		t.Fatal("RouteModel() Handled = true, want false without scripts")
	}
}
