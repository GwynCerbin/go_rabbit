// SPDX-License-Identifier: MIT
// Copyright © 2024–2026 Alexander Demin

package adapter

import (
	"time"

	"github.com/rabbitmq/amqp091-go"
)

const mimeReadLimit = 512 //bytes that mime will read

type Client struct {
	Username         string        `env:"USERNAME" yaml:"-"`
	Password         string        `env:"PASSWORD" yaml:"-"`
	Host             string        `env:"HOST" yaml:"host"`
	VHost            string        `env:"VHOST" yaml:"vhost"`
	TcpHeartBeat     time.Duration `env:"HEARTBEAT" yaml:"tcp_heartbeat"`
	Properties       amqp091.Table `env:"PROPERTIES" yaml:"properties"`
	MaxReconnectTime time.Duration `env:"RECONNECT" yaml:"reconnect"`
	Logging          bool          `env:"LOGGING" yaml:"logging"`
}

type ConsumerConfig struct {
	QueueName string        `env:"QUEUE" yaml:"queue"`
	Args      amqp091.Table `env:"ARGS" yaml:"args"`
}

type PublisherConfig struct {
	ExchangeName      string `env:"EXCHANGE" yaml:"exchange"`
	RoutingKey        string `env:"ROUTING" yaml:"routing_key"`
	MessagePersistent bool   `env:"PERSISTENT" yaml:"is_persistent"`
	AppId             string `env:"APP_ID" yaml:"app_id"`
}

type ExchangeDeclare struct {
	Name       string        `env:"NAME" yaml:"name"`
	Type       string        `env:"TYPE" yaml:"type"`
	Durable    bool          `env:"DURABLE" yaml:"durable"`
	AutoDelete bool          `env:"AUTO_DELETE" yaml:"auto_delete"`
	Internal   bool          `env:"INTERNAL" yaml:"internal"`
	Args       amqp091.Table `env:"ARGS" yaml:"args"`
}
type QueueDeclareAndBind struct {
	Name         string        `env:"NAME" yaml:"name"`
	NoBind       bool          `env:"NO_BIND" yaml:"no_bind"`
	RoutingKey   string        `env:"ROUTING_KEY" yaml:"routing_key"`
	ExchangeName string        `env:"EXCHANGE_NAME" yaml:"exchange_name"`
	BindArgs     amqp091.Table `env:"BIND_ARGS" yaml:"bind_args"`
	Durable      bool          `env:"DURABLE" yaml:"durable"`
	AutoDelete   bool          `env:"AUTO_DELETE" yaml:"auto_delete"`
	Exclusive    bool          `env:"EXCLUSIVE" yaml:"exclusive"`
	Args         amqp091.Table `env:"ARGS" yaml:"args"`
}
