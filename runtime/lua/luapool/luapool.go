package luapool

// tasks
// 1. Properly access to the context of the request (in the modules, like sql)
// 2. Concurrency
import "go.uber.org/zap"

type Pool struct {
	log *zap.Logger
}

func NewPool(log *zap.Logger) *Pool {
	return &Pool{
		log: log,
	}
}
