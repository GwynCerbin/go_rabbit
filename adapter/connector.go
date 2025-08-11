// SPDX-License-Identifier: MIT
// Copyright © 2024–2026 Alexander Demin

package adapter

import (
	"fmt"
	"log"
	"math/bits"
	"net/url"
	"sync"
	"time"

	rabbit "github.com/GwynCerbin/go_rabbit"
	"github.com/rabbitmq/amqp091-go"
)

// Con manages a RabbitMQ AMQP091 connection with automatic reconnection.
// It holds the active connection, target URI, client configuration, and coordinates
// reconnection and shutdown across consumers and publishers:
//   - connection: active AMQP091 connection
//   - url: broker URI for dialing
//   - cfg: AMQP091 client configuration
//   - stop: channel signaling the reconnection loop to exit
//   - cons: WaitGroup tracking active consumers and publishers for graceful shutdown
//   - logging: flag to enable verbose log output
//   - maxReconnectTime: maximum delay for exponential backoff on reconnect
//   - mute: mutex protecting reconnection setup
type Con struct {
	// connection holds the active AMQP connection.
	connection *amqp091.Connection
	// url is the target URI for dialing the broker.
	url *url.URL
	// stop signals the reconnection loop to exit.
	stop chan struct{}
	// cfg stores the AMQP client configuration.
	cfg amqp091.Config
	// cons tracks active consumers to allow graceful shutdown.
	cons sync.WaitGroup
	// logging toggles verbose log output for debugging.
	logging bool
	// maxReconnectTime caps the exponential backoff delay in seconds.
	maxReconnectTime time.Duration
	// mute serializes access during reconnection setup.
	mute sync.RWMutex
}

// Dial establishes an AMQP connection using the provided client configuration.
// It returns a Con instance ready to declare exchanges, queues, and create publishers/consumers.
func Dial(cfg *Client) (*Con, error) {
	if cfg == nil {
		return nil, ConConfEmptyError{}
	}

	const stdMaxTime time.Duration = 0x3_ffff_ffff

	var (
		clientCfg = amqp091.Config{
			SASL: []amqp091.Authentication{
				&amqp091.PlainAuth{Username: cfg.Username, Password: cfg.Password},
			},
			Vhost:      cfg.VHost,
			Properties: cfg.Properties,
			Heartbeat:  cfg.TcpHeartBeat,
		}
		maxTime = stdMaxTime
		uri     = &url.URL{
			Scheme: "amqp",
			Host:   cfg.Host,
		}
	)

	if cfg.MaxReconnectTime != 0 {
		var value uint64

		value = ^uint64(0) << (63 - bits.LeadingZeros64(uint64(cfg.MaxReconnectTime)))
		maxTime = time.Duration(^value)
		if uint64(maxTime-cfg.MaxReconnectTime) > uint64(cfg.MaxReconnectTime-time.Duration(^(value<<1))) {
			maxTime = time.Duration(^(value << 1))
		}
	}

	con, err := amqp091.DialConfig(uri.String(), clientCfg)
	if err != nil {
		return nil, fmt.Errorf("dial amqp091: %w", err)
	}

	return &Con{
		connection:       con,
		url:              uri,
		cfg:              clientCfg,
		stop:             make(chan struct{}, 1),
		maxReconnectTime: maxTime,
		logging:          cfg.Logging,
	}, nil
}

// reconnect triggers a one-time reconnection sequence upon connection loss.
// It ensures multiple errors in quick succession do not spawn multiple loops.
func (c *Con) reconnect(err error) {
	if c.mute.TryLock() {
		if c.logging {
			log.Printf("rabbit reconnect lost: %v", err)
		}
		c.reconnectLoop()
		c.mute.Unlock()
	}
}

// ReconnectLoop attempts to re-establish the AMQP connection using exponential backoff.
// It doubles the wait time after each failed attempt, capped by maxReconnectTime.
// The loop exits when stop is closed or a new connection is successfully made.
func (c *Con) reconnectLoop() {
	const firstDelay time.Duration = 0x1_FFFF_FFF // ~1.07 seconds

	var (
		timer   = time.NewTimer(0)
		maxTime = c.maxReconnectTime
	)

	defer timer.Stop()

	for waitTime, attempt := firstDelay, 1; true; waitTime, attempt = maxTime&(waitTime<<1|1), attempt+1 {
		select {
		case <-c.stop:
			return
		case <-timer.C:
			if c.logging {
				log.Printf("rabbit reconnect attempt %d", attempt)
			}

			con, err := amqp091.DialConfig(c.url.String(), c.cfg)
			if err != nil {
				timer.Reset(waitTime)

				continue
			}

			c.connection = con
			if c.logging {
				log.Print("rabbit reconnect success")
			}

			return
		}
	}
}

// DeclareExchange opens a channel, declares an exchange, and closes the channel.
func (c *Con) DeclareExchange(cfg *ExchangeDeclare) error {
	c.mute.RLock()
	ch, err := c.connection.Channel()
	c.mute.RUnlock()
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
	c.mute.RLock()
	ch, err := c.connection.Channel()
	c.mute.RUnlock()
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
	c.mute.RLock()
	ch, err := c.connection.Channel()
	c.mute.RUnlock()
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
	c.mute.RLock()
	ch, err := c.connection.Channel()
	c.mute.RUnlock()
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
func (c *Con) CreateConsumer(cfg *ConsumerConfig) (rabbit.Consumer, error) {
	if cfg == nil {
		return nil, ConsumerConfEmptyError{}
	}

	return newConsumer(c, c.createNotifyChan(), *cfg)
}

// CreatePublisher returns a new broker.Publisher instance or an error PublisherConfEmptyError if the configuration is nil.
func (c *Con) CreatePublisher(cfg *PublisherConfig) (rabbit.Publisher, error) {
	if cfg == nil {
		return nil, PublisherConfEmptyError{}
	}

	return newPublisher(c, *cfg)
}

// CreatePublisherWithConfirmation returns a broker.Publisher that supports confirmation acknowledgments.
func (c *Con) CreatePublisherWithConfirmation(cfg *PublisherConfig) (rabbit.Publisher, error) {
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
