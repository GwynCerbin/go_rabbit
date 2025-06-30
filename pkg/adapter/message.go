// SPDX-License-Identifier: MIT
// Copyright © 2024–2026 Alexander Demin

package adapter

import (
	"sync"

	"github.com/rabbitmq/amqp091-go"
)

// Message wraps an AMQP delivery and tracks acknowledgment state.
// It uses sync.Once to ensure the WaitGroup is decremented only once upon Ack/Nack/Reject.
type Message struct {
	// deliver holds the original AMQP delivery metadata and payload.
	deliver amqp091.Delivery
	// Once prevents multiple Done calls on the WaitGroup.
	sync.Once
	// wg tracks the number of in-flight messages for graceful shutdown.
	wg *sync.WaitGroup
}

// RoutingKey returns the message routing key set on the AMQP delivery.
func (m *Message) RoutingKey() string {
	return m.deliver.RoutingKey
}

// Headers returns the message headers set on the AMQP delivery.
func (m *Message) Headers() map[string]interface{} {
	return m.deliver.Headers
}

// ContentType returns the MIME content type of the message payload.
func (m *Message) ContentType() string {
	return m.deliver.ContentType
}

// IsRedelivered indicates if the delivery is a redelivery (duplicate) of a previous message.
func (m *Message) IsRedelivered() bool {
	return m.deliver.Redelivered
}

// Body returns the raw message payload as a byte slice.
func (m *Message) Body() []byte {
	return m.deliver.Body
}

// Ack acknowledges successful processing of the message.
// It signals the broker that the message can be removed from the queue.
// The WaitGroup Done is called exactly once.
func (m *Message) Ack() error {
	defer func() {
		m.Do(func() {
			m.wg.Done()
		})
	}()

	return m.deliver.Ack(false)
}

// Nack negatively acknowledges the message, optionally requeuing it.
// It signals that processing failed. The WaitGroup Done is called exactly once.
func (m *Message) Nack() error {
	defer func() {
		m.Do(func() {
			m.wg.Done()
		})
	}()

	return m.deliver.Nack(false, true)
}

// Reject rejects the message without multiple negative acknowledgments support.
// It optionally requeues the message. The WaitGroup Done is called exactly once.
func (m *Message) Reject() error {
	defer func() {
		m.Do(func() {
			m.wg.Done()
		})
	}()

	return m.deliver.Reject(false)
}
