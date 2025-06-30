// SPDX-License-Identifier: MIT
// Copyright © 2024–2026 Alexander Demin

package adapter

import (
	"fmt"
	"log"
	"net/url"
	"sync"
	"time"

	"github.com/GwynCerbin/go_rabbit/pkg/broker"

	"github.com/rabbitmq/amqp091-go"
)

// Con encapsulates a RabbitMQ connection with automatic reconnection logic.
// It manages the underlying AMQP connection, reconnection routines, and consumer/publisher lifecycle.
type Con struct {
	// connection holds the active AMQP connection.
	connection *amqp091.Connection
	// url is the target URI for dialing the broker.
	url *url.URL
	// stop signals the reconnection loop to exit.
	stop chan struct{}
	// once ensures reconnection is triggered only once per connection loss.
	once *sync.Once
	// wgRecon waits for the reconnection routine to finish before proceeding.
	wgRecon sync.WaitGroup
	// cfg stores the AMQP client configuration.
	cfg amqp091.Config
	// cons tracks active consumers to allow graceful shutdown.
	cons sync.WaitGroup
	// logging toggles verbose log output for debugging.
	logging bool
	// maxReconnectTime caps the exponential backoff delay in seconds.
	maxReconnectTime int64
}

// Dial establishes an AMQP connection using the provided client configuration.
// It returns a Con instance ready to declare exchanges, queues, and create publishers/consumers.
func Dial(cfg *Client) (*Con, error) {
	const stdMaxTime = 32 * time.Second

	var (
		clientCfg = amqp091.Config{
			SASL: []amqp091.Authentication{
				&amqp091.PlainAuth{Username: cfg.Username, Password: cfg.Password},
			},
			Vhost:      cfg.VHost,
			Properties: cfg.Properties,
			Heartbeat:  cfg.TcpHeartBeat,
		}
		maxTime = int64(cfg.MaxReconnectTime.Seconds())
		uri     = &url.URL{
			Scheme: "amqp",
			Host:   cfg.Host,
		}
	)

	if maxTime == 0 {
		maxTime = int64(stdMaxTime.Seconds())
	}

	con, err := amqp091.DialConfig(uri.String(), clientCfg)
	if err != nil {
		return nil, fmt.Errorf("dial amqp091: %w", err)
	}

	return &Con{
		connection:       con,
		url:              uri,
		cfg:              clientCfg,
		stop:             make(chan struct{}),
		maxReconnectTime: maxTime,
		once:             new(sync.Once),
		logging:          cfg.Logging,
	}, nil
}

// reconnect triggers a one-time reconnection sequence upon connection loss.
// It ensures multiple errors in quick succession do not spawn multiple loops.
func (c *Con) reconnect(err error) {
	c.once.Do(func() {
		c.wgRecon.Add(1)

		if c.logging {
			log.Printf("rabbit connection lost: %v", err)
		}

		c.reconnectLoop()
		c.wgRecon.Done()
	})
	c.wgRecon.Wait()
}

// ReconnectLoop attempts to re-establish the AMQP connection using exponential backoff.
// It doubles the wait time after each failed attempt, capped by maxReconnectTime.
// The loop exits when stop is closed or a new connection is successfully made.
func (c *Con) reconnectLoop() {
	for waitTime, attempt, maxTime := int64(1), 1, c.maxReconnectTime; true; waitTime, attempt = waitTime<<1, attempt+1 {
		if waitTime > maxTime {
			waitTime = maxTime
		}
		select {
		case <-c.stop:
			return
		case <-time.After(time.Duration(waitTime) * time.Second):
			if c.logging {
				log.Printf("rabbit reconnect attempt %d", attempt)
			}

			con, err := amqp091.DialConfig(c.url.String(), c.cfg)
			if err != nil {
				continue
			}

			c.connection = con
			if c.logging {
				log.Print("rabbit reconnect success")
			}

			c.once = new(sync.Once)

			return
		}
	}
}

