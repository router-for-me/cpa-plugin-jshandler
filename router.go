package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/dop251/goja"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
	log "github.com/sirupsen/logrus"
)

var _ pluginapi.ModelRouter = (*jsHandlerPlugin)(nil)

// RouteModel implements pluginapi.ModelRouter by invoking the optional
// on_route_model(ctx) hook across the configured scripts in order.
// The first script whose returned ctx has handled=true wins; scripts that do
// not export the hook, or return without setting handled, pass the request to
// the next script. Scripts run before provider resolution, so ctx.to_format
// is always empty here.
func (p *jsHandlerPlugin) RouteModel(ctx context.Context, req pluginapi.ModelRouteRequest) (pluginapi.ModelRouteResponse, error) {
	return p.routeModel(ctx, req, "")
}

func (p *jsHandlerPlugin) routeModel(_ context.Context, req pluginapi.ModelRouteRequest, hostCallbackID string) (pluginapi.ModelRouteResponse, error) {
	resp := pluginapi.ModelRouteResponse{}
	for _, scriptPath := range p.allScriptPaths() {
		scriptPath = strings.TrimSpace(scriptPath)
		if scriptPath == "" {
			continue
		}
		decision, handled, errJS := p.applyJSRouteHook(scriptPath, req, hostCallbackID)
		if errJS != nil {
			log.Warnf("failed to execute JS model router [%s]: %v", scriptPath, errJS)
			continue
		}
		if handled {
			return decision, nil
		}
	}
	return resp, nil
}

func (p *jsHandlerPlugin) applyJSRouteHook(scriptPath string, req pluginapi.ModelRouteRequest, hostCallbackID string) (pluginapi.ModelRouteResponse, bool, error) {
	program, err := getJSProgram(scriptPath)
	if err != nil {
		return pluginapi.ModelRouteResponse{}, false, err
	}

	engine := newJSEngine(newHostJSConsoleLogger(hostCallbackID))
	if errRun := engine.runProgram(program, p.cfg.Timeout); errRun != nil {
		return pluginapi.ModelRouteResponse{}, false, errRun
	}

	jsCtx := map[string]any{
		"id":                  generateRequestID(),
		"body":                string(req.Body),
		"headers":             headerToAnyMap(req.Headers),
		"query":               headerToAnyMap(http.Header(req.Query)),
		"url":                 "",
		"model":               req.RequestedModel,
		"requested_model":     req.RequestedModel,
		"protocol":            req.SourceFormat,
		"source_format":       req.SourceFormat,
		"to_format":           "",
		"sourceFormat":        req.SourceFormat,
		"toFormat":            "",
		"stream":              req.Stream,
		"available_providers": append([]string(nil), req.AvailableProviders...),
		"metadata":            req.Metadata,
	}

	jsVal, errCall := engine.callFunction("on_route_model", p.cfg.Timeout, jsCtx)
	if errCall != nil {
		if errors.Is(errCall, ErrFunctionNotFound) {
			return pluginapi.ModelRouteResponse{}, false, nil
		}
		return pluginapi.ModelRouteResponse{}, false, fmt.Errorf("on_route_model failed for %s: %w", scriptPath, errCall)
	}

	if jsVal == nil || goja.IsUndefined(jsVal) || goja.IsNull(jsVal) {
		return pluginapi.ModelRouteResponse{}, false, nil
	}

	objMap, ok := jsVal.Export().(map[string]any)
	if !ok || objMap == nil {
		return pluginapi.ModelRouteResponse{}, false, nil
	}

	if handled, _ := objMap["handled"].(bool); !handled {
		return pluginapi.ModelRouteResponse{}, false, nil
	}

	resp := pluginapi.ModelRouteResponse{
		Handled:     true,
		TargetKind:  pluginapi.ModelRouteTargetKind(stringFromAnyMap(objMap, "target_kind", "targetKind")),
		Target:      stringFromAnyMap(objMap, "target"),
		TargetModel: stringFromAnyMap(objMap, "target_model", "targetModel"),
		Reason:      stringFromAnyMap(objMap, "reason"),
	}
	switch resp.TargetKind {
	case pluginapi.ModelRouteTargetProvider, pluginapi.ModelRouteTargetExecutor, pluginapi.ModelRouteTargetSelf:
	default:
		return pluginapi.ModelRouteResponse{}, false, fmt.Errorf("on_route_model returned unsupported target_kind %q", resp.TargetKind)
	}
	return resp, true, nil
}

func stringFromAnyMap(m map[string]any, keys ...string) string {
	for _, key := range keys {
		if s, ok := m[key].(string); ok {
			return strings.TrimSpace(s)
		}
	}
	return ""
}
