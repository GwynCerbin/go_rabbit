// SPDX-License-Identifier: MIT
// Copyright © 2024–2026 Alexander Demin

package broker

import "context"

// Publisher defines the interface for publishing messages to a broker.
// Implementations should send the payload and handle any connection lifecycle.
type Publisher interface {
	// Publish sends a message payload in the given context.
	// It returns an error if the message could not be delivered.
	Publish(context.Context, []byte) error

	// Close releases any resources held by the publisher, such as channels or connections.
	// After Close, further calls to Publish should return an error.
	Close() error
}

// Consumer defines the interface for consuming messages from a broker.
// Each implementation should manage its own connection and message stream.
type Consumer interface {
	// Consume retrieves the next available message or an error if the consumer is closed.
	// The returned Message must be acknowledged or rejected by the caller.
	Consume() (Message, error)

	// Close stops message consumption and releases any resources.
	// After Close, subsequent calls to Consume should return an error.
	Close() error
}

// Message represents a single broker-delivered message, allowing inspection and acknowledgment.
// Implementations wrap the broker-specific delivery type.
type Message interface {
	// Headers returns the message metadata headers.
	Headers() map[string]interface{}

	// ContentType returns the MIME type of the message payload.
	ContentType() string

	// IsRedelivered signals if this delivery is a redelivery of a previous message.
	IsRedelivered() bool

	// Body returns the raw payload bytes.
	Body() []byte

	// RoutingKey returns the message routing key set on the AMQP delivery.
	RoutingKey() string

	// Ack acknowledges successful processing of the message.
	// It signals the broker to remove the message from the queue.
	Ack() error

	// Nack negatively acknowledges the message, requeuing it.
	// It signals a processing failure.
	Nack() error

	// Reject rejects the message without multiple-nack support.
	// It doesn't requeue the message.
	Reject() error
}
