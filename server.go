// SPDX-License-Identifier: MIT
// Copyright © 2024–2026 Alexander Demin

package rabbit

import (
	"context"
	"fmt"
	"log"
	"runtime"
	"sync"
)

// LoggerFunc is a pluggable callback for error reporting.
// Users can inject any logger (zap, logrus, zerolog, etc.)
// by calling listener.SetLogger(customLogger).
type LoggerFunc func(error)

// Listener encapsulates common parameters of a message‑queue subscriber.
//   - consumer: an object that implements the broker.Consumer interface.
//   - gos: desired number of concurrent goroutines used by an Instance.
//
// Listener itself does not process messages; it acts as a factory that
// creates an Instance where the real work happens.
type Listener struct {
	consumer   Consumer
	gos        int
	loggerFunc LoggerFunc
}

// NewListener constructs a Listener with a default parallelism level of 1.
func NewListener(consumer Consumer) *Listener {
	return &Listener{
		gos:      1,
		consumer: consumer,
	}
}

// SetConcurrency sets the number of goroutines that will be spawned later
// inside an Instance. It validates the input (n >= 1) and clamps the value
// by runtime.GOMAXPROCS(0).
func (l *Listener) SetConcurrency(n int) error {
	if n < 1 {
		return fmt.Errorf("invalid goroutines count: %d", n)
	}

	l.gos = min(n, runtime.GOMAXPROCS(0))

	return nil
}

// SetLogger overrides the default stdlib logger.
// Pass nil to restore logging to the standard library’s log.Print.
func (l *Listener) SetLogger(logger LoggerFunc) {
	l.loggerFunc = logger
}

// Instance is a running listener created from Listener.
//   - workChan: buffered channel through which the dispatcher feeds
//     anonymous handler functions to the workers.
//   - wg:       WaitGroup for graceful shutdown synchronization.
//   - gos:      fixed worker pool size determined at Init() time.
//   - router:   map routingKey → handler function.
//   - consumer: same consumer object shared with the parent Listener.
type Instance struct {
	workChan   chan func()
	wg         sync.WaitGroup
	gos        int
	router     Router
	consumer   Consumer
	loggerFunc LoggerFunc
}

// Init takes a Router snapshot and returns a ready‑to‑run Instance.
// The workChan capacity is set to 1; workers drain it quickly, so
// a larger buffer is rarely needed. To start with another router,
// create a new Instance instead of mutating the old one.
func (l *Listener) Init(router Router) *Instance {
	var logger LoggerFunc = func(err error) {
		log.Print(err)
	}

	if l.loggerFunc != nil {
		logger = l.loggerFunc
	}

	return &Instance{
		workChan:   make(chan func(), 1),
		gos:        l.gos,
		router:     router,
		consumer:   l.consumer,
		loggerFunc: logger,
	}
}

// ListenAndServe starts the worker pool and enters an infinite loop that
// consumes messages from the broker. If consumer.Consume() returns an error,
// it is propagated to the caller, enabling graceful shutdown at a higher level.
func (l *Instance) ListenAndServe() error {
	if len(l.router) == 0 {
		return EmptyRoutError{}
	}

	router := make(Router, len(l.router))
	for k, v := range l.router {
		router[k] = v
	}

	clear(l.router)

	l.router = router

	for range l.gos {
		l.wg.Add(1)

		go runner(l.workChan, &l.wg)
	}

	for {
		msg, err := l.consumer.Consume()
		if err != nil {
			return err
		}

		val, ok := l.router[msg.RoutingKey()]
		if !ok {
			l.loggerFunc(fmt.Errorf("%w, routing key: %s", UnroutedMessage{}, msg.RoutingKey()))

			if err = msg.Reject(); err != nil {
				l.loggerFunc(fmt.Errorf("reject unrouted message: %w", err))
			}

			continue
		}

		l.workChan <- func() {
			val(msg)
		}
	}
}

// Shutdown initiates a graceful shutdown. It waits either for stop() to
// finish or for the context to be canceled/expired.
func (l *Instance) Shutdown(ctx context.Context) error {
	select {
	case <-l.stop():
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// stop() closes the consumer, then the workChan, waits for all workers to
// finish, and finally returns the closed channel so that receives unblock.
func (l *Instance) stop() <-chan func() {
	if err := l.consumer.Close(); err != nil {
		l.loggerFunc(fmt.Errorf("%w: %w", ConsumerCloseError{}, err))
	}

	close(l.workChan)
	l.wg.Wait()

	return l.workChan
}

// runner executes tasks from workChan and signals completion via WaitGroup.
func runner(workChan chan func(), wg *sync.WaitGroup) {
	for work := range workChan {
		work()
	}

	wg.Done()
}
