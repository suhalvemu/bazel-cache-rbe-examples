# Bazel Cache Terminology — With Examples

---

## CAS — Content Addressable Store

The storage layer that holds **actual file bytes**, looked up by the SHA-256 hash of their contents.

### How it works

```
You write:     greeter.cc  (source file, 312 bytes)
Bazel hashes:  SHA-256("Hello from C++, Bazel!...") = a3f8c2d1...
Stores:        CAS["a3f8c2d1..."] = <312 bytes of greeter.cc>
```

When another machine needs the same file, Bazel asks the CAS: *"give me the file with hash a3f8c2d1."* No name. No path. Just the hash.

### Why "content addressable" matters

Two targets produce identical compiled output → they share one CAS entry:

```
apps/c-lib/greet.o    (compiled with clang -O2)  →  hash: b7c3a1...
apps/cpp-lib/greet.o  (same flags, same content)  →  hash: b7c3a1...  ← SAME ENTRY
```

The CAS stores one copy, both targets read it. This is why large monorepos don't blow up in storage costs.

### What gets stored in CAS

```
Inputs:   greeter.cc, version.h, clang binary, stdlib headers
Outputs:  greeter.o, greeter_test (binary), test.log
```

Every file that flows through a Bazel action — source or output — is a CAS blob.

---

## Action Cache (AC)

The **index** that maps *"I want to build X with these inputs"* → *"the result is this CAS digest"*.

### The lookup key

```
Action Cache Key = hash of:
  ├── all input file hashes   (greeter.cc → a3f8c2, version.h → d91be4, clang → 88fa12)
  ├── the command             (clang++ -O2 -std=c++17 -c greeter.cc -o greeter.o)
  └── environment variables   (declared ones only — hermeticity enforced here)

Result → SHA-256 of all the above = 4d9fe7...
```

### A full cache lookup in slow motion

**Step 1 — Bazel computes the action key**
```
inputs:  { greeter.cc: a3f8c2, version.h: d91be4 }
command: clang++ -O2 -c greeter.cc
key:     4d9fe7...
```

**Step 2 — Bazel queries the Action Cache**
```
AC.get("4d9fe7...") → { greeter.o: b7c3a1, size: 14336 }
                                    ↑
                              CAS digest of the output
```

**Step 3a — Cache HIT → fetch from CAS**
```
CAS.get("b7c3a1...") → <14336 bytes of greeter.o>
Written to: bazel-out/.../greeter.o
Time: ~80ms download  (vs 2.4s local compile)
```

**Step 3b — Cache MISS → run locally, then store**
```
Run: clang++ -O2 -c greeter.cc → produces greeter.o (hash: b7c3a1)
AC.put("4d9fe7...", { greeter.o: "b7c3a1" })   ← write AC entry
CAS.put("b7c3a1...", <greeter.o bytes>)         ← write CAS blob
Next build: cache hit guaranteed (until any input changes)
```

### AC and CAS together

```
Action Cache                        CAS
───────────────────────────         ──────────────────────────
4d9fe7... → { greeter.o: b7c3a1 }  b7c3a1... → <greeter.o bytes>
8a21cd... → { greeter_test: f4e2b9 }  f4e2b9... → <greeter_test bytes>
```

AC is the **map**. CAS is the **storage**. Neither works without the other.

---

## Cache Hit

An action whose result was already in the Action Cache. Bazel skips compilation entirely.

### What you see in terminal output

```
[5 / 10] GoStdlib external/rules_go+/stdlib_/pkg; 1s remote-cache    ← HIT
[6 / 10] CppCompile apps/cpp-lib/greeter.o; 2s darwin-sandbox        ← MISS (ran locally)
[8 / 10] GoLink apps/go-service/go-service_/go-service; Downloading 1.4 MiB / 1.4 MiB; 1s remote-cache  ← HIT
```

### What you see in the build summary

```
INFO: 10 processes: 4 remote cache hit, 6 internal.
                    ↑
         4 actions skipped — outputs fetched from BuildBuddy
```

### Cold vs warm build on this repo

```
Build 1 (nothing cached):
  10 processes: 6 internal, 4 darwin-sandbox
  Elapsed: 22.4s

Build 2 (after bazel clean, warm cache):
  10 processes: 4 remote cache hit, 6 internal
  Elapsed: 4.7s    ← 79% faster, zero compilation
```

