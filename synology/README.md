# synology — NAS 文件变动监听插件

监听 NAS 卷目录的文件变动（新增/修改/删除），推事件给平台触发工作流；配套 read_file /
list_dir / move_file / delete_file 操作完成「入库 → 处理 → 归档」闭环。

## 架构与凭证模型

- **凭证 = 监听配置**：`path`（容器内子目录）+ `include`（扩展名过滤）+ `ignore`（追加忽略）+
  `settle_seconds`（落定秒数）。**一个凭证 = 一个受监听目录 = 一个 source 实例**（进程内 goroutine）。
- 多个目录 → 建多个凭证；单容器全带；多容器部署时平台自动按凭证分片（per-credential source 架构）。
- 事件只带元数据（path/name/ext/dir/size/mtime + credential_id 自动回带）；工作流里用
  `read_file` 按需取文件（产出平台文件引用，直接接 PDF 解析/LLM 文件参数）。
- 全部操作路径 jail 在凭证 `path` 内，越界拒绝。

## 事件

| 事件 | 触发时机 | 专属字段 |
|------|---------|---------|
| file_created | 新文件**落定**后（静默 settle 秒且大小稳定） | size / mtime |
| file_changed | 已有文件被改写并落定 | size / mtime |
| file_deleted | 文件被删除或移出监听树 | — |

公共字段：`path` / `name` / `ext` / `dir`（+平台平铺的 `credential_id`——回复/处理节点动态凭证绑它）。

落定检测的意义：SMB 客户端保存 = 临时文件+改名，大文件拷贝 = 创建+持续写入；不等落定，
工作流会拿到写了一半的文件。默认 2 秒，拷大文件的目录可在凭证里调大。

内置忽略（恒生效）：`@eaDir`、`#recycle`、`#snapshot`、`@*` 系统目录、`._*`（AppleDouble）、
`~$*`（Office 锁）、`Thumbs.db`、`.DS_Store`、`*.tmp/.part/.crdownload` 等。

## Synology 必做（坑清单）

1. **容器必须跑在 NAS 本机**（Container Manager）。inotify 不穿网络文件系统：在别的机器上挂
   NFS/SMB 再 bind mount 是收不到事件的；跑在 NAS 上时，其他电脑经 SMB 写入由 smbd 本地落盘，
   事件正常触发。
2. **提高 inotify watch 上限**：DSM 默认 `fs.inotify.max_user_watches=8192`，目录树一大就打满
   且默认静默（本插件会在凭证状态上亮 error 提示）。`/etc/sysctl.conf` 会被 DSM 重置，用
   **控制面板 → 任务计划 → 新增 → 触发的任务(开机) → root** 执行：
   ```
   sh -c '(sleep 90 && sysctl -w fs.inotify.max_user_watches=1048576) &'
   ```
   （sleep 是避开 DSM 启动期初始化覆盖。）建完手动「运行」一次立即生效。
3. **权限**：卷内文件属主是 DSM 用户。容器缺省 root 没问题；要收紧就在 compose 里
   `user: "<uid>:<gid>"` 配成对该共享文件夹有读写权的账号。
4. 监听目录避免指向整个 `/volume1`（垃圾目录多、watch 数暴涨）；按业务子目录建凭证。

## 部署

```bash
# 1) 平台侧：插件管理新建「synology」（自定义 / nats 传输），拿默认组 token
# 2) 构建镜像（仓库根目录）
docker build -f plugin-builtin/synology/Dockerfile -t synology:latest .
# 3) 按 docker-compose.yml 样例改 env / 挂载，Container Manager 起项目
# 4) 平台凭证管理：给 synology 建凭证，path 填容器内路径（如 /watch/研报入库）
# 5) 画布：事件触发节点选 synology + 订阅 file_created → 下游 read_file（凭证绑「变量」= 触发的 credential_id）
```

## 本地开发冒烟

```bash
SOKEL_ENDPOINT=https://<平台地址>SOKEL_TOKEN=skp_xxx SOKEL_INSTANCE_ID=nas-dev go run .
# 凭证 path 指向本机任一目录，往里丢文件看事件日志
```

测试：`go test -race ./...`（含真实文件系统的落定/忽略/动态子目录用例）。
