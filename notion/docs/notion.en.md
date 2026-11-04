# Notion

Read and write Notion: search, query tables, create pages, edit Markdown content, work with comments and files,
and use the plugin as an **event source** to trigger workflows when a data-source row is added or modified.

## Step 0: run the plugin process

This is a **self-hosted plugin**. The platform contains its catalog entry, but you must run the process yourself.
It can run anywhere that can reach the broker and Notion. Copy the connection command from the plugin page, or use:

```bash
docker run -d --name notion --restart unless-stopped \
  -e SOKEL_ENDPOINT=https://<platform-address> \
  -e SOKEL_TOKEN=skp_xxx \            # Copy from Plugin page → Connection group → Connection command
  ghcr.io/sokel-dev/sokel-plugin-notion:latest
```

Once started, the logs show `已接入平台：插件「Notion」就绪` (“Connected to the platform: Notion plugin ready”),
and the connection group on the plugin page shows an online replica.
**Without this step, none of the plugin's operations work on the canvas**, even with correctly configured credentials.

⚠️ The event source uses incremental polling and stores its cursor in the credential. **A single replica is recommended**:
multiple replicas advance the same cursor independently, causing duplicate events and cursor jumps.

## Step 1: let the integration see your pages—the most common mistake

A Notion integration **cannot see anything by default**, even with a valid token. If you skip this step, every operation reports
that the object cannot be found or accessed. Notion's original message is only “Could not find page with ID ...”,
which can look like an incorrectly copied ID.

Open the page or database in Notion, then choose **⋯ → Connections → Add your integration** in the upper-right corner.
Subpages inherit access automatically, so sharing a top-level page is usually enough.

## Step 2: credentials—choose one of two authentication methods

**A. Internal integration secret**, the easiest option for a self-hosted setup

1. Open <https://www.notion.so/profile/integrations> and choose **New integration**.
2. Select **Internal**, choose the workspace, and enable **Read content / Update content / Insert content**.
   Enable comment capabilities as well if needed.
3. On the Secrets page, copy the **Internal Integration Secret**, starting with `ntn_`,
   into the credential's **Internal integration secret** field.

**B. OAuth authorization**, for shared use or when users should not handle secrets

**Leave all three token fields blank** and click **Authorize** on the credential row.
On Notion's consent page, select the pages to share, then return to the platform.
The platform writes `access_token`; entering it manually can break the configuration.

This requires **one-time setup by the platform administrator**. Afterward, everyone can create a credential and authorize directly:

1. Open the integration at <https://www.notion.so/profile/integrations> and change its type to **Public**.
2. Set the **Redirect URI** to **exactly match** the platform's callback URL:

   ```
   <platform-address>/api/v1/credentials/oauth/callback
   ```

   Development and production domains are **separate entries** and must both be registered,
   such as `http://localhost:5173/...` and `https://your-domain/...`.
   A one-character mismatch produces `redirect_uri_mismatch` during the code exchange.
3. Copy the **OAuth client ID / client secret** from that page into the platform backend's environment variables,
   then restart the backend:

   ```bash
   NOTION_OAUTH_CLIENT_ID=...
   NOTION_OAUTH_CLIENT_SECRET=...
   NOTION_OAUTH_REDIRECT_URL=https://your-domain/api/v1/credentials/oauth/callback   # Same value as step 2
   NOTION_OAUTH_PROXY=http://127.0.0.1:7897    # Optional if the platform cannot reach Notion, such as in mainland China
   ```

   Without this configuration, clicking **Authorize** explicitly reports `平台未配置 Notion OAuth 应用`
   (“The platform has not configured a Notion OAuth app”); it does not fail silently.

**Note:** for a public Notion integration, users choose which pages to grant on the consent screen.
**There are no scopes to request**, so what the integration can read depends on what that user selected.

The credential row's **Check** action reads the integration's own identity and returns its name and workspace.
It answers whether the token is valid and which workspace is connected; **it does not prove access to a particular page**.
That page still needs to be connected to the integration as described in step 1.

