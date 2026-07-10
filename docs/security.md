# Security — inferbench

This repo's security posture is narrow and strict: it holds credentials to benchmark targets, and
it emits artifacts designed to be shared publicly. The whole posture reduces to: **secrets never
enter emitted artifacts, and no real user data ever exists here.**

## Credentials

- Target API keys are supplied **only** via environment variables (e.g. `INFERBENCH_API_KEY`) or
  an ignored local file (gitignored, documented in the CLI help). Never in workload files,
  manifests, raw events, run logs, results, reports, shell history examples in docs, or git.
- `Authorization` headers are **redacted everywhere**: in run logs, debug output, error messages,
  and any recorded request metadata. Redaction is implemented centrally in the client, not per
  call site.
- The manifest capturer records *which* auth mode was used (e.g. `auth: bearer-env`) but never the
  credential material.

## No real data

- Workload prompts are **synthetic, generated deterministically from seeds**. No real user data,
  no PII, no scraped text, ever. This is also a reproducibility feature: the prompt content is a
  function of (workload version, seed).
- Raw events contain measurements and classifications, not prompt/response bodies. Token *counts*
  are recorded; content is not.

## Shareable-by-construction artifacts

Result files, manifests, and reports are the portfolio's public evidence, so they must be safe to
publish without scrubbing:

- The emitted-artifact schemas (Contract 3) contain no field where a secret could legitimately
  live; the writer additionally never serializes headers or environment.
- **Tested, not assumed:** the secret-leak test in `testing.md` runs the generator with a
  real-shaped key in the environment and asserts the key (and any `Authorization` value) appears
  in no emitted artifact. This test is part of CI from IB-T002 on.

## Repository hygiene

- `.gitignore` covers local credential files, run output directories, and any downloaded model or
  fixture material not meant for the repo.
- No secrets in CI configuration; CI targets the released mock image, which needs no real
  credentials.
- Dependencies are pinned (Go modules with sums; Python with a lock/requirements pin) so published
  reports' one-command reproduction lines resolve to the same code.

## Out of scope

Target-side security (gateway authn/z, tenant isolation, TLS termination) is `infergate`'s and
`inferops`'s. `inferbench` treats the target as an opaque endpoint and only ensures it doesn't
leak what it was given.
