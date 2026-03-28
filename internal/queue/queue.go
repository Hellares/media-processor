package queue

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

type Job struct {
	ID        string
	Execute   func(ctx context.Context) (interface{}, error)
	ResultCh  chan JobResult
	CreatedAt time.Time
}

type JobResult struct {
	ID     string
	Data   interface{}
	Error  error
	Took   time.Duration
}

type WorkerPool struct {
	maxWorkers int
	jobQueue   chan Job
	wg         sync.WaitGroup
	ctx        context.Context
	cancel     context.CancelFunc
	active     int64
	processed  int64
	failed     int64
	timeout    time.Duration
}

func NewWorkerPool(maxWorkers, queueSize int, timeout time.Duration) *WorkerPool {
	ctx, cancel := context.WithCancel(context.Background())
	pool := &WorkerPool{
		maxWorkers: maxWorkers,
		jobQueue:   make(chan Job, queueSize),
		ctx:        ctx,
		cancel:     cancel,
		timeout:    timeout,
	}

	// Iniciar workers
	for i := 0; i < maxWorkers; i++ {
		pool.wg.Add(1)
		go pool.worker(i)
	}

	return pool
}

func (p *WorkerPool) worker(id int) {
	defer p.wg.Done()

	for {
		select {
		case <-p.ctx.Done():
			return
		case job, ok := <-p.jobQueue:
			if !ok {
				return
			}
			atomic.AddInt64(&p.active, 1)
			start := time.Now()

			// Ejecutar con timeout
			jobCtx, jobCancel := context.WithTimeout(p.ctx, p.timeout)
			result, err := job.Execute(jobCtx)
			jobCancel()

			took := time.Since(start)

			if err != nil {
				atomic.AddInt64(&p.failed, 1)
			} else {
				atomic.AddInt64(&p.processed, 1)
			}
			atomic.AddInt64(&p.active, -1)

			// Enviar resultado
			if job.ResultCh != nil {
				job.ResultCh <- JobResult{
					ID:   job.ID,
					Data: result,
					Error: err,
					Took: took,
				}
			}
		}
	}
}

func (p *WorkerPool) Submit(job Job) error {
	select {
	case p.jobQueue <- job:
		return nil
	default:
		return fmt.Errorf("queue full (%d/%d), try again later", len(p.jobQueue), cap(p.jobQueue))
	}
}

func (p *WorkerPool) Shutdown() {
	p.cancel()
	close(p.jobQueue)
	p.wg.Wait()
}

func (p *WorkerPool) Stats() map[string]interface{} {
	return map[string]interface{}{
		"workers":      p.maxWorkers,
		"active":       atomic.LoadInt64(&p.active),
		"queued":       len(p.jobQueue),
		"queueMax":     cap(p.jobQueue),
		"processed":    atomic.LoadInt64(&p.processed),
		"failed":       atomic.LoadInt64(&p.failed),
	}
}
