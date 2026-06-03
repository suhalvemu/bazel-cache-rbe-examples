# Bazel Cache Terminology

A reference for every term you'll encounter when working with Bazel caching and RBE — what it means, how it works, and why it matters.

---

## The Two Cache Stores

### CAS — Content Addressable Store

The storage layer that holds **file contents**.

Every file is stored and retrieved by the **SHA-256 hash of its contents**, not by its name or path. Two targets that produce identical outputs share the same CAS entry — no duplication.

```
file: greeter.o (12 KB)
sha256: a3f8c2...  ←── this is the key

CAS[a3f8c2...] = <binary contents of greeter.o>
```

- **"Content addressable"** means: the address (key) IS the content (hash). If the hash matches, the content is guaranteed to be identical.
- Bazel uses CAS to store both **inputs** (source files, toolchains) and **outputs** (compiled objects, binaries, test logs).
- In BuildBuddy, the CAS is the backend storage — when you see "Download 1.4 MiB", that's Bazel fetching an output from the CAS.

### Action Cache (AC)

The index that maps **"what I want to build" → "what the result was"**.

An action cache entry answers: *"Given these exact inputs and this exact command, what was the output?"*

```
Action Cache key:
  input hashes:  [greeter.cc → a3f8c2, version.h → d91be4]
  command hash:  [clang -O2 -c greeter.cc → 88fa12]
  ─────────────────────────────────────────────────────
  → output digest: { greeter.o → b7c3a1 }
```

When Bazel checks the cache:
1. It computes the action key from all declared inputs + the command
2. It looks up that key in the Action Cache
3. If found (cache hit): it fetches the output from the CAS by its digest
4. If not found (cache miss): it runs the action locally, then stores both the AC entry and the CAS blobs

**AC and CAS work together** — AC holds the mapping, CAS holds the actual files. Neither is useful without the other.

---

## Cache Hit and Miss

### Cache Hit

An action whose result was found in the Action Cache. Bazel skips execution entirely and downloads the output from CAS.

```
[cached] CppCompile apps/cpp-lib/greeter.o   ← cache hit, ~50ms download
[local]  CppLink apps/cpp-lib/greeter_test    ← cache miss, ~2s local execution
```

A **remote cache hit** means the result came from the shared remote cache (BuildBuddy, bazel-remote, GCS). A **local cache hit** means it came from `~/.cache/bazel-disk-cache` on your machine.

### Cache Miss

An action with no matching entry in the Action Cache. Bazel runs it locally (or on a remote worker if using RBE), then stores the result so future builds can hit it.

Cache misses happen when:
- First time a target is ever built
- Any input file changed (source, header, toolchain, flag)
- The action command changed (compiler flag, env var declared in the action)

### Cache Hit Rate

The percentage of actions served from cache in a given build.

```
Cache Hit Rate = (remote cache hits) / (total actions) × 100
```

| Rate | What it means |
|------|--------------|
| 0% | Cold build — nothing cached yet |
| 50-70% | Partial hit — some deps changed or cache is warming up |
| 80-95% | Healthy production cache |
| 95%+ | Excellent — almost nothing rebuilt |

Target: **80%+ in CI**. Below this usually means non-hermetic builds or missing `--remote_upload_local_results=true`.

---

## Transfer Metrics

### Download Throughput

The rate at which Bazel fetches cached outputs from the remote cache.

```
Download throughput: 45 MB/s
```

Higher is better. Bottlenecks are usually:
- Network bandwidth between the build machine and cache server
- Cache server disk I/O
- Number of concurrent downloads (`--remote_download_outputs`)

### Upload Throughput

The rate at which Bazel writes new action outputs to the remote cache after a cache miss.

```
Upload throughput: 12 MB/s
```

Upload only happens when `--remote_upload_local_results=true` is set. Without it, local builds never populate the shared cache.

### Cache Volume

Total data transferred in a build — uploads + downloads combined.

```
Downloaded: 48 MB   (cache hits — outputs fetched)
Uploaded:   220 MB  (cache misses — outputs stored)
```

High upload volume = many cache misses = cold or invalidated cache.
High download volume = many cache hits = healthy warm cache.

---

## Action Graph Concepts

### Action

A single unit of work Bazel can cache: compile a file, link a binary, run a test, execute a genrule.

