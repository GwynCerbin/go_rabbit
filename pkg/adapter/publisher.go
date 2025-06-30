// SPDX-License-Identifier: MIT
// Copyright © 2024–2026 Alexander Demin

package adapter

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync/atomic"

	"github.com/gabriel-vasile/mimetype"
	"github.com/google/uuid"
	"github.com/rabbitmq/amqp091-go"
)

// Publisher handles message publication to RabbitMQ with reconnection support.
// It manages the AMQP channel, publisher configuration, and error notifications.
type Publisher struct {
	// con is the parent connection wrapper for reconnection logic.
	con *Con
	// notifyChan receives connection-close notifications for reconnection.
	notifyChan chan *amqp091.Error
	// rabChan is the AMQP channel used for publishing messages.
	rabChan *amqp091.Channel
	// cfg stores publisher settings like exchange name, routing key, and AppId.
	cfg PublisherConfig
	// isClosed indicates whether the publisher has been closed.
	isClosed atomic.Bool
}

// newPublisher initializes a Publisher: opens a dedicated channel and sets up mimetype detection.
func newPublisher(c *Con, notifyCh chan *amqp091.Error, cfg PublisherConfig) (*Publisher, error) {
	rabbitChan, err := c.connection.Channel()
	if err != nil {
		return nil, fmt.Errorf("create publish channel: %w", err)
	}

	mimetype.SetLimit(mimeReadLimit)

	return &Publisher{
		con:        c,
		notifyChan: notifyCh,
		rabChan:    rabbitChan,
		cfg:        cfg,
	}, nil
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
		case val, ok := <-p.notifyChan:
			if err := p.reconnectInit(val, ok); err != nil && p.con.logging {
				log.Printf("reconnect init: %v", err)
			}
		default:
			if err := p.rabChan.PublishWithContext(setPublisherConfig(ctx, p.cfg, data)); err != nil {
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
//
//nolint:gocritic // returning multiple values is justified in this context
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

	p.con.reconnect(amqpErr)
	p.notifyChan = p.con.createNotifyChan()

	rabbitChan, err := p.con.connection.Channel()
	if err != nil {
		return fmt.Errorf("create publisher channel: %w", err)
	}

	p.rabChan = rabbitChan

	return nil
}

// Close marks the publisher as closed and closes the AMQP channel.
func (p *Publisher) Close() error {
	p.isClosed.Store(true)

	if err := p.rabChan.Close(); err != nil {
		return fmt.Errorf("close publisher channel: %w", err)
	}

	return nil
}
