# FAQ — Cache Misses and Slow Builds

Sourced from Bazel documentation, BuildBuddy engineering blog, GitHub issues, and community discussions.

---

## Cache Miss FAQs

---

### Q: My hit rate is 0% even though I ran the build before. Why?

**Most likely cause: you ran `bazel clean --expunge`.**

```bash
bazel clean          # clears local output, does NOT clear remote cache
bazel clean --expunge  # clears everything including the local analysis cache
                       # → forces full remote cache lookup, still gets hits
```

If hit rate is still 0% after `bazel clean` (not `--expunge`), your remote cache is not being reached. Check:

```bash
# Does the build output show the BES/cache URL?
bazel build //apps/go-service --config=buildbuddy-cache 2>&1 | grep "Streaming"
# If nothing: your API key or --bes_backend flag is missing
```

---

### Q: I changed nothing, but the build is rebuilding everything. What happened?

**Most likely cause: a Bazel flag changed and discarded the analysis cache.**

Bazel drops its in-memory analysis cache whenever a build option changes. You'll see:

```
WARNING: Build option --compilation_mode has changed, discarding analysis cache.
```

Common triggers:
- Switching between `bazel build` and `bazel test` with different flags
- Running `bazel query` or `bazel cquery` between builds (different config)
- Adding `--define`, `--config`, or `--compilation_mode` to one build but not another

**Fix:** Use a consistent set of flags. Put them in `.bazelrc` so every invocation uses the same config. To make cache discards an error instead of a silent warning:

```bash
build --noallow_analysis_cache_discard   # fails loudly on discard (Bazel 6.4+)
```

---

### Q: CI gets cache misses but my local build gets hits. Why?

**Most likely cause: your local machine and CI have different environments leaking into the build.**

Common culprits:

| Issue | Symptom | Fix |
|-------|---------|-----|
| System JDK vs Bazel JDK | Java action keys differ between machines | `build --java_runtime_version=remotejdk_21` |
| `PATH` leaking into actions | Different paths → different action keys | Use `--action_env` to pin declared vars |
| Local user env vars | `$HOME`, `$USER` in a genrule | Remove from genrule command |
| Different OS/arch | macOS local vs Linux CI | Expected — cross-platform cache miss |
| Bazel version mismatch | 8.1 local vs 8.2 CI | Pin via `.bazelversion` |

**Debug with execution logs:**

```bash
# Run locally, save execution log
bazel build //apps/cpp-lib --execution_log_compact_file=/tmp/local.log

# Run in CI, save execution log
bazel build //apps/cpp-lib --execution_log_compact_file=/tmp/ci.log

# Compare: find the action that differs
bazel-execlog diff /tmp/local.log /tmp/ci.log
```

The diff shows exactly which input hash differs between local and CI.

---

### Q: My genrule is always a cache miss. How do I fix it?

**Cause: the genrule command produces non-deterministic output.**

```python
# BAD — embeds a timestamp, different output every run
genrule(
    name = "bad_gen",
    outs = ["info.txt"],
    cmd = "echo 'Built at $(date)' > $@",  # ← non-deterministic
)

# BAD — reads from PATH (non-hermetic)
genrule(
    name = "bad_gen2",
    outs = ["out.txt"],
    cmd = "my-custom-tool > $@",  # ← tool must be a declared input
)

# GOOD — deterministic, hermetic
genrule(
    name = "good_gen",
    srcs = [":my_tool"],           # tool declared as input
    outs = ["out.txt"],
    cmd = "$(location :my_tool) --version=1.0 > $@",  # fixed inputs only
)
```

**Rule:** any genrule that produces different bytes on different runs will always be a cache miss. Audit every `cmd` for: `date`, `hostname`, `$RANDOM`, `$PID`, network calls, or tools from `PATH`.

---

### Q: Targets using `--stamp` (build stamping) are always cache misses. Is that expected?

