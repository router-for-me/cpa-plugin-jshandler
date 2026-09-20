# Empty-installation streaming overhead

## Scope and existing work

Investigation date: 2026-09-21 (UTC+8). Host: CLIProxyAPI v7.2.159,
Linux amd64, two vCPUs. Original plugin: jshandler v1.0.1.
The enabled plugin had no configured script paths and no built-in JS files.

- Related issue: https://github.com/router-for-me/cpa-plugin-jshandler/issues/2
- Related proposal: https://github.com/router-for-me/cpa-plugin-jshandler/pull/7
- Host payload issue: https://github.com/router-for-me/CLIProxyAPI/issues/4876
- History issue: https://github.com/router-for-me/CLIProxyAPI/issues/5451

PR #7 addresses script payload size through schema v5. This change instead
removes unused response subscriptions for an empty installation. It retains
the existing schema and script semantics, including request bodies and history.
It does not claim to fix the overhead of installations that actually run scripts.

## Mechanism

v1.0.1 registers every interceptor, even with no scripts. The host clones and
encodes the payload before the dynamic library copies it through C.GoBytes and
decodes the full request. Only then does the interceptor discover there are no
scripts and return. On a legacy stream subscription that repeats large request
bodies/history for each chunk, doing nothing still costs substantial CPU.

The fix keeps the required request capability but omits response/stream hooks
when no scripts are selected. At least one capability is necessary: an initial
zero-capability candidate was rejected by the host and is not counted as a
successful enabled-plugin comparison below. Explicit configured paths remain
subscribed even when a file is temporarily absent. Newly added builtin scripts
in a previously empty installation need reconfiguration/reload; this is documented.

## Production evidence (natural traffic, not a controlled benchmark)

`perf record -F 49 -e cpu-clock -p <pid> -- sleep <seconds>` was used. One
initial 20-second frame-pointer sample also showed JSHandlerPluginCall stacks.
Only aggregate module/stack data were retained; raw profiles were removed.

| State | Window | Samples | Plugin share | Approx. process CPU from sample count |
| --- | --- | --- | --- | --- |
| Original enabled | 10 seconds | 912 | 77.41% jshandler-v1.0.1.so | 186% |
| Disabled, after in-flight work drained | 10 seconds | 60 | no jshandler samples | 12% |
| Repaired, loaded AND registered, enabled | 20 seconds | 88 | 2 samples, 2.27% | 9% |

100% means one CPU core. CPU-clock sample estimates are approximate. Traffic,
request lengths, concurrent requests and time of observation differed; these
figures are not a controlled throughput benchmark or a promised speedup.
The repaired plugin still performs request hooks, so zero CPU is not claimed.

During the repaired observation interval, real successful streaming requests
continued (including inputs of roughly 350k tokens). Subsequent Astra examples
with 322k/323k input tokens completed in 14/15 seconds with 247/324 output tokens.
These are examples, not matched before/after latency comparisons.

## Verification

- `go test ./...`, `go test -race ./...`, `go vet ./...` passed locally.
- Linux amd64 tests and CGO shared-library build passed in Go 1.26.
- Registration regression exercises empty installation, an explicit missing
  script, builtin addition and builtin removal across reconfiguration.
- Existing request/response/stream script tests continue to pass.
- Production logs explicitly confirmed the repaired version loaded and registered.
- Management configuration enabled=true alone was not treated as acceptance.
- API health remained available and natural requests completed successfully.

Deployment lessons: the host's `store.version` pin must match the installed
library filename. A candidate initially missed that pin and was not loaded;
its low CPU observation was excluded. Library replacement used a protected
backup and a controlled restart. No paid diagnostic requests were generated.
