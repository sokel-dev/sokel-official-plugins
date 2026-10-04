# claude-code — Claude Code 插件（第一方内置目录）

把**本机装的 Claude Code** 接进工作流：4 个操作（执行任务 / 继续任务 / 清理工作树 /
health_check）。任务操作声明 `Stream: true`，过程实时回传。说明书
[docs/claude-code.md](docs/claude-code.md)。

**部署约束**：必须跑在「能访问目标 GitLab + 装了 `claude` + 有磁盘」的机器上。

## 为什么是 CLI 子进程

Claude Agent SDK（Claude Code 的库形态）只有 Python / TypeScript 绑定，**没有 Go 的**。
对 Go 宿主，CLI 就是官方路径。CLI 没有 `--cwd`，但 Go 的 `cmd.Dir` 正好补上。

## 坑（改代码前先读）

- **开关必须照实装核对，不能照文档抄**：`--bare` 和 `--max-turns` 在 2.1.38 **不存在**
  （文档里有）；`--allowedTools` 是驼峰；工具过滤是**冒号**语法 `Bash(git:*)` 不是空格。
  成本上限用 `--max-budget-usd`。测试钉着这几个。
- **result 行是唯一可信的终态**，退出码只说明「进程怎么没的」：0 正常、2 撞成本上限、
  130/143 被信号打断——撞上限那次其实有产出，当失败处理就把已经花掉的钱扔了。
- **用 bufio.Reader 不用 Scanner**：`init` 那行动辄几十 KB（工具/技能/插件清单全在里面），
  Scanner 默认 64KB 上限会 "token too long" 把整条流判死。
- **只解三类行**（system/init、assistant、result），其余忽略。CC 还会吐 hook/mcp 噪音行，
  硬性穷举类型早晚被新版本的新类型打断；非 JSON 的脏行也直接丢，不让一行脏数据断掉整条流。
- **代理只给 CC，git 一律直连**：典型部署是「内网 GitLab + 需要代理连 Anthropic」，
  两者出站路径相反。把 `http_proxy` 也塞给 git，clone 内网仓库会卡在代理上超时，
  而报错长得像网络抖动。`gitEnv()` 负责剥干净——注意别把 `GOPROXY` 一起剥了。
- **令牌只走命令行不进 .git/config**：clone 后立刻 `remote set-url` 换回干净地址。
  工作树是 CC 能读的目录，令牌躺在那儿等于交出去了。测试钉着。
- **同名分支复用工作树且不 reset**：「复用」的语义是接着上次干；reset 会把上一轮的改动
  悄悄抹掉——没有任何报错，只是活白干了。
- **清理走 `git worktree remove` 不是 rm -rf**：后者在仓库里留悬空记录，下次同名分支
  `worktree add` 直接报 already registered。
- **会话接管的边界是「同机 + 同 OS 用户」**：CC 把会话写在
  `~/.claude/projects/<cwd 转义>/<id>.jsonl`，权限 0600。插件跑在哪个账号下，
  就只有那个账号能 `--resume`。所以出参必须给出 worktree 路径——不然人知道会话 id
  也不知道该 cd 去哪。
- **接管外部目录的闸在部署环境上**（`SOKEL_CC_ALLOW_EXTERNAL_DIRS`，默认关）：那是唯一一处让工作流
  入参决定 CC 在哪干活的地方，等于把爆炸半径从工作区扩到整机。默认必须是拒绝，测试钉着。
  外部目录**不给推送**——远端和分支归人管，插件猜一个推上去太危险。
- **API Key 是可选的**，且空值**绝不能注入环境**：`ANTHROPIC_API_KEY=` 摆在那儿会盖掉
  机器上 CC 的登录态，把「用订阅额度」这条合法路径堵死，报错还很难懂。测试钉着。
- **布尔开关的默认值要靠字面意思成立**：清理用 `include_dirty`（连脏的一起删）而不是
  `keep_dirty`（跳过脏的）——布尔留空就是 false，前者「没填=保护」天然成立，
  后者还得去猜作者填没填过。同一个 0/缺省坑的第三次。
- 会话归 CC 自己管（`~/.claude/projects/`），清理工作树**不动它**——否则会误伤同机手工会话。
- **凭证只放 API Key，其余全走进程环境变量**（env.go）。GitLab 地址/令牌、工作区、
  claude 路径、代理曾经都在凭证里——那是把部署配置塞进了凭证：同一个令牌要在 gitlab
  插件和这里各配一份（轮换必漏一处），换机器部署要改凭证，而且凭证是**工作流作者**选的、
  机器能碰哪些目录该由**运维**定。分工是一条线：凭证=用谁的额度，环境=这台机器能干什么。

## 开发

```bash
go generate ./... && go build ./... && go vet ./... && go test -race ./...
```

测试夹具是**实机 2.1.38 的 stream-json 原文**——合成语料在这里会骗人（字段名、嵌套层级、
哪一行才带 session_id 都猜不准）。
