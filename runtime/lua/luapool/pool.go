package pool

// tasks
// 1. Properly access to the context of the request (in the modules, like sql)
// 2. Concurrency
import (
	"context"
	"errors"
	"runtime"
	"time"

	"github.com/ponyruntime/pony/api"
	"github.com/ponyruntime/pony/runtime/lua/luapool/vm"
	"go.uber.org/zap"
)

type PoolCfg struct {
	// Number of Virtual Machines (Lua) to be created
	NumVMs int
	// lua script code (actual lua code, tool)
	Script string
	// main function name (entry point, might be any name, not only main)
	Main string
}

type Task struct {
	Args     any
	ScriptID string
	Resp     chan<- string
}

type Pool struct {
	numWorkers int
	taskQueue  chan *Task
	stopCh     chan struct{}
	logger     *zap.Logger
	timeout    time.Duration
	modules    []api.Module
	vms        map[string]chan *vm.Vm
}

// scripts - scriptID -> script
// module - scriptID -> []Modules ?
func NewLuaPool(log *zap.Logger, scripts map[string]*PoolCfg, options ...Options) (*Pool, error) {
	lp := &Pool{
		numWorkers: runtime.NumCPU(),
	}

	// apply options
	for _, opt := range options {
		opt(lp)
	}

	vms := make(map[string]chan *vm.Vm)
	for scriptID, cfg := range scripts {
		log.Debug("creating vms pool", zap.String("scriptID", scriptID), zap.Int("number of VMs to create", cfg.NumVMs))
		ch := make(chan *vm.Vm, cfg.NumVMs)
		for i := 0; i < cfg.NumVMs; i++ {
			log.Debug("creating vm", zap.String("scriptID", scriptID), zap.String("script", cfg.Script), zap.String("main", cfg.Main))
			vm, err := vm.New(log, cfg.Script, cfg.Main, lp.modules...)
			if err != nil {
				return nil, err
			}

			// save vm
			// we have to use select to avoid blocking and inconsistency between number of vms and channel size
			select {
			case ch <- vm:
			default:
				log.Error("failed to save vm")
				return nil, errors.New("failed to save vm, critical error")
			}
		}

		vms[scriptID] = ch
	}

	if lp.timeout == 0 {
		lp.timeout = time.Minute * 30
	}

	lp.logger = log
	lp.vms = vms
	lp.stopCh = make(chan struct{})
	lp.taskQueue = make(chan *Task, lp.numWorkers)

	lp.poll()

	return lp, nil
}

// Queue adds task to the queue
func (w *Pool) Queue(task *Task) {
	w.taskQueue <- task
}

// here is the actual work happens
func (w *Pool) do(v *vm.Vm, task *Task) error {
	if task == nil {
		w.logger.Error("task is nil")
		return nil
	}

	w.logger.Info("executing script on VM", zap.Any("sid", task.ScriptID), zap.Any("args", task.Args))

	res, err := v.Execute(context.Background(), task.Args)
	if err != nil {
		return err
	}

	select {
	case task.Resp <- res:
		// send response
		w.logger.Info("work done, response was sent", zap.String("result", res))
	default:
		w.logger.Error("failed to send the response")
	}

	return nil
}

// Stop sends stop signal to every worker in the pool
func (w *Pool) Stop() {
	w.stopCh <- struct{}{}
}

// private ---

func (w *Pool) poll() {
	go func() {
		for {
			select {
			case work := <-w.taskQueue:
				go func() {
					w.logger.Info("work received")
					// get vms
					select {
					case <-time.After(w.timeout):
						w.logger.Error("task timed out", zap.String("scriptID", work.ScriptID))
						// get the VM to execute the script
					case vm := <-w.vms[work.ScriptID]:
						// execute the script
						err := w.do(vm, work)
						if err != nil {
							w.logger.Error("failed to execute script", zap.Error(err))
						}
						w.vms[work.ScriptID] <- vm
					}
				}()
			case <-w.stopCh:
				w.logger.Info("luapool stopped")
			}
		}
	}()
}
