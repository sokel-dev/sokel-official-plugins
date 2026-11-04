# Shopify

Use Shopify GraphQL Admin API version **2026-10** to read shop, product, order and inventory item data, and update a product title or status.

## Credentials and permissions

Set `shop` to the subdomain before `.myshopify.com`, such as `example-shop`. Do not enter a URL, custom storefront domain or `.myshopify.com` suffix. Set `access_token` to an Admin API token issued for that shop. The connector does not obtain or refresh tokens; replace expiring tokens externally. Storefront tokens are not supported.

The app and the authorizing user must have access to the requested resources. Product reads need `read_products`; product updates need `write_products` and permission to update products. Inventory item reads need `read_inventory` or `read_products`. Order reads need `read_orders`; Shopify normally exposes only the last 60 days. Older orders require separately approved `read_all_orders` access. Protected customer data rules can further restrict an app. This version does not request customer contact or address fields.

`health_check` performs a real, read-only shop query. It returns `ok` and a sanitized `message`, and does not prove permission for every operation. `get_shop` returns ID, shop name, myshopify domain and currency.

## Products and inventory items

`list_products` accepts `limit` (1–100, default 50), an optional Shopify search `query`, and a pagination `cursor`. `get_product` requires a full `gid://shopify/Product/...` ID. Product output contains ID, title, handle, status, vendor, product type, tags and update time.

`list_inventory_items` uses the same pagination and query inputs. It returns item ID, SKU, tracking flag, shipping requirement and update time. It does **not** return stock quantities per location, prices or variant details.

`update_product` requires a product GID and a nonempty `changes` object. Supported fields are `title` and `status`; omit a field to leave it unchanged. Status may be `ACTIVE`, `DRAFT` or `ARCHIVED`. A blank title is rejected. Changes can affect store visibility. Setting ACTIVE alone does not publish a product to a sales channel. This connector does not modify prices, variants, payments or refunds.

## Orders

`list_orders` accepts the same `limit`, `query` and `cursor` inputs. `get_order` requires a `gid://shopify/Order/...` ID. Outputs include order name, creation/update times, financial/fulfillment status and current total in shop currency. Amounts are decimal strings, not binary floating-point values. Order line items, customer identity, fulfillment actions and order writes are outside this version.

## Pagination and failures

Keep query and operation unchanged when passing `next_cursor` into the next request. Continue only while `has_more=true`. Each call reads one page. Changes during pagination can affect results; cursors are not a snapshot or durable synchronization checkpoint. Shopify GraphQL query-cost limits apply. The connector does not automatically retry rate-limited calls or writes.

Every operation has a 60-second platform timeout and a 55-second HTTP timeout. Requests and responses are bounded to 8 MiB. Calls only go to the configured shop's official HTTPS Admin API endpoint; redirects are rejected. GraphQL errors, mutation userErrors, missing resources and malformed success responses fail explicitly. Upstream error text is not echoed because it can contain sensitive input. An unexpected API-version response header fails explicitly instead of silently accepting version fallback.

After a timeout or invalid write receipt, read the product before retrying. No live shop call was performed during development. Tests use clearly labeled synthetic protocol fixtures. No triggers, webhooks, bulk jobs or inventory writes are included.

## Official references

- [GraphQL Admin products](https://shopify.dev/docs/api/admin-graphql/2026-10/queries/products)
- [Update product](https://shopify.dev/docs/api/admin-graphql/2026-10/mutations/productUpdate)
- [Orders and access limitations](https://shopify.dev/docs/api/admin-graphql/2026-10/objects/Order)
- [Inventory items](https://shopify.dev/docs/api/admin-graphql/2026-10/queries/inventoryItems)
