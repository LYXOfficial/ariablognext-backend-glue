// Package glue 只做一件事：把仓库根的 config.yml 编进二进制。
//
// 为什么：Vercel 的 Go serverless 产物只包含编译后的二进制，
// 仓库里的 config.yml 不会进函数包——线上启动时按路径读文件必然
// `no such file or directory`（上线时踩过）。embed 是编译期拷贝，
// 线上拿默认配置、本地仍然以磁盘文件为准（文件优先，见 internal/config）。
package glue

import _ "embed"

//go:embed config.yml
var ConfigYAML []byte
