# WPS365 Cloud Documents

Sokel 官方插件，版本 0.1.1。共七项操作，使用公开的 `sokel-plugin-sdk v0.7.8`；界面默认英文，并提供完整简体中文翻译。

[使用指南](docs/wps.md)说明了凭证、权限、API 边界与操作示例；平台可通过 `locales/zh-CN.json` 展示全文中文指南。

## 开发

模块独立构建，无本地 replace 或私有 Go 依赖。运行英文 README 中的生成、race 测试、vet、build 和 codegen check 命令。

`manifest.yml` 为契约来源，不要手工修改 `zz_*.go`。运行时与 manifest 版本均为 0.1.1。部署环境的 `ENDPOINT` 和 `TOKEN` 用于连接 Sokel；服务凭证通过平台凭证表单设置。服务 API 域名固定。

测试使用合成 HTTP 夹具，不需要真实凭证，也不执行真实写入。覆盖请求字节、分页、凭证边界、异常响应、限流和生成处理器。本地验证不代表真实服务验收。任何临时首次发布目录夹具均仅为测试输入，不声称发布成功或拥有真实镜像摘要。
