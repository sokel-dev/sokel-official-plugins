# Fixture provenance

`documented-wire.json` is a **synthetic protocol fixture**, not a response captured from a live instance. It was assembled on 2026-11-04 from the official sources linked in the plugin README. Inputs use non-secret illustrative tokens and example.com URLs; IDs and values are test data.

PocketBase's records documentation supplies the collection/record envelope, list page shape and expected HTTP statuses. The health example follows its health API. Adaptations add numeric value `9007199254740993`, false and zero, and reduce pagination to a one-record page to exercise precision and continuation. The ordinary token header follows the official authentication documentation. No PocketBase binary was executed; inspecting a release archive for its MIT license is not live API acceptance.

The generated-dispatch acceptance test compares request bytes and authentication handling. Boundary tests modify these fixtures to exercise failures and limits. The first behavior run used compiling handlers that returned `Not implemented.`; all operation cases failed, then passed after runtime implementation. These tests establish protocol conformance to the cited evidence, not universal compatibility with untested deployments.
