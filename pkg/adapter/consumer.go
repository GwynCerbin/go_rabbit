// SPDX-License-Identifier: MIT
// Copyright © 2024–2026 Alexander Demin

package adapter

import (
	"errors"
	"fmt"
	"log"
	"sync"
	"sync/atomic"

	"github.com/GwynCerbin/go_rabbit/pkg/broker"

	"github.com/rabbitmq/amqp091-go"
)

// Consumer manages message consumption from a RabbitMQ queue.
// It holds the channel, delivery stream, and reconnection notifications.
type Consumer struct {
	// con is the parent connection wrapper for reconnection logic.
	con *Con
	// rabChan is the AMQP channel used for consuming messages.
	rabChan *amqp091.Channel
	// notifyChan receives connection-close notifications for reconnection.
	notifyChan chan *amqp091.Error
	// workChan streams incoming deliveries to be processed.
	workChan <-chan amqp091.Delivery
	// cfg stores consumer configuration such as queue name and args.
	cfg ConsumerConfig
	// isClosed indicates whether the consumer has been closed.
	isClosed atomic.Bool

	jobs *sync.WaitGroup
}

// newConsumer initializes a Consumer: opens a channel, starts consuming, and returns the instance.
func newConsumer(c *Con, notifyCh chan *amqp091.Error, cfg ConsumerConfig) (*Consumer, error) {
	c.mute.RLock()
	rabbitChan, err := c.connection.Channel()
	c.mute.RUnlock()
	if err != nil {
		return nil, fmt.Errorf("failed to create channel: %w", err)
	}

	msgCh, err := rabbitChan.Consume(setConsumerConfig(cfg))
	if err != nil {
		return nil, fmt.Errorf("failed to create consumer channel: %w", err)
	}

	return &Consumer{
		con:        c,
		rabChan:    rabbitChan,
		notifyChan: notifyCh,
		cfg:        cfg,
		workChan:   msgCh,
		jobs:       new(sync.WaitGroup),
	}, nil
}

// setConsumerConfig maps our ConsumerConfig to the parameters expected by amqp091.Channel.Consume.
//
//nolint:gocritic // returning multiple values is justified in this context
func setConsumerConfig(cfg ConsumerConfig) (queue, consumer string, autoAck, exclusive, noLocal, noWait bool, args amqp091.Table) {
	return cfg.QueueName, "", autoAck, exclusive, noLocal, noWait, cfg.Args
}

// Consume retrieves the next broker.Message or an error ConnClosedError if the consumer or connection is closed.
// It handles reconnection transparently using notifyChan and Con.reconnect.
func (c *Consumer) Consume() (broker.Message, error) {
	c.con.cons.Add(1)
	defer c.con.cons.Done()

	for !c.isClosed.Load() {
		select {
		case <-c.con.stop:
			return nil, ConnClosedError{}
		case val, ok := <-c.notifyChan:
			if err := c.reconnectInit(val, ok); err != nil && c.con.logging {
				log.Printf("reconnect init: %v", err)
			}
		case val, ok := <-c.workChan:
			if !ok {
				continue
			}
			c.jobs.Add(1)

			return broker.Message(&Message{
				deliver: val,
				wg:      c.jobs,
			}), nil
		}
	}

	c.jobs.Wait()

	return nil, ConsumerClosedError{}
}

// reconnectInit handles AMQP errors by re-establishing the consumer channel and re-subscribing.
func (c *Consumer) reconnectInit(amqpErr *amqp091.Error, isValid bool) error {
	if !isValid {
		return nil
	}

	c.con.reconnect(amqpErr)

	c.con.mute.RLock()
	c.notifyChan = c.con.createNotifyChan()

	rabbitChan, err := c.con.connection.Channel()
	if err != nil {
		return fmt.Errorf("create consumer channel: %w", err)
	}
	c.con.mute.RUnlock()

	msgCh, err := rabbitChan.Consume(setConsumerConfig(c.cfg))
	if err != nil {
		return fmt.Errorf("create consumer msg channel: %w", err)
	}

	c.rabChan = rabbitChan
	c.workChan = msgCh

	return nil
}

// Close stops message consumption and closes the AMQP channel.
// It is safe to call after Consume has returned.
func (c *Consumer) Close() error {
	c.isClosed.Store(true)

	c.jobs.Wait()

	if err := c.rabChan.Close(); err != nil {
		if errors.Is(err, ConnClosedError{}) {
			return nil
		}

		return fmt.Errorf("close consumer channel: %w", err)
	}

	return nil
}
