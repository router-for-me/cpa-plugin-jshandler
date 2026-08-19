# JS Handler Plugin

A CLIProxyAPI plugin that executes external JavaScript scripts to intercept and modify requests, responses, and streaming chunks using the Goja VM engine.

Repository: https://github.com/router-for-me/cpa-plugin-jshandler

## Acknowledgements

This plugin's code comes entirely from [router-for-me/CLIProxyAPI#3768](https://github.com/router-for-me/CLIProxyAPI/pull/3768) by [easyatm](https://github.com/easyatm). Respect and thanks for the original contribution.

## License

This project is licensed under the [MIT License](LICENSE).

Copyright (c) 2026.6-present easyatm

Copyright (c) 2026.6-present Router-For.ME

## Features

- **Request Interception** (`on_before_request`, `on_after_auth_request`): Modify request payloads and headers before and after credential selection.
- **Response Interception** (`on_after_nonstream_response`): Modify non-streaming response bodies and headers.
- **Stream Chunk Interception** (`on_after_stream_response`): Modify individual streaming chunks with read-only `history_chunks` context.
- **Model Routing** (`on_route_model`): Decide the execution target (built-in provider + model, or a plugin executor) before model-to-provider resolution and credential selection.
- **Hot Reload**: Scripts are automatically reloaded when modified on disk.
- **Execution Timeout**: Configurable timeout prevents infinite loops.
- **Graceful Degradation**: Original data is preserved on JS execution errors.

## Configuration

```yaml
plugins:
  enabled: true
  dir: "plugins-dir"
  configs:
    jshandler:
      enabled: true
      script_paths:
        - /path/to/custom_handler.js
        - ./relative_handler.js
      timeout: 1s
```

### Fields

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `enabled` | boolean | `true` | Enable or disable the plugin |
| `script_paths` | array | `[]` | JS script file paths (absolute or relative to plugin directory) |
| `timeout` | string | `1s` | Execution timeout per JS hook call |

## JS Script API

Scripts can export these global functions:

### `on_before_request(ctx)`

Called before credential selection. At this point the target upstream protocol is not selected yet.

**ctx structure:**
```javascript
{
    "id": "request-id",
    "body": "...",        // Request body string
    "headers": {},        // Request headers
    "url": "",
    "model": "gpt-4",
    "protocol": "openai",
    "source_format": "openai",
    "sourceFormat": "openai",
    "to_format": "",
    "toFormat": ""
}
```

### `on_after_auth_request(ctx)`

Called after credential selection and before request translation, request normalization, and built-in payload configuration.

**ctx structure:**
```javascript
{
    "id": "request-id",
    "body": "...",             // Request body string
    "headers": {},             // Request headers
    "url": "",
    "model": "gpt-4",
    "protocol": "openai",      // Same as source_format
    "source_format": "openai",
    "sourceFormat": "openai",
    "to_format": "codex",
    "toFormat": "codex"
}
```

### `on_after_nonstream_response(ctx)`

Called after a non-streaming response is received from upstream.

**ctx structure (non-streaming):**
```javascript
{
    "id": "request-id",
    "body": "...",        // Full response body
    "req": { "body": "...", "headers": {}, "url": "" },
    "protocol": "openai",
    "headers": {},
    "chunk": null,
    "history_chunks": null
}
```

### `on_after_stream_response(ctx)`

Called after each streaming response chunk is received from upstream.

**ctx structure:**
```javascript
{
    "id": "request-id",
    "body": null,
    "req": { "body": "...", "headers": {}, "url": "" },
    "protocol": "openai",
    "headers": {},
    "chunk": "...",              // Current writable chunk
    "history_chunks": ["..."]    // Read-only frozen array
}
```

### `on_route_model(ctx)`

Called before model-to-provider resolution and credential selection. Use it to route
requests based on any combination of model name, headers, query parameters, and body
fields. `ctx.model` is the client-requested model name, and `ctx.to_format` is always
empty because the upstream protocol has not been selected yet.

**ctx structure:**
```javascript
{
    "id": "request-id",
    "body": "...",                  // Raw client request body string
    "headers": {},                  // Inbound request headers
    "query": {},                    // Inbound query parameters
    "url": "",
    "model": "deepseek-flash",      // Client-requested model (alias)
    "requested_model": "deepseek-flash",
    "protocol": "openai",           // Same as source_format
    "source_format": "openai",      // Inbound protocol
    "sourceFormat": "openai",
    "to_format": "",                // Always empty at routing time
    "toFormat": "",
    "stream": false,
    "available_providers": ["codex", "openai-compatibility"],
    "metadata": {}
}
```

