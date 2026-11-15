package protocol

import (
	"github.com/fakeyanss/jt808-server-go/internal/repository"
	"github.com/fakeyanss/jt808-server-go/internal/repository/database"
	"github.com/fakeyanss/jt808-server-go/internal/repository/memory"
)

// newTestRepositories 提供协议层测试用的内存仓储集合，避免测试依赖进程全局单例。
func newTestRepositories() repository.Repositories {
	return memory.NewRepositories()
}

// defaultTestRepositories 让测试与 database.Default() 共用同一份内存仓储：
// 既有用例通过 database.Default() 造数据与断言，协议层不再隐式取全局。
func defaultTestRepositories() repository.Repositories {
	return database.Default().Repositories
}
