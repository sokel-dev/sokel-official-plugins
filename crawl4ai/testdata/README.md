# Fixture provenance

`documented-wire.json` is a **synthetic protocol fixture**, not a response captured from a live instance. It was assembled on 2026-11-04 from the official sources linked in the plugin README. Inputs use non-secret illustrative tokens and example.com URLs; IDs and values are test data.

Crawl4AI v0.9.5 `server.py`, `schemas.py`, `job.py` and `api.py` supply the wire shapes, 202 receipt, task-ID format, processing/completed/failed states and nested result wrapper. Adaptations use one synthetic page, explicit false and zero options, and unwanted HTML/trace fields to verify filtering. No Docker browser container or live crawl was run. Raw Markdown and empty-health checks are not substitutes for a live browser workload.

The generated-dispatch acceptance test compares request bytes and authentication handling. Boundary tests modify these fixtures to exercise failures and limits. The first behavior run used compiling handlers that returned `Not implemented.`; all operation cases failed, then passed after runtime implementation. These tests establish protocol conformance to the cited evidence, not universal compatibility with untested deployments.