To make a routing decision, set these fields on the returned `ctx`:

| Field | Type | Description |
|-------|------|-------------|
| `handled` | boolean | `true` to claim the routing decision |
| `target_kind` | string | `"provider"` (built-in provider), `"executor"` or `"self"` (plugin executors) |
| `target` | string | Provider key for `"provider"` routes (must have credentials registered); plugin id for executor routes |
| `target_model` | string | Model name (or configured alias) used on the provider path; empty keeps the requested model |
| `reason` | string | Optional diagnostic reason recorded by the host |

Scripts are evaluated in `script_paths` order and the **first script that returns
`handled: true` wins**; scripts that do not export `on_route_model`, throw, or leave
`handled` unset pass the request to the next script. When no script handles the
request, CLIProxyAPI falls back to its normal model routing. Routing scripts are
hot-reloaded like all other scripts.

**Example:** route chat-completions requests that carry a `json_schema` response
format to a Responses-API provider, where the schema is supported natively:

```javascript
function on_route_model(ctx) {
  var body;
  try { body = JSON.parse(ctx.body || '{}'); } catch (e) { return ctx; }
  if (!body || typeof body !== 'object') return ctx;

  var model = String(ctx.model || '');
  if (model.indexOf('deepseek') === -1) return ctx;
  if (/-resp$/.test(model)) return ctx; // already on the Responses-API alias

  var hasSchema =
    (body.response_format && body.response_format.type === 'json_schema') ||
    (body.text && body.text.format && body.text.format.type === 'json_schema');
  if (!hasSchema) return ctx;

  if ((ctx.available_providers || []).indexOf('codex') === -1) return ctx;

  ctx.handled = true;
  ctx.target_kind = 'provider';
  ctx.target = 'codex';
  ctx.target_model = model + '-resp';
  ctx.reason = 'json_schema reroute';
  return ctx;
}
```

### Return Value

Return the modified `ctx` object, or a plain string to replace the body/chunk.

## Built-in Scripts

The `scripts/` directory contains built-in scripts loaded automatically:

- `copilot_handler.js`: Fixes tool-call `finish_reason` for GitHub Copilot compatibility.

## Building

```bash
make build
```

The Makefile chooses the plugin extension from the target platform:

| GOOS | Output |
|------|--------|
| `linux` / `freebsd` | `jshandler.so` |
| `darwin` | `jshandler.dylib` |
| `windows` | `jshandler.dll` |

You can override the target and output directory:

```bash
make build GOOS=darwin GOARCH=arm64 BUILD_DIR=/path/to/plugins/darwin/arm64
```

Release builds can inject the runtime plugin version:

```bash
make build VERSION=0.1.0
```

## Plugin Store Release Assets

The GitHub Actions workflow builds plugin-store-compatible archives for:

| GOOS | GOARCH | Runner |
|------|--------|--------|
| `linux` | `amd64` | `ubuntu-24.04` |
| `linux` | `arm64` | `ubuntu-24.04-arm` |
| `freebsd` | `amd64` | `go-cross/cgo-actions` on `ubuntu-24.04` |
| `darwin` | `amd64` | `macos-15-intel` |
| `darwin` | `arm64` | `macos-15` |
| `windows` | `amd64` | `windows-2025` |
| `windows` | `arm64` | `go-cross/cgo-actions` on `ubuntu-24.04` |

FreeBSD release builds are limited to `amd64` because Go does not support `-buildmode=c-shared` for `freebsd/arm64`.

Tag pushes such as `v0.1.0` publish release assets named:

```text
jshandler_0.1.0_linux_amd64.zip
jshandler_0.1.0_linux_arm64.zip
jshandler_0.1.0_freebsd_amd64.zip
jshandler_0.1.0_darwin_amd64.zip
jshandler_0.1.0_darwin_arm64.zip
jshandler_0.1.0_windows_amd64.zip
jshandler_0.1.0_windows_arm64.zip
checksums.txt
```

Each archive contains the platform dynamic library at the zip root, using the filename expected by the CLIProxyAPI plugin store: `jshandler.so`, `jshandler.dylib`, or `jshandler.dll`.
