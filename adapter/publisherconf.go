// SPDX-License-Identifier: MIT
// Copyright © 2024–2026 Alexander Demin

package adapter

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gabriel-vasile/mimetype"
	"github.com/rabbitmq/amqp091-go"
)

// ConfirmerPublisher extends Publisher with deferred confirmation support.
// It ensures each published message is acknowledged by the broker, retrying on failures.
type ConfirmerPublisher struct {
	// con is the parent connection wrapper for reconnection logic.
	con *Con
	// notifyChan receives connection-close notifications for reconnection.
	notifyChan atomic.Value
	// rabChan is the AMQP channel used for publishing with confirmations.
	rabChan atomic.Pointer[amqp091.Channel]
	// cfg stores publisher settings like exchange name and routing key.
	cfg PublisherConfig
	// isClosed indicates whether the publisher has been closed.
	isClosed atomic.Bool
	mute     sync.Mutex
	reconCh  atomic.Value
}

// firstDelay is the initial pause for the exponential back-off.
// Value is expressed in nanoseconds: 0xFFFFF = 1 048 575 ns ≈ 1.05 ms.
// A sub-millisecond start keeps the first retry quick without
// thrashing the CPU.
const firstDelay time.Duration = 0xFFFFF

// maxDelayMask is the saturation limit for the back-off delay.
// 0x7FFFFFF = 134 217 727 ns ≈ 134 ms.
// Because it is of the form 2ⁿ−1 (all lower 27 bits set),
// you can compactly grow the delay with
//
//	delay = maxDelayMask & (delay<<1 | 1)
//
// which doubles the delay (delay<<1), guarantees it never
// becomes zero (| 1), and clips everything beyond 134 ms (&).
const maxDelayMask time.Duration = 0x7FFFFFF

// newConfirmerPublisher initializes a ConfirmPublisher: opens a channel, enables confirm mode, and sets mimetype limits.
func newConfirmerPublisher(c *Con, notifyCh chan *amqp091.Error, cfg PublisherConfig) (*ConfirmerPublisher, error) {
	c.mute.RLock()
	rabbitChan, err := c.connection.Channel()
	c.mute.RUnlock()
	if err != nil {
		return nil, fmt.Errorf("create publish channel: %w", err)
	}

	if err := rabbitChan.Confirm(false); err != nil {
		return nil, fmt.Errorf("confirm channel for publisher: %w", err)
	}

	mimetype.SetLimit(mimeReadLimit)

	publisher := &ConfirmerPublisher{
		con: c,
		cfg: cfg,
	}

	publisher.rabChan.Store(rabbitChan)

	publisher.notifyChan.Store(notifyCh)

	ch := make(chan struct{})
	close(ch)
	publisher.reconCh.Store(ch)

	return publisher, nil
}

// Publish sends data with deferred confirmation, retrying up to maxAttempts on negative confirmation.
// It uses exponential backoff starting at firstDelay between confirmation attempts.
func (p *ConfirmerPublisher) Publish(ctx context.Context, data []byte) error {
	var (
		timer = time.NewTimer(0)
		delay = firstDelay
	)

	p.con.cons.Add(1)

	defer func() {
		p.con.cons.Done()
		timer.Stop()
	}()

	for !p.isClosed.Load() {
		select {
		case <-p.con.stop:
			return ConnClosedError{}
		case val, ok := <-p.notifyChan.Load().(chan *amqp091.Error):
			if err := p.reconnectInit(val, ok); err != nil && p.con.logging {
				log.Printf("reconnect init: %v", err)
			}

			timer.Reset(0)
		default:
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-p.checkRecon(timer):
			conf, err := p.rabChan.Load().PublishWithDeferredConfirmWithContext(setPublisherConfig(ctx, p.cfg, data))
			if err != nil {
				if errors.Is(err, amqp091.ErrClosed) {
					continue
				}

				return err
			}

			success, err := conf.WaitContext(ctx)
			if err != nil {
				return err
			}

			if success {
				return nil
			}

			timer.Reset(delay)

			delay = maxDelayMask & (delay<<1 | 1)
		}
	}

	return PublisherClosedError{}
}

func (p *ConfirmerPublisher) checkRecon(timer *time.Timer) <-chan struct{} {
	<-timer.C
	return p.reconCh.Load().(chan struct{})
}

// reconnectInit handles AMQP errors by re-establishing the confirm channel and reconfirm mode.
func (p *ConfirmerPublisher) reconnectInit(amqpErr *amqp091.Error, isValid bool) error {
	if !isValid {
		return nil
	}

	if !p.mute.TryLock() {
		return nil
	}

	var ch = make(chan struct{})

	p.reconCh.Store(ch)

	defer func() {
		p.mute.Unlock()
		close(ch)
	}()

	p.con.reconnect(amqpErr)

	p.con.mute.RLock()
	p.notifyChan.Store(p.con.createNotifyChan())

	rabbitChan, err := p.con.connection.Channel()
	p.con.mute.RUnlock()
	if err != nil {
		return fmt.Errorf("create publisher channel: %w", err)
	}

	if err := rabbitChan.Confirm(false); err != nil {
		return fmt.Errorf("recreate confirm channel for publisher: %w", err)
	}

	p.rabChan.Store(rabbitChan)

	return nil
}

// Close marks the confirmer publisher as closed and closes the AMQP channel.
func (p *ConfirmerPublisher) Close() error {
	p.isClosed.Store(true)

	if err := p.rabChan.Load().Close(); err != nil {
		return fmt.Errorf("close publisher channel: %w", err)
	}

	return nil
}
