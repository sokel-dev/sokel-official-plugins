# 网页身份、聊天入口与审批集成

这些能力扩展的是同一个企业微信自建应用。普通微信可以通过微信插件收通知、打开交互消息 H5；企业微信专有的聊天工具栏、应用身份 JSAPI、原生审批界面需要在企业微信客户端中使用。

## JS-SDK 签名

插件生成企业签名和应用签名，输出 `corp_id`、`agent_id`、`timestamp`、`nonce_str`、`signature`、`agent_signature`、`signed_url`。访问令牌与两类 `jsapi_ticket` 都留在服务器，按完整应用凭证分别缓存，并在过期前刷新；不要把 Secret 或 ticket 交给网页。

输入必须是浏览器中的完整 HTTPS 页面地址。签名保留路径转义、查询参数的原始顺序，只移除 `#` 及其后内容。页面 URL 变更后，需要重新请求签名。

如果把签名操作开放给业务网页，应由业务后端验证当前会话，并把可签名域名限制为配置好的可信域名。插件中的工作流操作本身不是一个匿名的网页签名服务；不要在前端嵌入 Sokel 管理令牌。

企业微信页面使用当前官方 `@wecom/jssdk`，注册方式如下。`loadSignedConfig` 是业务系统自己的鉴权接口，它通过后端调用插件签名操作，不是插件内置路由。

```js
import * as ww from '@wecom/jssdk'

ww.register({
  corpId: '你的 CorpID',
  agentId: 1000002,
  jsApiList: ['getContext', 'getCurExternalContact', 'sendChatMessage'],
  async getConfigSignature(url) {
    const c = await loadSignedConfig(url)
    return { timestamp: c.timestamp, nonceStr: c.nonce_str, signature: c.signature }
  },
  async getAgentConfigSignature(url) {
    const c = await loadSignedConfig(url)
    return { timestamp: c.timestamp, nonceStr: c.nonce_str, signature: c.agent_signature }
  }
})
```

普通微信的微信插件页面遵循官方旧版兼容说明，使用 `jweixin-1.2.0.js` 和 `wx.config`。不能把企业微信 SDK 的客户端能力移植到普通微信。交互消息 H5 身份识别使用 `snsapi_base` 网页授权，与 JS-SDK 签名是两个不同环节。