// DeclareExchange opens a channel, declares an exchange, and closes the channel.
func (c *Con) DeclareExchange(cfg *ExchangeDeclare) error {
	ch, err := c.connection.Channel()
	if err != nil {
		return fmt.Errorf("create channel: %w", err)
	}

	defer func() {
		if err := ch.Close(); err != nil && c.logging {
			log.Printf("close channel: %v", err)
		}
	}()

	if err = ch.ExchangeDeclare(cfg.Name, cfg.Type, cfg.Durable, cfg.AutoDelete, cfg.Internal, false, cfg.Args); err != nil {
		return fmt.Errorf("declare exchange: %w", err)
	}

	return nil
}

// QueueDeclareAndBind declares a queue and optionally binds it to an exchange.
func (c *Con) QueueDeclareAndBind(cfg *QueueDeclareAndBind) error {
	ch, err := c.connection.Channel()
	if err != nil {
		return fmt.Errorf("create channel: %w", err)
	}

	defer func() {
		if err := ch.Close(); err != nil && c.logging {
			log.Printf("close channel: %v", err)
		}
	}()

	queue, err := ch.QueueDeclare(cfg.Name, cfg.Durable, cfg.AutoDelete, cfg.Exclusive, false, cfg.Args)
	if err != nil {
		return fmt.Errorf("create queue: %w", err)
	}

	if cfg.NoBind {
		return nil
	}

	if err = ch.QueueBind(queue.Name, cfg.RoutingKey, cfg.ExchangeName, false, cfg.BindArgs); err != nil {
		return fmt.Errorf("create queue binding: %w", err)
	}

	return nil
}

// DeleteExchange removes an existing exchange by name.
func (c *Con) DeleteExchange(name string) error {
	ch, err := c.connection.Channel()
	if err != nil {
		return fmt.Errorf("create channel: %w", err)
	}

	defer func() {
		if err := ch.Close(); err != nil && c.logging {
			log.Printf("close channel: %v", err)
		}
	}()

	if err = ch.ExchangeDelete(name, false, false); err != nil {
		return fmt.Errorf("delete exchange: %w", err)
	}

	return nil
}

// DeleteQueue removes an existing queue by name.
func (c *Con) DeleteQueue(name string) error {
	ch, err := c.connection.Channel()
	if err != nil {
		return fmt.Errorf("create channel: %w", err)
	}

	defer func() {
		if err := ch.Close(); err != nil && c.logging {
			log.Printf("close channel: %v", err)
		}
	}()

	if _, err = ch.QueueDelete(name, false, false, false); err != nil {
		return fmt.Errorf("delete queue: %w", err)
	}

	return nil
}

// CreateConsumer returns a new broker.Consumer instance or an error ConsumerConfEmptyError if the configuration is nil.
func (c *Con) CreateConsumer(cfg *ConsumerConfig) (broker.Consumer, error) {
	if cfg == nil {
		return nil, ConsumerConfEmptyError{}
	}

	return newConsumer(c, c.createNotifyChan(), *cfg)
}

// CreatePublisher returns a new broker.Publisher instance or an error PublisherConfEmptyError if the configuration is nil.
func (c *Con) CreatePublisher(cfg *PublisherConfig) (broker.Publisher, error) {
	if cfg == nil {
		return nil, PublisherConfEmptyError{}
	}

	return newPublisher(c, c.createNotifyChan(), *cfg)
}

// CreatePublisherWithConfirmation returns a broker.Publisher that supports confirmation acknowledgments.
func (c *Con) CreatePublisherWithConfirmation(cfg *PublisherConfig) (broker.Publisher, error) {
	if cfg == nil {
		return nil, PublisherConfEmptyError{}
	}

	return newConfirmerPublisher(c, c.createNotifyChan(), *cfg)
}

// createNotifyChan returns a channel to receive AMQP connection close notifications.
func (c *Con) createNotifyChan() chan *amqp091.Error {
	return c.connection.NotifyClose(make(chan *amqp091.Error, 1))
}

// Close gracefully shuts down the connection, waiting for consumers to finish before closing.
func (c *Con) Close() error {
	close(c.stop)

	c.cons.Wait()

	if err := c.connection.Close(); err != nil {
		return fmt.Errorf("close connection error: %w", err)
	}

	return nil
}
