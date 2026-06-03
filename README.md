# Bazel Monorepo Reference

> A hands-on reference for engineers adopting Bazel in a polyglot monorepo — built from real-world Engineering Productivity experience.

Most Bazel tutorials stop at "hello world." This repo shows what a production setup actually looks like: 6 languages building in the same graph, shared libraries with enforced visibility, remote caching that cuts CI costs by 40-79%, RBE that scales build parallelism to 50+ concurrent actions, and hermetic toolchains that eliminate "works on my machine."

Everything here compiles, tests pass, and CI is green.

Built on **Bazel 8.2.1** with **bzlmod** (no legacy WORKSPACE).

---

## Quick Start

```bash
# Prerequisites: Bazelisk (https://github.com/bazelbuild/bazelisk)
brew install bazelisk

# Build everything
bazel build //...

# Test everything
bazel test //...

# Build with BuildBuddy remote cache (requires BUILDBUDDY_API_KEY in user.bazelrc)
bazel build //... --config=buildbuddy-cache
```

---

## What's Inside

### Languages

| Directory | Language | Rules |
|-----------|----------|-------|
| `apps/go-service/` | Go | `rules_go` — binary + test, stamped with git commit |
| `apps/python-lib/` | Python | `rules_python` — library + test |
| `apps/c-lib/` | C | `rules_cc` — library + test |
| `apps/cpp-lib/` | C++ | `rules_cc` — library + GoogleTest |
| `apps/java-app/` | Java | `rules_java` — binary + JUnit 4 test |
| `apps/ts-app/` | TypeScript | `aspect_rules_ts` — ts_project + js_test |

### Monorepo Patterns

| Directory | What it demonstrates |
|-----------|---------------------|
| `proto/` | Cross-language protobuf codegen (Go, C++, Java) |
| `libs/common/` | Shared library depended on by multiple apps |
| `bazel/defs.bzl` | Starlark macros (cc_library_with_test) |
| `bazel/rules.bzl` | Custom rule (generate_header) |
| `platforms/` | Platform definitions for cross-compilation |
| `stamp/` | Build stamping (git commit embedded in binaries) |
| `tools/` | filegroup, genrule patterns |

### Caching

| Directory | Caching approach |
|-----------|-----------------|
| `caching/01-local-disk/` | `--disk_cache` — persists across `bazel clean` |
| `caching/02-bazel-remote/` | Self-hosted `bazel-remote` (Docker Compose) |
| `caching/03-buildbuddy/` | BuildBuddy free tier (hosted, with UI) |
| `caching/04-gcs/` | Google Cloud Storage bucket |

### Remote Build Execution (RBE)

| Directory | RBE provider |
|-----------|-------------|
| `rbe/01-buildbuddy-rbe/` | BuildBuddy (free tier, managed) |
| `rbe/02-buildbarn/` | Buildbarn (self-hosted, Docker Compose) |
| `rbe/03-engflow/` | EngFlow (commercial, managed) |

### Documentation

| File | Topic |
|------|-------|
| `docs/bazel-concepts.md` | BUILD files, targets, labels, three build phases |
| `docs/caching.md` | Cache layers, hit rate, real-world cost savings |
| `docs/rbe.md` | RBE vs caching, when to use each |
| `docs/monorepo-patterns.md` | Dep graph, shared libs, macros, proto, cross-compile |
| `docs/query-examples.md` | `query`, `cquery`, `aquery` command reference |
| `docs/cache-terminology.md` | CAS, Action Cache, BES, cache hit rate, throughput, volume — full glossary |

---

## Config Profiles

All configs are defined in `.bazelrc`. Use `--config=<name>`:

```bash
bazel build //... --config=local-cache       # local disk cache
bazel build //... --config=buildbuddy-cache  # BuildBuddy remote cache
bazel build //... --config=buildbuddy-rbe   # BuildBuddy RBE
bazel build //... --config=bazel-remote     # self-hosted bazel-remote
bazel build //... --config=gcs              # GCS bucket cache
bazel build //... --config=stamp            # embed git commit into binaries
bazel build //... --config=linux            # cross-compile for linux/amd64
bazel test  //... --config=smoke            # run only smoke-tagged tests
```

For configs that need an API key, add to `user.bazelrc` (gitignored):
```
build --remote_header=x-buildbuddy-api-key=YOUR_KEY
```

---

## CI

GitHub Actions workflow (`.github/workflows/ci.yml`) runs on every push and PR:
- Builds and tests all targets with BuildBuddy cache
- On PRs: also runs affected-targets-only job
- Secret: set `BUILDBUDDY_API_KEY` in GitHub repo settings → Secrets

---

## Prerequisites

- **Bazelisk** — version manager for Bazel (reads `.bazelversion`)
- **Docker** — for self-hosted cache/RBE examples (caching/02, rbe/02)
- **pnpm** — for TypeScript example bootstrap
- **gcloud** — for GCS cache example
- **BuildBuddy account** — free at [app.buildbuddy.io](https://app.buildbuddy.io) for cloud cache/RBE

---

## Author

Built by **Suhal Vemu** — Senior Software Engineer specialising in Bazel, build infrastructure, and Engineering Productivity.

- GitHub: [@suhalvemu](https://github.com/suhalvemu)
- LinkedIn: [linkedin.com/in/suhalvemu](https://www.linkedin.com/in/suhalvemu)
- Email: vemusuhal@gmail.com

Questions, issues, or want to discuss Bazel at scale? Open an [issue](https://github.com/suhalvemu/bazel-cache-rbe-examples/issues) or reach out directly — always happy to talk build systems.
