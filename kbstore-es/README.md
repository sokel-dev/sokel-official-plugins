# kbstore-es — Elasticsearch 知识库存储插件

把知识库的向量/全文存储接到自建 Elasticsearch 上。面向用户的说明书是
[docs/kbstore-es.md](docs/kbstore-es.md)。

## 定位

知识库的**第二存储**：平台默认用内置存储，接了这个插件之后，某个知识库可以改用 ES
（`plugin-builtin/kbstore-*` 与 `dev-plugins/kbstore-pgvector` 是同一类东西，
契约一致，换一个 = 换一份凭证，画布与检索链路不用改）。

## 部署上的一条要点

容器里跑时，凭证的 `es_url` 要填**服务名**（如 `http://elasticsearch:9200`）而不是
`localhost`——插件与 ES 在同一个 compose 网络里；用 host 进程方式跑时反过来。
这一条踩过：改成服务名后忘了把 host 方式那份凭证改回去，表现是「插件在线但检索全空」。

## 开发

```bash
go generate ./... && go build ./... && go vet ./... && go test -race ./...
```