Every action has:
- **Inputs** — declared files the action reads (srcs, hdrs, deps)
- **Command** — the exact executable + flags
- **Outputs** — files the action produces

Hermeticity requires that ALL inputs are declared. An undeclared input = non-hermetic action = cache key may not reflect actual inputs.

### Action Key / Cache Key

The SHA-256 hash of all action inputs + the command. This is what Bazel looks up in the Action Cache.

Change any input (even a whitespace change in a header) → different hash → cache miss.

### Critical Path

The longest chain of sequential actions in the build graph. The build cannot complete faster than the critical path, regardless of how many workers you add.

```
greeter.cc → greeter.o → greeter_test (linked) → greeter_test (run)
     2s           1s             3s                    1s
                              ↑
                        critical path = 7s
```

BuildBuddy's Timing tab highlights the critical path. Optimising it (e.g. via RBE parallelism) is how you reduce total build time.

### Memoisation

The theoretical model behind caching: *"if this function (action) was called with these exact arguments (inputs) before, return the stored result."* Bazel's action cache is a distributed memoisation layer over your build.

---

## Remote Execution Concepts

### CAS Digest

A `(hash, size)` pair that uniquely identifies a CAS entry.

```
Digest {
  hash: "a3f8c2d1..."   # SHA-256 of file contents
  size: 12288           # size in bytes
}
```

The Remote Execution API uses digests everywhere — to refer to input files, output files, and action definitions.

### Input Root

The complete set of input files an action needs, assembled by Bazel and uploaded to CAS before the action runs on a remote worker.

In RBE, Bazel:
1. Computes the input root (all declared inputs)
2. Uploads any missing CAS blobs
3. Sends the action to the remote worker
4. Worker reconstructs the input root from CAS
5. Runs the action
6. Uploads outputs to CAS
7. Records the result in the Action Cache

### Executor / Worker

A machine that runs build actions in RBE. Multiple workers run actions in parallel, providing the speed-up that pure caching can't give (caching only helps on repeated builds; RBE helps on the first build too).

---

## BuildBuddy-Specific Terms

### Invocation

A single `bazel build` or `bazel test` run. BuildBuddy records every invocation as a separate entry in the UI, identified by a UUID (e.g. `208d8e9f-cb64-4282-8390-53e6d5f89805`).

Each invocation shows: targets built, cache hit rate, timing waterfall, test results, and BES event stream.

### BES — Build Event Service / Build Event Stream

The gRPC protocol Bazel uses to stream structured build events (target started, action finished, test result, build complete) to an external service like BuildBuddy.

Configured via:
```ini
--bes_backend=grpcs://remote.buildbuddy.io
--bes_results_url=https://app.buildbuddy.io/invocation/
```

**BES is separate from caching.** You can have caching without BES (no UI), or BES without caching (UI shows build events but no cache hits). For the full BuildBuddy experience, you need both.

### Remote Header

An HTTP/gRPC header Bazel attaches to every request to the remote cache or BES backend. Used for authentication:

```ini
--remote_header=x-buildbuddy-api-key=YOUR_KEY
```

This is how BuildBuddy identifies which account's cache to use and where to post invocation results.

---

## Quick Reference

| Term | One line |
|------|----------|
| **CAS** | Storage keyed by content hash — holds the actual file bytes |
| **Action Cache** | Index mapping (inputs + command) → output digests |
| **Cache hit** | Action result found in AC — execution skipped |
| **Cache miss** | Action result not found — Bazel runs it, then stores result |
| **Cache hit rate** | % of actions served from cache (target: 80%+) |
| **Download throughput** | Speed of fetching cached outputs from remote cache |
| **Upload throughput** | Speed of writing new outputs to remote cache |
| **Cache volume** | Total bytes transferred (uploads + downloads) |
| **Action key** | Hash of all inputs + command — the cache lookup key |
| **Critical path** | Longest sequential chain of actions — determines minimum build time |
| **Input root** | Complete input file tree assembled before an action runs |
| **Executor/Worker** | Machine that runs actions in RBE |
| **Invocation** | One `bazel build/test` run — recorded as a unit in BuildBuddy |
| **BES** | Protocol that streams build events to BuildBuddy UI |
| **Remote header** | Auth credential attached to every cache/BES request |
