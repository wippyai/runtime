package host

//
//type workerPool struct {
//	workers int
//	workCh  chan stepWork
//	log     *zap.Logger
//	wg      sync.WaitGroup
//}
//
//func newWorkerPool(workers int, workCh chan stepWork, log *zap.Logger) *workerPool {
//	return &workerPool{
//		workers: workers,
//		workCh:  workCh,
//		log:     log,
//	}
//}
//
//func (p *workerPool) start(ctx context.Context) {
//	for i := 0; i < p.workers; i++ {
//		p.wg.Add(1)
//		go p.worker(ctx)
//	}
//}
//
//func (p *workerPool) stop() {
//	p.wg.Wait()
//}
//
//func (p *workerPool) worker(ctx context.Context) {
//	defer p.wg.Done()
//
//	for {
//		select {
//		case <-ctx.Done():
//			return
//
//		case work := <-p.workCh:
//			// Get process instance
//			// Note: The host instance is accessible from the manager
//			if instance, ok := h.processes.Load(work.pid); ok {
//				proc := instance.(*processInstance)
//
//				// Execute step
//				if err := proc.process.Step(); err != nil {
//					if err == process.ErrTerminated {
//						// Process completed, clean up
//						h.commandCh <- stopCommand{pid: work.pid}
//					} else {
//						p.log.Error("process step failed",
//							zap.String("pid", work.pid.String()),
//							zap.Error(err))
//					}
//				}
//			}
//		}
//	}
//}
