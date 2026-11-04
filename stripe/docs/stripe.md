# Stripe

Read customers, subscriptions and PaymentIntents, and receive signed Stripe snapshot webhook events. All outbound Stripe API requests in this version are GET requests. The plugin does not create payments, charge customers, confirm PaymentIntents, issue refunds, or change subscriptions.

## Setup

Save a Stripe secret key or restricted key as **Secret or restricted API key**. A restricted key needs read access for the resources you query. Publishable keys are rejected. Test-mode keys are recommended for acceptance; returned `livemode` indicates the mode of each object or event. This is a manual credential integration, with no OAuth provider or Connect account routing inputs.

Run **Check credentials**. It lists one customer and returns `ok` and `message`; it verifies customer read access only, not subscription permissions, PaymentIntent permissions or the webhook signing secret. Failures return `ok=false`.

The API base is `https://api.stripe.com`. Operators may set `STRIPE_API_BASE_URL` to a trusted HTTPS deployment endpoint. Workflow inputs and credentials cannot override it. HTTP is allowed only for loopback tests; redirects are rejected. Every API request sends `Stripe-Version: 2026-10-28.endive`, and webhook endpoints must use that same version for supported events.

## Read operations

| Operation | Behavior |
| --- | --- |
| `health_check` | List one customer to check read access. |
| `list_customers` | List up to 100 customers, optionally filtered by email. |
| `get_customer` | Read one `cus_` ID. |
| `list_subscriptions` | List up to 100 subscriptions, optionally filtered by customer and status. |
| `get_subscription` | Read one `sub_` ID. |
| `list_payment_intents` | List up to 100 PaymentIntents, optionally filtered by customer. |
| `get_payment_intent` | Read one `pi_` ID. |

List operations return `has_more` and `next_starting_after`. Pass the returned ID as `starting_after` to read the next page, until `has_more=false`. Each call reads one page. An empty page with `has_more=true`, duplicate IDs or a repeated cursor fails instead of silently truncating results. Lists are not an immutable snapshot while account data changes.

Subscription status can be `all`, `active`, `past_due`, `unpaid`, `canceled`, `incomplete`, `incomplete_expired`, `trialing` or `paused`. Omitting it retains Stripe's default, which excludes canceled subscriptions. Object IDs remain strings. Timestamps are Unix seconds; amounts are integer smallest currency units, interpreted according to Stripe's currency rules rather than always divided by 100. A missing or null customer reference is represented by an empty string.

Outputs deliberately select a small typed subset. PaymentIntent output excludes `client_secret`, payment methods, card data and arbitrary metadata. Subscription output excludes item-level billing periods. A deleted customer can contain only its ID and `deleted=true`; other projected fields then use empty strings, zero or false. No raw upstream object is exposed.

## Signed webhooks

Set **Webhook signing secret** to the endpoint-specific `whsec_...` secret, then configure a Stripe HTTPS webhook destination using the platform-provided URL for this credential. Choose snapshot events and API version `2026-10-28.endive`. Stripe CLI forwarding uses its own signing secret; it is not interchangeable with a Dashboard endpoint secret. Do not reuse a secret from a different endpoint or mode.

| Stripe event types | Sokel event |
| --- | --- |
| `customer.created`, `customer.updated`, `customer.deleted` | `customer_changed` |
| `customer.subscription.*` | `subscription_changed` |
| `payment_intent.*` | `payment_intent_changed` |

The handler verifies HMAC-SHA256 against the exact raw request bytes before parsing JSON. It accepts a valid `v1` signature with a timestamp within 300 seconds in either direction, supports multiple `v1` signatures during key rotation, and requires the event ID, creation timestamp, mode and expected object type. Supported events with a different API version are rejected. Thin/v2 events are not supported. Validly signed unsupported event types receive HTTP 200 without a Sokel event. No signing secret returns HTTP 503; invalid signatures return HTTP 401; malformed supported events return HTTP 400; oversized bodies return HTTP 413.

Each emitted event includes `event_id`, `event_type`, `created`, `livemode`, `account_id`, and the corresponding typed customer, subscription or PaymentIntent projection. `account_id` is an empty string when no connected-account field is present. `event_id` is passed as the platform idempotency key so retries use a stable identity. Consumers should deduplicate by that ID and tolerate events arriving out of order.

An HTTP 200 response means the SDK accepted local publication, not that a workflow completed or an event was durably committed. SDK v0.7.7 publishes without a durable acknowledgement, so delivery is best effort; failures or restarts can cause losses or replay. The handler does not claim exactly-once delivery. A local publication error returns HTTP 503. Webhook bodies are limited to 2 MiB; the base64 envelope fallback is bounded at 3 MiB.

## Errors and validation

Outbound requests time out after 45 seconds and JSON responses are limited to 4 MiB. There are no automatic retries. A 429 error reports `Retry-After` or a 60-second fallback. Error bodies, API keys, signing secrets and transport URLs are never included in errors.

Run `go generate ./...`, `go test -race ./...`, `go vet ./...`, `go build ./...`, and `go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen check .`. The only direct dependency is public Sokel Plugin SDK v0.7.7. Synthetic HTTP and signed-event fixtures cover exact queries, pinned versions, generated output binding, pagination, secret projection, signature rotation, tampering, clock tolerance, mode and event identity, replay keys and local publication failures. These checks are not live Stripe acceptance, billing activity or registry publication.

## Official references

- [Customers](https://docs.stripe.com/api/customers/list)
- [Subscriptions](https://docs.stripe.com/api/subscriptions/list)
- [PaymentIntents](https://docs.stripe.com/api/payment_intents/list)
- [API versioning](https://docs.stripe.com/api/versioning)
- [Webhook endpoints](https://docs.stripe.com/webhooks)
- [Official Go signature verification implementation](https://github.com/stripe/stripe-go/blob/master/webhook/client.go)
