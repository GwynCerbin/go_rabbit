// SPDX-License-Identifier: MIT
// Copyright © 2024–2026 Alexander Demin

package adapter

import (
	"sync"
	"sync/atomic"

	"github.com/rabbitmq/amqp091-go"
)

// Message wraps an AMQP delivery and tracks acknowledgment state.
// It uses sync.Once to ensure the WaitGroup is decremented only once upon Ack/Nack/Reject.
type Message struct {
	// deliver holds the original AMQP delivery metadata and payload.
	deliver amqp091.Delivery
	// Once prevents multiple Done calls on the WaitGroup.
	completed atomic.Bool
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

// Ack acknowledges successful processing of the message by the broker
// and decrements the WaitGroup counter exactly once. It returns any error
// from the underlying AMQP delivery acknowledgment.
func (m *Message) Ack() error {
	if m.completed.CompareAndSwap(false, true) {
		err := m.deliver.Ack(false)

		m.wg.Done()

		return err
	}
	return nil
}

// Nack negatively acknowledges the message exactly once, with requeue.
// It decrements the WaitGroup counter and returns any error from the
// underlying AMQP negative acknowledgment.
func (m *Message) Nack() error {
	if m.completed.CompareAndSwap(false, true) {
		err := m.deliver.Nack(false, true)

		m.wg.Done()

		return err
	}
	return nil
}

// Reject rejects the message exactly once without support for multiple-message
// negative acknowledgments, optionally requeuing it. It decrements the
// WaitGroup counter and returns any error from the underlying AMQP reject.
func (m *Message) Reject() error {
	if m.completed.CompareAndSwap(false, true) {
		err := m.deliver.Reject(false)

		m.wg.Done()

		return err
	}
	return nil
}