---

## Cache Miss

An action with no matching AC entry. Bazel runs it locally, then populates the cache for everyone else.

### When does a cache miss happen?

| Cause | Example |
|-------|---------|
| First ever build | Never built before on any machine |
| Source file changed | Edit one line in `greeter.cc` |
| Header changed | Add a field to `version.h` |
| Compiler flag changed | Add `-DDEBUG` to a build |
| Toolchain updated | Go SDK upgraded from 1.22 → 1.23 |
| Bazel version changed | Upgrade from 8.1 → 8.2 |

### Cache miss is surgical — only affected targets rebuild

```
Change:  libs/common/version.cc

Rebuilds:
  libs/common:version           ← changed
  apps/cpp-lib:greeter          ← depends on version
  apps/cpp-lib:greeter_test     ← depends on greeter

Cache hits (unchanged):
  apps/go-service:go-service    ← no dependency on version.cc
  apps/python-lib:python-lib    ← no dependency on version.cc
  apps/java-app:greeter-app     ← no dependency on version.cc
```

This is the core value of Bazel's dependency graph — it rebuilds the minimum possible set.

---

## Cache Hit Rate

The percentage of build actions served from cache in one invocation.

```
Cache Hit Rate = (remote cache hits ÷ total actions) × 100
```

### Real numbers from this repo

```
Cold build (first run):    3 / 112 actions =  2.6%   ← normal for first build
Warm build (second run): 109 / 112 actions = 97.3%   ← healthy production cache
```

### What each range means

| Rate | Meaning | Action |
|------|---------|--------|
| 0–10% | Cold cache or first build | Normal, will improve |
| 30–60% | Cache warming up, or many changes | Wait a few builds |
| 70–85% | Healthy team cache | Good |
| 85–95% | Excellent | Production-ready |
| 95%+ | Near-perfect | Elite — hermetic + shared cache |
| <50% consistently | Problem: non-hermetic builds, missing uploads, or too many changes | Investigate |

### The most common reason for low hit rate

```
# Missing this flag:
--remote_upload_local_results=true

Without it: local builds read from cache but never write to it.
Developers get cache hits from CI, but CI never gets cache hits from developers.
→ hit rate stays low for everyone.
```

---

## Download Throughput

How fast Bazel pulls cached outputs from the remote cache.

```
Build output:
  Downloading apps/go-service/go-service_/go-service, 1.4 MiB / 1.4 MiB; 1s remote-cache
  ↑ Downloaded 1.4 MB in 1s = 1.4 MB/s download throughput
```

### What affects download throughput

```
Machine ←─── network ───→ BuildBuddy / bazel-remote
 │                               │
 │ bandwidth limit               │ disk I/O / CPU
 │ (common bottleneck on CI)     │ (rare bottleneck with managed SaaS)
```

**Typical values:**
- Developer laptop on office WiFi: 5–20 MB/s
- CI runner (GitHub Actions): 20–100 MB/s
- Same datacenter as cache: 200–1000 MB/s (RBE benefit)

Low throughput = cache hits still take time → consider BuildBuddy RBE where workers are co-located with the cache.

---

## Upload Throughput

How fast Bazel writes new build outputs to the remote cache after a cache miss.

```
After a cache miss, Bazel stores the result:
  Uploading apps/cpp-lib/greeter.o (14 KB)    →  0.1s
  Uploading apps/cpp-lib/greeter_test (2.1 MB) →  0.8s
```

Upload only happens when `--remote_upload_local_results=true` is set. This flag makes your local build a **cache contributor**, not just a cache consumer.

```
Without flag:  you benefit from others' builds, contribute nothing
With flag:     you contribute, CI benefits, team benefits
```

---

## Cache Volume

Total bytes transferred during one build — uploads + downloads combined.

```
BuildBuddy invocation summary:
  Downloaded:  48 MB   (cache hits — 109 outputs fetched)
  Uploaded:   220 MB   (cache misses — 3 new outputs stored)
```

### Reading volume as a signal

| Pattern | What it tells you |
|---------|-----------------|
| High download, low upload | Warm cache, mostly hits — healthy |
| High upload, low download | Cold cache or many changes — normal after changes |
| High upload + low hit rate | Hermetic problem — same work being re-uploaded repeatedly |
| Both near zero | Local-only build (BES connected but no remote cache) |

