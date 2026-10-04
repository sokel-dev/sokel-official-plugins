package main

// 使用说明：真的 markdown 文件，编译期 embed 进来（见 docs/dev-playbook.md §5.0.0）。
// 随注册握手上报给平台，界面上「使用说明」看到的就是它——凭证怎么拿、有什么坑，
// 跟着插件代码走，重新部署即更新。

import _ "embed"

//go:embed docs/wechat-claw.md
var usageDoc string
