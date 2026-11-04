# Fixture provenance

Reviewed against official documentation on 2026-11-04. These are published documentation examples, not captured account responses:

- `create.official.json`: complete HTTP 201 example from [Create a contact](https://developers.brevo.com/reference/create-contact).
- `send.official.json`: complete HTTP 201 example from [Send a transactional email](https://developers.brevo.com/reference/send-transac-email).

Only whitespace was normalized. Inline contact/list/account and error responses in tests are synthetic variants of the documented shapes, including large IDs, missing fields, zero counts, and explicit false. They are not live captures. No actual contact or email was created.
