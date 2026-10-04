# xueqiu — 雪球发帖插件（**非官方接口**）

雪球没有官方发布 API（它的开放平台只有行情，见
[docs/social-publishing-plugins.md](../../docs/social-publishing-plugins.md) §4）。
本插件调的是**网页端自己在用的私有接口**——纯 HTTP，不跑浏览器。

面向用户的说明书是 [docs/xueqiu.md](docs/xueqiu.md)。

## 两套接口，两个子站

**短帖走 `xueqiu.com`，长文走 `mp.xueqiu.com`——同一份 cookie，接口完全不同。**

| 用途 | 请求 | 来源 |
|---|---|---|
| 发短帖 | `POST xueqiu.com/statuses/update.json`，表单 `status`(HTML) / **`session_token`** / `ai_disclose` / `allow_reward` | 用户抓包 |
| 短帖传图 | `POST xueqiu.com/photo/upload.json`，multipart `file` | 用户抓包 |
| **写长文（存草稿）** | `POST mp.xueqiu.com/xq/statuses/draft/save.json`，表单 `title` / `text`(HTML) / `is_private`——**不要 session_token、没见风控参数** | 参照 wechatsync 的 xueqiu driver |
| 长文传图 | `POST mp.xueqiu.com/xq/photo/upload.json` → 回 `{url, filename}` 两段，要自己拼 | 同上 |
| 登录态 | `GET mp.xueqiu.com/write/`，页面里有 `window.UOM_CURRENTUSER` → 登录态 + uid + 昵称 | 同上 |

> 用户最早给的 `GET /etc/private_fund/state.json` 也能当登录态探测，但写作页那条**语义更直接**
> （它就是「你能不能写」这件事本身），还顺带给出 uid 与昵称，所以 `health_check` 用后者。

**长文才是投研内容该去的地方**，而且它那套接口干净得多——没有 session_token 与风控参数这两个
最容易坏的东西。短帖适合一句话观点。

**长文产出的是草稿，不是已发布**：`save.json` 落草稿箱，最后一步「发布」留给人在网页上点。
这既是接口本身的语义，也正好是合规上更稳的形态——自动写、人工发。

四个头是雪球认人的关键：`Cookie` / `User-Agent` / `X-Requested-With: XMLHttpRequest` /
`Referer`+`Origin`。少一个都可能被 WAF 拦，而它回的是**一整页 HTML**，与登录态无关。

## 五条设计判断

1. **应答形状是猜的 → 宽松提取**。没有文档，钉死键名等于把「雪球改一次字段名」变成
   「全线解析失败」。图片地址按**内容**找（含图床域名的那个字符串），帖子 id 递归按键名找。
2. **失败有三种长相**：HTTP 403/401、JSON 信封里的 `error_code`、以及被风控拦时的整页 HTML。
   第三种最坑——按 JSON 解只会得到「解析失败」，所以 `translate` 专门认它并告诉人去粘风控参数。
3. **`session_token` 三级回退**：凭证里粘的 → 从首页抓 → 报错并**告诉人去哪儿复制**。
   不做的话就是发一个必然被拒的请求。
4. **正文先转义再拼**。纯文本里一个 `<` 就能冲掉整段结构；判「已经是 HTML」要求形如
   `<p …>` 的完整标签，只看 `<p` 的话「A<B」也会被放行（有测试钉住）。
5. **`ai_disclose` 是显式入参**，不写死 0。内容经 LLM 改写过就该置 1——这压在合规线上
   （[GEO 方案](../../docs/geo-platform.md) §6），不是可选项。

## 两个未验证的地方（**只影响短帖那条路**）

抓包时它们都在，但**没验证过是否必需**（我没法从这里发真实请求）。
长文那套两样都不需要，所以要稳就先用长文：

- **`md5__1038`（风控参数）**：插件默认**不带**，凭证里填了才挂上去。
  若发帖回的是一整页 HTML，就是它必需 → 粘一条进凭证。
  **万一它是 JS 现算的**，那这条路就要复刻算法或定期用浏览器取——那是唯一可能需要浏览器的地方。
- **`session_token` 的来源**：目前是从首页正则抓。抓不到时会明确让用户手工粘。

第一次真实跑通后，把结论回填到本节。

## 开发

```bash
go generate ./... && go build ./... && go vet ./... && go test -race ./...
```

测试用假雪球，钉的是：请求形状（表单字段 + 四个头）、宽松提取扛得住键名变化、
风控 HTML 要说成人话、健康检查零副作用、正文转义、token 三级回退。

## 没做的 / 注意

- **长文、评论、删帖**都没做（长文是另一个接口，还没抓到；删帖发错了去网页删更安全）。
- **这是未授权的自动化**：只发自有真实内容、走账号自身额度、低频、失败即报警。
  形态与红线见 GEO 方案 §6。
