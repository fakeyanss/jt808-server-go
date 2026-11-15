// Package repository 定义持久化仓储的业务契约，是接口的唯一来源。
//
// 分类沿用 docs/archive/storage-abstraction-design.md：B 类耐用态、C 类派生投影。
// 接口按领域分散在 *_repository.go；内存实现在 repository/memory，
// SQL 实现在 repository/sqlcore + 方言包 repository/sqlite、repository/mysql，
// 装配入口在 repository/database，SQL 驱动注册在 repository/database/sqlbackend。
//
// 扩展方式见 CONTRIBUTING.md 的“目录结构与扩展方式”。
package repository
