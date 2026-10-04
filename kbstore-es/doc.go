package main

// 使用说明：真的 markdown 文件，编译期 embed 进来（见 docs/dev-playbook.md §5.0.0）。
// 随注册握手上报给平台，界面上「使用说明」看到的就是它。

import _ "embed"

//go:embed docs/kbstore-es.md
var usageDoc string
