package main

// 使用说明：真的 markdown 文件，编译期 embed 进来（见 docs/dev-playbook.md §5.0.0）。
// 随注册握手上报给平台，界面上「使用说明」看到的就是它——app 怎么建、作用域怎么勾、
// 每个操作花多少钱，跟着插件代码走，重新部署即更新。

import _ "embed"

//go:embed docs/x.md
var usageDoc string
