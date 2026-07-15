# nREPL

`lgs repl` can expose the same serialized let-go evaluator used by the terminal REPL over nREPL:

```bash
lgs repl --nrepl 0
# nREPL listening on 127.0.0.1:43817
```

Port `0` asks the operating system for an available port. By default the server writes that resolved port to `.nrepl-port` for editor discovery and removes the file on clean shutdown. Disable this with `--nrepl-port-file=false`, or select a fixed port with `--nrepl 7888`.

## Security

The default bind address is `127.0.0.1`. There is currently no authentication or TLS. Exposing nREPL allows clients to evaluate arbitrary let-go code with the permissions of the `lgs` process.

A non-loopback listener must be requested explicitly:

```bash
lgs repl --nrepl 7888 --nrepl-bind 0.0.0.0
```

`lgs` prints a security warning when configured this way. Use host firewalling and a trusted network.

## Editor connections

Read the port from `.nrepl-port`, then connect to `127.0.0.1` from CIDER, Calva, Conjure, or another nREPL client. The pinned let-go protocol implementation has raw-protocol integration coverage; full CIDER compatibility certification is not part of Phase 3.

Supported operations:

- `clone`
- `close`
- `describe`
- `eval`
- `interrupt`
- `load-file`
- `stdin`
- `ls-sessions`

Each cloned session tracks its current namespace. Vars, synth definitions, scheduling, controls, and audio state are process-global. Evaluation and `load-file` compile top-level forms sequentially, so namespace changes affect subsequent forms and persist in that session.

`stdout` and `stderr` are returned as nREPL messages. Per-evaluation output is bounded; overflow returns the structured `nrepl-output-overflow` status instead of consuming unbounded memory. Connections and sessions are also bounded.

## Evaluation and audio safety

nREPL delegates to `internal/lisp.Runtime`'s evaluation mutex, the same control-side boundary used by the terminal REPL. It cannot invoke the audio callback directly or bypass the scheduler. A slow client may delay its own network response but does not perform network I/O while holding the evaluation mutex.

Already acknowledged music operations are not rolled back by an interrupt. let-go v1.11.1 does not expose evaluator cancellation, so `interrupt` is currently cooperative and reports `session-idle`; it never stops audio. The `stdin` operation is accepted for client compatibility, but runtime forms do not currently consume nREPL stdin.

## Pinned let-go capability notes

let-go v1.11.1 includes `pkg/nrepl`, but its server requires an internal compiler context that `api.LetGo` does not expose. It also shares one compiler context among sessions and does not provide configurable binding or resource bounds. Therefore `lgs` uses a small protocol adapter around its embedded `Runtime` rather than starting that package directly.