官方依据：[开始使用 JS-SDK](https://developer.work.weixin.qq.com/document/path/90514)、[签名和票据](https://developer.work.weixin.qq.com/document/path/90506)。

## 企业微信 Web 登录

插件构造当前官方登录地址：

```text
https://login.work.weixin.qq.com/wwlogin/sso/login?login_type=CorpApp&appid=...&agentid=...&redirect_uri=...&state=...
```

该入口用于桌面系统浏览器中的扫码登录或企业微信桌面端快速登录。企业微信、普通微信内的交互消息 H5 使用网页 OAuth 登录方式。

业务系统需要完成下面的会话管理：

1. 产生至少 16 字节、不可预测的随机 `state`，绑定发起浏览器会话；插件要求传入 16–128 字节。
2. 构造登录链接，回调地址须为 HTTPS，不能带 `#` 片段。该域名须已配置为应用 OAuth 可信域名或 Web 网页授权回调域名。
3. 回调时验证并一次性消费会话 `state`，然后把 `code` 交给后端换取 UserID。插件调用当前官方 `GET /cgi-bin/auth/getuserinfo`，不会把非成员的 OpenID 当作成员账号。
4. 为业务系统建立自己的登录会话。不能把“生成登录链接”或“获得 UserID”单独当成已经完成防重放和会话鉴权。

`code` 一次有效、5 分钟失效；扫码用户须有相应应用访问权限。官方依据：[Web 登录组件与链接](https://developer.work.weixin.qq.com/document/path/98152)、[获取登录身份](https://developer.work.weixin.qq.com/document/path/98176)。

## 聊天附件栏与聊天工具栏

这些卡片配置的是应用网页入口。管理员在应用详情中，把自己的业务 H5 URL 配置到对应入口；官方开放能力主要是页面内的客户端 JSAPI，没有可据此自动填好所有后台入口配置的通用服务端接口。

业务页面完成网页身份识别和 JS-SDK 签名后，可以读取入口上下文，按场景取得当前客户或客户群，或由成员操作分享到当前会话：

```js
const context = await ww.getContext()
// 工具栏通常为 single_chat_tools 或 group_chat_tools。
// 根据入口和用户操作调用 getCurExternalContact、getCurExternalChat、sendChatMessage。
```

当前客户、客户群接口还受应用客户联系权限、入口环境与客户端版本限制。配置一个入口不等于自动获得客户数据权限，也不等于能从服务器任意主动给普通微信联系人发消息。

插件提供后台签名能力；业务页面、页面内的交互设计及后台入口配置仍由业务系统管理。插件的交互消息页用于显示正文与自定义按钮，不提供客户资料页或聊天素材库。

官方依据：[聊天工具栏入口](https://developer.work.weixin.qq.com/document/path/91789)、[获取当前客户](https://developer.work.weixin.qq.com/document/path/100746)、[分享到当前会话](https://developer.work.weixin.qq.com/document/path/100749)。

## 两套审批能力

| 能力 | 数据归属 | 模板创建 | 发起方式 | 插件支持 |
| --- | --- | --- | --- | --- |
| 自建应用“审批接口”中的流程引擎 | 自建应用 | 应用管理后台创建 | 企业微信页面 JSAPI `thirdPartyOpenPage` | 按 `thirdNo` 查询；审批变化事件接收 |
| 企业微信“审批”应用 | 系统审批应用 | 管理后台或审批模板 API | 服务端提交申请 API | 创建/更新/读取模板、提交申请、查询单据、分页列出单据 |

两套体系的模板 ID 与单据编号不能混用。流程引擎没有对应的服务端“创建并提交”接口；插件不会用审批应用 API 冒充该能力。

`send_interactive_message` 示例中的 `approve` / `reject` 只是自定义按钮 ID，与本页的两套企业微信审批 API 没有自动关联。需要正式审批时，由业务工作流明确调用对应接口并检查结果；收集发布意见的示例不会执行发布或创建审批单。

### 审批应用的后台授权

进入企业微信管理后台的 **审批 → 可调用接口的应用**，加入当前自建应用。仍然使用同一组 CorpID、AgentID、自建应用 Secret；不需要另填系统审批应用 Secret。官方说明从 2023 年 12 月 1 日起新企业不再支持用系统应用 Secret 调用，存量企业规则另计。

申请人需要在应用可见范围内。查询列表也受可见范围限制；不能因为某页返回条目少于页大小就判断已经查询完，应继续使用返回的 `new_next_cursor`。

### 审批模板与提交 JSON

控件因企业模板而不同，插件接收官方原生 JSON，保留控件、流程、摘要等原始字段。先读取模板，取得真实控件 ID，再组装申请；不要直接照抄示例中的占位 ID。

创建模板示例：

```json
{
  "template_name": [{ "text": "版本发布审批", "lang": "zh_CN" }],
  "template_content": {
    "controls": [{
      "property": {
        "control": "Text",
        "id": "Text-01",
        "title": [{ "text": "发布内容", "lang": "zh_CN" }],
        "require": 1
      }
    }]
  }
}
```

提交示例，使用模板在后台配置好的审批流程：

```json
{
  "creator_userid": "申请人成员账号",
  "template_id": "审批应用模板 ID",
  "use_template_approver": 1,
  "apply_data": {
    "contents": [{
      "control": "Text",
      "id": "模板中的真实控件 ID",
      "value": { "text": "申请发布版本 rel-001" }
    }]
  },
  "summary_list": [{
    "summary_info": [{ "text": "版本 rel-001 发布申请", "lang": "zh_CN" }]
  }]
}
```

`use_template_approver: 0` 时需要 `process.node_list` 明确流程。模板流程中如果含“申请人自选”节点，不能直接使用模式 1。摘要为 1–3 行，每条文字不超过 20 字符。自定义表单控件的必填与类型规则由企业微信按实际模板继续校验。

创建/更新模板、提交审批都会改动真实企业数据。网络超时可能意味着上游已处理，返回结果未知时先查询核实，再决定重试，避免重复审批单。

批量查询使用字符串 Unix 时间戳、新版不透明分页游标：

```json
{
  "starttime": "1760000000",
  "endtime": "1760086400",
  "new_cursor": "",
  "size": 100,
  "filters": [{ "key": "creator", "value": "申请人成员账号" }]
}
```

跨度最多 31 天，一页最多 100 条。下一页填返回的 `new_next_cursor`，直到该值为空。

官方依据：[审批体系区别](https://developer.work.weixin.qq.com/document/path/91854)、[创建模板](https://developer.work.weixin.qq.com/document/path/97437)、[更新模板](https://developer.work.weixin.qq.com/document/path/97438)、[读取模板](https://developer.work.weixin.qq.com/document/path/91982)、[提交申请](https://developer.work.weixin.qq.com/document/path/91853)、[查询单据](https://developer.work.weixin.qq.com/document/path/91983)、[批量查询](https://developer.work.weixin.qq.com/document/path/91816)。

### 自建应用流程引擎页面集成

在自建应用后台“审批接口”创建流程模板后，由企业微信页面用户触发：

```js
await ww.invoke('thirdPartyOpenPage', {
  oaType: '10001',
  templateId: '自建应用流程模板 ID',
  thirdNo: '业务系统中唯一且不可复用的单号',
  extData: {
    fieldList: [
      { title: '发布内容', type: 'text', value: '申请发布版本 rel-001' },
      { title: '业务详情', type: 'link', value: 'https://你的域名/业务详情' }
    ]
  }
})
```

页面需要提前注册企业和应用 JS-SDK 身份，将 `thirdPartyOpenPage` 加入所需 JSAPI 列表；这是企业微信客户端能力。`oaType: '10002'` 用于查看既有审批详情。后台可按 `thirdNo` 查询当前状态，或在应用 API 接收配置中订阅 `open_approval_change` 事件。

官方依据：[自建应用审批流程引擎](https://developer.work.weixin.qq.com/document/path/90269)。

## 验证范围

新增协议测试采用官方文档示例和本地模拟上游，覆盖签名向量、票据缓存与并发、URL 构造、审批参数与分页。测试不会创建真实模板、发起真实审批或变更企业后台配置。各企业的审批授权、模板内容、聊天入口及客户端 JSAPI 还需要按实际业务页面验收。