If both authentication methods are filled in, the internal secret takes precedence.

## Deployments in mainland China

Notion is hosted outside mainland China. An unreachable network may show only a timeout, without identifying the connectivity issue.
Enter an address such as `http://127.0.0.1:7897` in the credential's **Outbound proxy** field.
The proxy applies to that credential and does not affect other platform traffic.

## Databases versus data sources: since September 2025

A **database** is a container holding one or more **data sources**, the tables you see in Notion.
Queries, row creation, and schema changes use a **data source ID**.

- For a single-source database, which is the usual case, paste its database URL into **Data source ID**.
  The plugin automatically resolves the one data source inside it.
- For a multi-source database, use **List data sources** first and select the desired ID. The plugin will not guess.

All ID fields accept **complete Notion URLs**. The plugin extracts the ID and recognizes that the value after `?v=`
is a view ID, not a database ID.

## Writing properties

Each row read from Notion includes two representations of its properties:

- `props`: normalized, flat values that can be read and referenced directly.
  Text becomes a string; multi-select becomes a string array; dates become `{start, end}`;
  people become an array of names; relations become an array of page IDs.
- `properties_raw`: Notion's original nested JSON. Use it for values such as `rollup` and `formula`
  where normalization would lose information.

For writes, supply the normalized representation. The plugin uses the table schema to wrap it in Notion's required format:

```json
{"Status": "In progress", "Tags": ["Report", "Reviewed"], "Due": "2026-08-20", "Owner": ["Alex"], "Done": true}
```

Keep these points in mind:

- **Misspelled column names, nonexistent options, or computed columns** are rejected before the request is sent,
  with the available columns or options listed in the error.
- `formula`, `rollup`, and fields such as Created time are computed by Notion and cannot be written.
- To clear a column, pass `null`.
- People columns such as Owner accept **names or user IDs**. Names are resolved by looking up workspace members.
- The title does not need to be included in `props`. Create-page and update-page operations have a separate **Title** input,
  since the title column has different names in different databases.

## Use Markdown for page content

**Read page** returns the entire page as Markdown, and **Update page content** accepts Markdown. Three modes are supported:

| Mode | Purpose |
|---|---|
| `append` | Append to the end of the page; the default |
| `replace` | Replace the entire page |
| `edit` | Search and replace `old_str` with `new_str`; **recommended for models**, as it is much safer than rewriting the entire page |

For content Markdown cannot express, such as database blocks, embeds, or boards, use **Read child blocks / Append raw blocks**.
Those operations use Notion's original block JSON.

## Events: data-source changes

- In the **credential**, set **Data sources to watch** to comma-separated IDs or URLs. Blank means no events are emitted.
- Each credential has one polling loop, every 60 seconds by default. Do not shorten the interval as you watch more tables;
  Notion limits requests to three per second.
- Two events are available: **Row added** and **Row modified**, distinguished by whether creation and last-edit times are nearly equal.
- The first startup begins at the current point and **does not replay historical rows**.
- Events contain metadata and properties only. Add **Read page** downstream if you need the body.

**For real-time events, or events about page content and comments**, Notion webhook subscriptions must be created manually
in the integration settings; the API cannot create them, so the plugin cannot install them. Instead:

1. Add a **Webhook trigger** node to the canvas and copy its URL.
2. In the Notion integration settings, open **Webhooks → New subscription** and paste the URL.
3. Notion first sends a request containing `verification_token`. Find it in the platform's **Run history**
   and paste it into Notion's verification field.
4. Afterward, all 23 event types arrive in real time. The payload contains only `entity.id`;
   add **Read page** to retrieve the content.

## Limits

- Notion **search matches titles only**, not page bodies.
- A request allows at most 100 blocks and 500 KB. Each rich-text item allows 2,000 characters;
  the plugin splits longer text automatically.
- A single file upload is limited to 20 MB.
- Pages can only be moved to **Trash**; the API cannot permanently delete them.
- Filters and sorts use Notion's native DSL and are passed through unchanged. See
  <https://developers.notion.com/reference/post-database-query-filter>.