---

## BES — Build Event Service

The gRPC stream Bazel opens **alongside** the build to send structured events to BuildBuddy's UI.

### What events flow over BES

```
Build started     → creates the invocation record in UI
Target configured → "building //apps/cpp-lib:greeter"
Action finished   → "compiled greeter.cc in 2.1s, cache miss"
Test result       → "greeter_test PASSED in 0.7s (shard 1/2)"
Build complete    → final summary: 112 actions, 109 cache hits, 4.7s
```

### BES vs remote cache — two separate things

```
--remote_cache=grpcs://remote.buildbuddy.io
    ↑ stores/fetches build artifacts (outputs)
    ↑ caching works without BES

--bes_backend=grpcs://remote.buildbuddy.io
    ↑ streams build events to the UI
    ↑ UI works without affecting caching
```

You can have caching without BES (fast builds, no UI). You can have BES without caching (UI shows events, but every build is a cache miss). For full value, you want both — which is why both flags are in `.bazelrc`.

### The invocation URL

Every build with BES prints:
```
INFO: Streaming build results to: https://app.buildbuddy.io/invocation/208d8e9f-...
```

Open it while the build is running — the UI updates live as events stream in.

---

## Remote Header

An HTTP/gRPC metadata header Bazel attaches to every request to the cache or BES backend. Used for authentication and routing.

```ini
# user.bazelrc (never commit this)
build --remote_header=x-buildbuddy-api-key=efAkJg6Z...
```

Every cache read, cache write, and BES event carries this header. BuildBuddy uses it to:
- Route requests to your account's cache partition
- Post invocations to your dashboard
- Enforce usage limits on the free tier

### Why it's in user.bazelrc and not .bazelrc

```
.bazelrc  (committed to git)   → shared config, no secrets
user.bazelrc (gitignored)      → personal secrets, local overrides

try-import %workspace%/user.bazelrc   ← loads user.bazelrc if it exists,
                                         silently skips if it doesn't (e.g. CI)
```

In CI, the key is injected via `${{ secrets.BUILDBUDDY_API_KEY }}` as an environment variable — the workflow passes it as `--remote_header` directly, never touching a file.

---

## Critical Path

The longest sequential chain of actions in the build graph. The build cannot finish faster than this chain, no matter how many workers you add.

```
Full build graph:

greeter.cc ──→ greeter.o (2s) ──→ greeter_test_linked (1s) ──→ greeter_test_run (0.7s)
version.cc ──→ version.o  (1s) ──┘
greeter.h  ──→ (header only, no compile)

Critical path:  greeter.cc → greeter.o → greeter_test_linked → greeter_test_run
Duration:       2s + 1s + 0.7s = 3.7s  (minimum possible build time)
```

Adding 10 more workers doesn't help if the critical path is 3.7s — that's the floor.

**BuildBuddy's Timing tab** highlights the critical path in orange so you know where to focus optimization effort.

---

## Quick Reference

| Term | What it is | Example value |
|------|-----------|---------------|
| **CAS** | Storage for file bytes, keyed by hash | `a3f8c2... → <greeter.o bytes>` |
| **Action Cache** | Index: inputs+command → output hashes | `4d9fe7... → { greeter.o: b7c3a1 }` |
| **Cache hit** | Action result found, execution skipped | `4 remote cache hit` in build summary |
| **Cache miss** | Action result not found, runs locally | `4 darwin-sandbox` in build summary |
| **Cache hit rate** | % actions from cache | `97.3%` warm, `2.6%` cold |
| **Download throughput** | Speed fetching cached outputs | `1.4 MB/s` on WiFi, `100 MB/s` CI |
| **Upload throughput** | Speed storing new outputs | Requires `--remote_upload_local_results=true` |
| **Cache volume** | Total bytes transferred | `48 MB down, 220 MB up` |
| **BES** | Event stream → BuildBuddy UI | Separate from caching |
| **Remote header** | Auth credential per request | `x-buildbuddy-api-key=...` |
| **Critical path** | Minimum possible build time | Highlighted in BuildBuddy Timing tab |
| **Invocation** | One `bazel build/test` run | UUID in BuildBuddy dashboard |
