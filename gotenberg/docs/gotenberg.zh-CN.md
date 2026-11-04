# Gotenberg

通过自建 Gotenberg 服务将 HTML 和办公文档转换为 PDF，或合并 PDF 文件，无需 SaaS 账户。本版本面向 Gotenberg 8 API，已对照官方 8.39.0 文档与源码核实。Gotenberg 自身代码采用 MIT 许可证，随附转换引擎各有自己的许可证。

## 部署与认证

将必填部署环境变量 `SOKEL_GOTENBERG_URL` 设置为服务基础地址，例如 `http://gotenberg:3000` 或 `https://documents.example.org/gotenberg`。支持内网 HTTP、HTTPS 和反向代理路径前缀，地址必须能从插件进程访问。插件不会自动启动服务，也不会回退到公共服务。URL 不允许包含用户信息、查询字符串、片段、百分号编码路径或点路径段。

服务不需要认证时，将两个凭证字段都留空；否则同时填写 **Basic 认证用户名** 和 **Basic 认证密码**。这些凭证只用于向配置的服务或反向代理进行 HTTP Basic 认证。网络不可信时请使用 HTTPS。不支持任意请求头、浏览器 Cookie、Bearer 令牌或 OAuth。插件拒绝所有重定向，包括同主机重定向，因此应直接配置最终服务地址。

`health_check` 读取 `/health`，返回 `ok` 和 `message`。只有收到官方规定的 `status: up` 响应才报告成功，失败时返回 `ok=false`。即使转换接口要求认证，健康检查接口也可能公开。因此，检查成功只确认服务健康，不代表具备转换权限或支持某个具体文档。

## 操作

| 操作 | 输入与结果 |
| --- | --- |
| `health_check` | 检查配置的服务，不转换文档。 |
| `html_to_pdf` | 必填 `html`，可选 `assets`、`options` 和 `output_name`。将 HTML 作为 `index.html` 上传，返回平台 PDF 文件。 |
| `office_to_pdf` | 必填平台 `file`，可选 `output_name`。通过 LibreOffice 转换单个办公文档，返回平台 PDF 文件。 |
| `merge_pdfs` | 必填 `files` 数组，按所需顺序包含 2–20 个平台 PDF；可选 `output_name`。返回一个合并后的平台 PDF 文件。 |

所有成功的转换都通过 `file` 字段输出结果，可直接连接到接受平台文件的后续操作。输入应包含平台文件 ID 和纯文件名，不能使用本地路径或外部下载 URL。SDK 也接受小型内联文件数据。文件字节通过平台文件服务读取；本插件不会打开输入路径或下载用户提供的远程 URL。

## HTML 与素材

输入示例：

```json
{
  "html": "<!doctype html><html><meta charset='utf-8'><body><h1>Report</h1><img src='logo.png'></body></html>",
  "assets": [{"id": "<platform-file-id>", "name": "logo.png", "size": 1234}],
  "options": {"landscape": false, "print_background": true, "prefer_css_page_size": false},
  "output_name": "report.pdf"
}
```

请使用前序上传或文件操作输出的真实文件引用；上例中的 ID 和大小均为占位值。素材与 `index.html` 存放在同一个扁平目录中，应仅通过文件名引用，例如 `logo.png`，并附上文档需要的字体。文件名忽略大小写后必须唯一。`index.html`、`header.html` 和 `footer.html` 是保留名称，不能作为素材。支持中文文件名以及含引号的文件名。

三个渲染选项都是可选布尔值。显式 `false` 会原样发送；省略时使用 Gotenberg 默认值。`prefer_css_page_size` 允许优先采用 CSS `@page` 尺寸。本版本不开放任意 Chromium 参数、等待条件、请求头、Cookie、URL 转换或 JavaScript 表达式。

HTML 本身可以包含脚本和外部资源引用。这些内容由 Gotenberg 内的 Chromium 处理，不由本插件下载。请根据你的信任边界配置 Gotenberg 的出站 URL 过滤规则和网络权限。服务 Basic 凭证绝不会复制到浏览器请求头。需要自包含转换时，请使用上传素材或 data URI，避免依赖远程资源。插件不保证 HTML 文档与网络隔离。

## 办公文档转换与 PDF 顺序

支持的扩展名为 `.doc`、`.docx`、`.docm`、`.odt`、`.rtf`、`.txt`、`.xls`、`.xlsx`、`.xlsm`、`.ods`、`.csv`、`.ppt`、`.pptx`、`.pptm` 和 `.odp`。实际支持范围与排版取决于安装的 LibreOffice 引擎、字体和文档内容。不开放加密文档密码或各格式导入选项。较新 Gotenberg 会禁用宏并忽略链接内容；请嵌入图片等内容，避免依赖外部链接。

Gotenberg 按文件名合并 PDF。插件将合并输入依次上传为 `0001.pdf`、`0002.pdf` 等，确保遵循 `files` 数组顺序，即使原始文件名的排序不同或重复。原始平台文件不会被修改。合并输入必须以 PDF 标识开头，实际 PDF 解析由 Gotenberg 完成。

`output_name` 是平台输出文件名，不是服务端文件系统路径。它必须是以 `.pdf` 结尾的纯文件名。转换默认使用 `document.pdf`，合并默认使用 `merged.pdf`。插件不会采用上游 Content-Disposition 文件名。PDF 响应必须具有 `application/pdf` 媒体类型、PDF 头和文件结束标记，之后才存入平台；这只是传输完整性检查，并非完整 PDF 校验器。平台存储失败会使操作失败。

## 限制与错误

HTML 最大 2 MiB，可附带最多 20 个素材。单个输入文件以及所有文件合计均不超过 32 MiB；含 multipart 封装的请求上限为 33 MiB。输出最大 64 MiB。获取文件前检查声明大小，获取后再次检查实际字节数。SDK 会将文件读入内存，因此这些操作适用于大小受控的文档，而非大型归档。

转换操作的平台超时为 300 秒，HTTP 超时为 240 秒。Gotenberg 和反向代理的超时仍然生效，请按工作负载配置；服务可能更早拒绝请求。HTTP 错误、无效 PDF 响应、数据超限和平台文件错误都会产生脱敏错误信息。429 响应会报告受限的重试等待时间，但不会自动重试。错误不包含上游响应正文、凭证或连接 URL。

任何服务调用都不会自动重试。调用方可以明确决定重复失败的转换，但这会再次消耗转换资源，也可能存储另一个输出文件。本版本不提供 PDF/A、加密、页面拆分、webhook、文件系统访问或服务部署管理。仓库分别记录 fixture 测试和真实服务验收。

## 官方参考

- [Gotenberg 安装](https://gotenberg.dev/docs/getting-started/installation)
- [HTML 转 PDF](https://gotenberg.dev/docs/convert-with-chromium/convert-html-to-pdf)
- [LibreOffice 转换](https://gotenberg.dev/docs/convert-with-libreoffice/convert-to-pdf)
- [合并 PDF](https://gotenberg.dev/docs/manipulate-pdfs/merge-pdfs)
- [健康检查端点](https://gotenberg.dev/docs/system/get-health-check)
- [Gotenberg 8.39.0 源码与许可证](https://github.com/gotenberg/gotenberg/tree/v8.39.0)
