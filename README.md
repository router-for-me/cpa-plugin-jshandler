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
- **Request Termination** (`on_before_request`, `on_after_auth_request`): Block a request and return a custom response without calling upstream.
- **Response Interception** (`on_after_nonstream_response`): Modify non-streaming response bodies and headers.
- **Stream Chunk Interception** (`on_after_stream_response`): Modify individual streaming chunks with read-only `history_chunks` context.
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

### Return Value

Return the modified `ctx` object, or a plain string to replace the body/chunk.

### Request Termination

`on_before_request` and `on_after_auth_request` can block a request by returning an object (or the modified `ctx`) with `terminate: true`. CLIProxyAPI skips the upstream call and returns the provided response to the client. Remaining scripts and interceptors are not executed.

```javascript
function on_before_request(ctx) {
    if (ctx.model === "blocked-model") {
        return {
            terminate: true,
            status_code: 403,
            response_headers: { "Content-Type": "application/json" },
            response_body: JSON.stringify({ error: { message: "model is not allowed" } })
        };
    }
    return ctx;
}
```

| Field | Type | Description |
|-------|------|-------------|
| `terminate` | boolean | Must be `true` to block the request |
| `status_code` | integer | Response HTTP status; missing, non-integer, or outside `200`-`599` defaults to `403` |
| `response_headers` | object | Response headers; `Content-Type` defaults to `application/json` |
| `response_body` | string | Response body; use `JSON.stringify` for objects |

Request termination requires CLIProxyAPI v7.2.103 or later. Older versions ignore these fields and forward the request.

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
| `freebsd` | `amd64` | `clang` with a FreeBSD 14.4 sysroot on `ubuntu-24.04` |
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