**Yes — for STABLE_ variables. Volatile variables should not cause misses.**

```bash
# stamp/workspace_status.sh outputs two kinds of variables:
STABLE_GIT_COMMIT abc123   # changes when you push → SHOULD bust cache
BUILD_TIMESTAMP 2026-06-03  # changes every build → should NOT bust cache
```

Bazel treats `STABLE_*` values as real cache inputs — changing them invalidates stamped targets. `BUILD_TIMESTAMP` (no prefix) is volatile — Bazel intentionally ignores it for caching.

**Fix if stamped builds are unexpectedly missing:**

```bash
# Don't use --stamp during development
bazel build //apps/go-service           # no stamp, maximum cache hits

# Only stamp for release builds
bazel build //apps/go-service --config=stamp
```

**Known Bazel bug:** in some versions, `volatile-status.txt` content leaks into the action key. Workaround: use `--nostamp` unless you specifically need version embedding. ([GitHub #16231](https://github.com/bazelbuild/bazel/issues/16231))

---

### Q: `--remote_upload_local_results` — do I need it?

**Yes. Without it, your local builds never populate the shared cache.**

```
Without flag:  local build reads from remote cache, never writes to it
               → developers benefit from CI cache hits
               → CI never benefits from developer builds
               → hit rate stays 60-70% forever

With flag:     local builds write outputs to remote cache
               → CI hits outputs built by any developer
               → hit rate reaches 90%+
```

```ini
# .bazelrc
build:buildbuddy-cache --remote_upload_local_results=true  # ← already set in this repo
```

---

### Q: Cache TTL expired — Bazel is rebuilding everything after 3 hours. Why?

**Bazel's default remote cache TTL is 3 hours.** After that, cached action results are considered stale even if nothing changed.

This affects long-running CI pipelines or overnight builds.

```bash
# Extend the TTL check (doesn't change server TTL, just Bazel's local staleness check)
build --experimental_remote_cache_ttl=86400s   # 24 hours

# Or tell Bazel not to check TTL at all
build --remote_cache_check_ttl=never
```

On BuildBuddy free tier, actual cache objects are retained for 7 days. The 3-hour issue is a Bazel client-side behaviour. ([GitHub #26140](https://github.com/bazelbuild/bazel/issues/26140))

---

### Q: I added a new `--define` or `--config` flag. Now everything rebuilds. Normal?

**Yes — Bazel flags that affect action keys will always cause a full rebuild the first time.**

```bash
bazel build //... --define=env=prod   # first run: all misses (new cache partition)
bazel build //... --define=env=prod   # second run: all hits
bazel build //... --define=env=dev    # all misses again (different partition)
```

**Fix:** treat `--define` as a cache partition key. Each unique combination of flags has its own cache namespace. Minimise the number of distinct flag combinations in CI.

---

### Q: How do I find out exactly WHY a specific action was a cache miss?

**Use `--explain` to get a per-action explanation:**

```bash
bazel build //apps/cpp-lib --explain=/tmp/explain.log --verbose_explanations
cat /tmp/explain.log
```

Output example:
```
Executing CppCompile apps/cpp-lib/greeter.o:
  Reason: output apps/cpp-lib/greeter.o is missing
  Reason: input apps/cpp-lib/version.h changed (was: d91be4..., now: a3f8c2...)
```

For remote cache specifically, use execution logs:
```bash
bazel build //apps/cpp-lib \
  --execution_log_compact_file=/tmp/exec.log \
  --remote_print_execution_messages=failure
```

---

## Slow Build FAQs

---

### Q: My first build is always slow. Is that fixable?

**Yes and no.** The first build (cold) must compile everything. RBE helps here — actions run in parallel on remote workers instead of sequentially on your machine.

```bash
# Local: compiles sequentially on your cores
bazel build //...              # 120s on MacBook (4 effective parallel actions)

# RBE: distributes across workers
bazel build //... --config=buildbuddy-rbe   # 25s (50 concurrent remote actions)
```

For the second run (warm), remote caching eliminates almost all work — only changed targets rebuild.

---

### Q: `bazel query` runs fast but then `bazel build` is slow. What happened?

**`bazel query` discards the analysis cache, forcing a full re-analysis on the next build.**

```bash
# This sequence is slow
bazel build //apps/go-service      # builds and caches analysis
bazel query //apps/...             # ← DISCARDS analysis cache silently
bazel build //apps/go-service      # re-analyzes everything from scratch

# Fix: use cquery for analysis-time queries (doesn't discard cache)
bazel cquery //apps/...
```

Or use `--noallow_analysis_cache_discard` to catch this immediately.

---

### Q: My build is slow because too many targets depend on one shared library. Fix?

**This is the "fan-out problem."** When a frequently-changed library sits deep in the dep graph, every change to it invalidates all its consumers.

```
libs/common:version  ← changes often
    ├── apps/cpp-lib:greeter
    ├── apps/c-lib:greet
    ├── services/auth:auth_service
    └── services/api:api_server    ← everything rebuilds on every version.cc change
```

**Fixes:**
1. **Split the library** — separate stable API (`version.h`) from frequently-changing implementation
2. **Use interfaces** — consumers depend on a header-only target, not the implementation
3. **Reduce fan-out** — not everything needs to depend on `version`; audit the actual necessity

```python
# Split into stable header + changing implementation
cc_library(name = "version_header", hdrs = ["version.h"])   # stable, rarely changes
cc_library(name = "version", srcs = ["version.cc"], deps = [":version_header"])

# Consumers depend on the stable header, not the implementation
cc_library(name = "greeter", deps = ["//libs/common:version_header"])
```

---

### Q: glob patterns are making my build slow. What's the right way to use them?

**Overly broad globs force Bazel to stat many files, slowing the loading phase.**

```python
# BAD — globs the entire tree, defeats incremental builds
srcs = glob(["**/*"])

# BAD — globs headers that might not change together
hdrs = glob(["**/*.h"])

# GOOD — explicit, fast, precise
srcs = ["greeter.cc"],
hdrs = ["greeter.h"],

# ACCEPTABLE — scoped to current package only
srcs = glob(["*.cc"]),     # only current dir
hdrs = glob(["*.h"]),
```

Broad `**` globs also cause cache misses when ANY file in the tree changes — even an unrelated one.

---

### Q: Switching between `bazel build` and `bazel test` makes subsequent builds slow. Why?

**`bazel test` uses a different configuration from `bazel build`** — specifically around test trimming. Each switch can discard the analysis cache.

**Fix:** pin the test configuration to match the build configuration:

```ini
# .bazelrc
build --trim_test_configuration=false
```

Or always use `bazel test` (not `bazel build`) for targets you'll be testing — the analysis cache stays warm.

---

### Q: My build is slow even with RBE. The timing shows long idle gaps. Why?

**Cause: the critical path has sequential bottlenecks RBE can't parallelise.**

```
Build timeline (50 workers available):

 0s ─── compile A (remote, 2s) ─┐
                                 ├── link B (remote, 8s) ← critical path bottleneck
 0s ─── compile C (remote, 1s) ─┘         │
                                           └── test B (remote, 1s)

Total: 11s — 50 workers didn't help because link B must wait for compile A+C
```

**Find the critical path:** open BuildBuddy → Timing tab → longest orange bar chain.

**Fix options:**
- Split large targets into smaller ones (more parallelism)
- Use `--jobs` to increase concurrency: `--jobs=200`
- Identify if link time (not compile time) is the bottleneck — link is harder to parallelise

---

### Q: Build is slow specifically during the analysis phase (before any compilation). Why?

**Common causes:**

1. **Too many packages** — Bazel loads all BUILD files in `//...`. If you have 10,000 packages, loading takes time.
2. **Complex Starlark macros** — macros that generate hundreds of targets slow down loading.
3. **`bazel query` side effect** — as above, query discards analysis cache.

**Diagnose with profiling:**

```bash
bazel build //... --profile=/tmp/profile.gz
# Open in Chrome: chrome://tracing → load /tmp/profile.gz
# Look for large blocks in "Analysis" or "Loading" phases
```

---

### Q: What is the single most impactful thing I can do to speed up CI builds?

**Add `--remote_upload_local_results=true` + switch to RBE.**

Priority order of impact:

```
1. Remote cache (--config=buildbuddy-cache)
   Impact: 70-90% build time reduction on warm builds
   Cost: free tier available

2. --remote_upload_local_results=true
   Impact: developers warm the cache for CI and each other
   Cost: zero

3. RBE (--config=buildbuddy-rbe)
   Impact: cold builds 3-10x faster via parallelism
   Cost: free tier limited

4. Pin Bazel version (.bazelversion)
   Impact: prevents accidental analysis cache discards from version changes
   Cost: zero

5. Hermetic toolchains
   Impact: consistent cache keys across all machines
   Cost: setup effort
```

---

## Debugging Checklist

Copy this when your cache hit rate drops unexpectedly:

```bash
# 1. Is the remote cache actually being reached?
bazel build //apps/go-service --config=buildbuddy-cache 2>&1 | grep "Streaming"

# 2. Is the analysis cache being discarded?
bazel build //apps/go-service 2>&1 | grep "discarding analysis cache"

# 3. What exactly caused a cache miss?
bazel build //apps/cpp-lib --explain=/tmp/explain.log && cat /tmp/explain.log

# 4. Generate an execution log and compare two runs
bazel build //apps/cpp-lib --execution_log_compact_file=/tmp/run1.log
bazel clean
bazel build //apps/cpp-lib --execution_log_compact_file=/tmp/run2.log
# Then diff run1 vs run2 to find what changed

# 5. Check for non-hermetic actions (environment variables leaking in)
bazel build //apps/cpp-lib --sandbox_debug 2>&1 | grep -i "env\|PATH\|HOME"

# 6. Are timestamps in your genrule outputs?
grep -r "date\|timestamp\|$(date" */BUILD.bazel **/BUILD.bazel

# 7. Is --stamp causing unexpected misses?
bazel build //apps/go-service --nostamp   # bypass stamping entirely
```

---

## Sources

- [Debugging Remote Cache Hits for Local Execution — Bazel docs](https://bazel.build/remote/cache-local)
- [Debugging Remote Cache Hits for Remote Execution — Bazel docs](https://bazel.build/remote/cache-remote)
- [Why is my Bazel build so slow? — BuildBuddy blog](https://www.buildbuddy.io/blog/debugging-slow-bazel-builds/)
- [Optimize Iteration Speed — Bazel docs](https://bazel.build/advanced/performance/iteration-speed)
- [Breaking down build performance — Bazel docs](https://bazel.build/advanced/performance/build-performance-breakdown)
- [Bazel invalidates and rebuilds all actions when TTL expires — GitHub #26140](https://github.com/bazelbuild/bazel/issues/26140)
- [volatile-status.txt affects caching key — GitHub #16231](https://github.com/bazelbuild/bazel/issues/16231)
- [Cache misses with volatile keys in x_defs — rules_go #2102](https://github.com/bazelbuild/rules_go/issues/2102)
- [Advanced Bazel Troubleshooting — Mindful Chase](https://mindfulchase.com/explore/troubleshooting-tips/build-bundling/advanced-bazel-troubleshooting-fixing-build-performance,-dependency-issues,-and-remote-caching.html)
- [How Bazel Works — Hashnode](https://sluongng.hashnode.dev/bazel-caching-explained-pt-1-how-bazel-works)
- [Using Remote Cache Service for Bazel — ACM Queue](https://queue.acm.org/detail.cfm?id=3287302)
