Use Tencent Docs Open API to list, search and create files; read/write spreadsheet text; and read or insert text into online documents. Spreadsheet and document operations use v3; file management uses v2.

Register an application and obtain user OAuth authorization on Tencent Docs. Configure Access-Token, Client-Id and Open-Id. These are Open API credentials, not an MCP space token. This version accepts manually issued access tokens; renew expired credentials yourself. File listing and health require file read permission; creation requires file write permission. Sheets require scope.sheet.readonly / scope.sheet.editable, or scope.sheet; grant corresponding document read/write scopes.

File listing returns one page. Pass next as the next offset; search also reports has_more. Use API file IDs, not browser short links.

Read A1 ranges such as A1:C10 with at most 1000 rows, 200 columns and 10000 cells. Write rows at zero-based start coordinates, e.g. [{"values":["Name","Amount"]},{"values":["Alice","100"]}]. Values are written as text, not formulas, and only cell text is sent. Inspect updated_cells and updated to distinguish a complete write from a partial update.

Read the recursive document tree and version before inserting text. Pass the returned version and use upstream document indices. The service can transform edits across up to 100 newer versions; inspect the result when concurrent edits matter. Writes are not retried automatically; check the result before retrying a timeout.

Official references: [headers](https://docs.qq.com/open/document/app/openapi/v2/file/common/req_headers.html), [files](https://docs.qq.com/open/document/app/openapi/v2/file/files/create.html), [spreadsheet v3](https://docs.qq.com/open/document/app/openapi/v3/sheet/overview.html), [document read](https://docs.qq.com/open/document/app/openapi/v3/doc/get/get.html), [document write](https://docs.qq.com/open/document/app/openapi/v3/doc/batchupdate/update.html).
