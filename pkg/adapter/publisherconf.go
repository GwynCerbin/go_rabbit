package adapter

import (
	"context"
	"errors"
	"fmt"
	"log"
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
	notifyChan chan *amqp091.Error
	// rabChan is the AMQP channel used for publishing with confirmations.
	rabChan *amqp091.Channel
	// cfg stores publisher settings like exchange name and routing key.
	cfg PublisherConfig
	// isClosed indicates whether the publisher has been closed.
	isClosed atomic.Bool
}

// newConfirmerPublisher initializes a ConfirmPublisher: opens a channel, enables confirm mode, and sets mimetype limits.
func newConfirmerPublisher(c *Con, notifyCh chan *amqp091.Error, cfg PublisherConfig) (*ConfirmerPublisher, error) {
	rabbitChan, err := c.connection.Channel()
	if err != nil {
		return nil, fmt.Errorf("create publish channel: %w", err)
	}

	if err := rabbitChan.Confirm(false); err != nil {
		return nil, fmt.Errorf("confirm channel for publisher: %w", err)
	}

	mimetype.SetLimit(mimeReadLimit)

	return &ConfirmerPublisher{
		con:        c,
		notifyChan: notifyCh,
		rabChan:    rabbitChan,
		cfg:        cfg,
	}, nil
}

// Publish sends data with deferred confirmation, retrying up to maxAttempts on negative confirmation.
// It uses exponential backoff starting at firstTime between confirmation attempts.
// nolint:gocognit // complex logic for confirmation retry flow
func (p *ConfirmerPublisher) Publish(ctx context.Context, data []byte) error {
	const (
		maxAttempts = 3
		firstTime   = 10 * time.Millisecond
	)

	timer := time.NewTimer(0)

	p.con.cons.Add(1)

	defer func() {
		p.con.cons.Done()
		timer.Stop()
	}()

	for attempt, timeSleep := 1, firstTime; !p.isClosed.Load(); {
		select {
		case <-p.con.stop:
			return ConnClosedError{}
		case val, ok := <-p.notifyChan:
			if err := p.reconnectInit(val, ok); err != nil && p.con.logging {
				log.Printf("reconnect init: %v", err)
			}
		case <-timer.C:
			conf, err := p.rabChan.PublishWithDeferredConfirmWithContext(setPublisherConfig(ctx, p.cfg, data))
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

			if attempt >= maxAttempts {
				return fmt.Errorf("publisher failed to confirm after %d attempts", attempt)
			}

			timer.Reset(timeSleep)
			timeSleep *= 2
			attempt++
		}
	}

	return PublisherClosedError{}
}

// reconnectInit handles AMQP errors by re-establishing the confirm channel and reconfirm mode.
func (p *ConfirmerPublisher) reconnectInit(amqpErr *amqp091.Error, isValid bool) error {
	if !isValid {
		return nil
	}

	p.con.reconnect(amqpErr)
	p.notifyChan = p.con.createNotifyChan()

	rabbitChan, err := p.con.connection.Channel()
	if err != nil {
		return fmt.Errorf("create publisher channel: %w", err)
	}

	if err := rabbitChan.Confirm(false); err != nil {
		return fmt.Errorf("recreate confirm channel for publisher: %w", err)
	}

	p.rabChan = rabbitChan

	return nil
}

// Close marks the confirmer publisher as closed and closes the AMQP channel.
func (p *ConfirmerPublisher) Close() error {
	p.isClosed.Store(true)

	if err := p.rabChan.Close(); err != nil {
		return fmt.Errorf("close publisher channel: %w", err)
	}

	return nil
}
