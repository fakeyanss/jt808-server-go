// Package memory 提供持久化仓储的默认内存实现。
//
// 接口与领域类型的唯一来源是父包 repository；本包通过普通 import 引用它们，
// 以保证实现代码与接口定义不出现第二份副本。内存实现是默认后端，
// 数据随进程结束释放，生产环境应根据部署形态选择 sqlite 或 mysql。
package memory
