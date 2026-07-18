package subagent

import "sync"

type ConcurrencyPool struct {
	mu             sync.Mutex
	running        int
	maxConcurrency int
	queue          []func()
}

func NewConcurrencyPool(maxConcurrency int) *ConcurrencyPool {
	if maxConcurrency < 0 {
		maxConcurrency = 3
	}

	return &ConcurrencyPool{
		maxConcurrency: maxConcurrency,
		queue:          make([]func(), 0),
	}
}

func (cp *ConcurrencyPool) Run(fn func() (interface{}, error)) (interface{}, error) {
	cp.acquire()
	defer cp.release()
	return fn()
}

func (cp *ConcurrencyPool) acquire() {

	cp.mu.Lock()
	defer cp.mu.Unlock()

	if cp.running < cp.maxConcurrency {
		cp.running++
		return
	}
	done := make(chan struct{})
	cp.queue = append(cp.queue, func() {
		close(done)
	})
	cp.mu.Unlock()

	<-done
	cp.mu.Lock()
	cp.running++
}

func (cp *ConcurrencyPool) release() {
	cp.mu.Lock()
	defer cp.mu.Unlock()

	cp.running--

	if len(cp.queue) > 0 {
		next := cp.queue[0]
		cp.queue = cp.queue[1:]
		cp.running++
		go next()
	}

}

func (cp *ConcurrencyPool) Active() int {
	cp.mu.Lock()
	defer cp.mu.Unlock()

	return cp.running
}

func (cp *ConcurrencyPool) Pending() int {
	cp.mu.Lock()
	defer cp.mu.Unlock()
	return len(cp.queue)
}
