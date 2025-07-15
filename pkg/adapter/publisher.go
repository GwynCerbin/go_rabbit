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

	"github.com/gabriel-vasile/mimetype"
	"github.com/google/uuid"
	"github.com/rabbitmq/amqp091-go"
)

// Publisher implements reliable message publishing to RabbitMQ with support for automatic reconnection.
// Internally, it manages the AMQP channel, handles connection losses, and reconfigures the channel.
// When replacing rabChan, the old channel must be closed to prevent resource leaks.
type Publisher struct {
	// con is the parent connection object with reconnection logic.
	con *Con
	// notifyChan holds the channel for connection close notifications,
	// created via con.createNotifyChan().
	// Outdated notification channels should be closed upon reconnection.
	notifyChan atomic.Value // содержит chan *amqp091.Error

	// rabChan is the current AMQP channel for publishing.
	// During reconnectInit, the old channel should be closed: p.rabChan.Close().
	rabChan atomic.Pointer[amqp091.Channel]

	// cfg holds the publisher settings: exchange name, routing key, AppId, etc.
	cfg PublisherConfig

	// isClosed indicates that Close has been called and further Publish calls should be rejected.
	isClosed atomic.Bool

	// mute serializes reconnection logic to prevent the same event from triggering multiple reconnectInit calls.
	mute sync.Mutex

	// reconCh is used to notify Publish goroutines waiting for reconnection completion.
	// Old reconCh channels are closed at the end of reconnectInit.
	reconCh atomic.Value // содержит chan struct{}
}

// newPublisher opens a new AMQP channel and sets up the content type detector.
// If the connection is not ready or Channel() returns an error, newPublisher returns an error.
func newPublisher(c *Con, cfg PublisherConfig) (*Publisher, error) {
	c.mute.RLock()
	rabbitChan, err := c.connection.Channel()
	c.mute.RUnlock()
	if err != nil {
		return nil, fmt.Errorf("create publish channel: %w", err)
	}

	mimetype.SetLimit(mimeReadLimit)

	publisher := &Publisher{
		con: c,
		cfg: cfg,
	}

	publisher.rabChan.Store(rabbitChan)

	publisher.notifyChan.Store(c.createNotifyChan())

	ch := make(chan struct{})
	close(ch)
	publisher.reconCh.Store(ch)

	return publisher, nil
}

// Publish sends data to the configured exchange and routing key.
// It handles reconnection transparently and ensures in-flight messages are tracked.
// Context is reserved for future driver enhancements, such as async or transactional publishing.
func (p *Publisher) Publish(ctx context.Context, data []byte) error {
	p.con.cons.Add(1)

	defer p.con.cons.Done()

	for !p.isClosed.Load() {
		select {
		case <-p.con.stop:
			return ConnClosedError{}
		case val, ok := <-p.notifyChan.Load().(chan *amqp091.Error):
			if err := p.reconnectInit(val, ok); err != nil && p.con.logging {
				log.Printf("reconnect init: %v", err)
			}
		default:
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-p.reconCh.Load().(chan struct{}):
			err := p.rabChan.Load().PublishWithContext(setPublisherConfig(ctx, p.cfg, data))
			if err != nil {
				if errors.Is(err, amqp091.ErrClosed) {
					continue
				}

				return err
			}

			return nil
		}
	}

	return PublisherClosedError{}
}

// setPublisherConfig maps PublisherConfig and payload into AMQP publish arguments.
func setPublisherConfig(ctx context.Context, cfg PublisherConfig, data []byte) (_ context.Context, exchange, key string, mandatory, immediate bool, msg amqp091.Publishing) {
	msg = amqp091.Publishing{
		ContentType: mimetype.Detect(data).String(),
		Body:        data,
		AppId:       cfg.AppId,
		MessageId:   uuid.NewString(),
	}

	return ctx, cfg.ExchangeName, cfg.RoutingKey, true, false, msg
}

// reconnectInit handles AMQP errors by re-establishing the publisher channel.
func (p *Publisher) reconnectInit(amqpErr *amqp091.Error, isValid bool) error {
	if !isValid {
		return nil
	}

	if !p.mute.TryLock() {
		return nil
	}

	var (
		ch = make(chan struct{})
	)

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

	p.rabChan.Store(rabbitChan)

	return nil
}

// Close marks the publisher as closed and closes the AMQP channel.
func (p *Publisher) Close() error {
	p.isClosed.Store(true)

	if err := p.rabChan.Load().Close(); err != nil && p.con.logging {
		log.Printf("close publisher channel: %v", err)
	}

	return nil
}
