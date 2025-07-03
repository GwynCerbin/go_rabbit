<file name=0 path=README.MD># go_rabbit

Lightweight fault‑tolerant **RabbitMQ** adapter in Go – producers, consumers, automatic reconnects, graceful‑shutdown, and built‑in metrics. Suitable for microservices as well as CLI utilities.

> **Version:** 0.1.0  
> **License:** MIT

---

## Features

- **Reliable connection** — automatic _re-dial_ with back-off and publisher confirms.
- **Simple API** — unified wrapper over [`amqp091-go`](https://pkg.go.dev/github.com/rabbitmq/amqp091-go) for producers and consumers.
- **Graceful shutdown** — graceful shutdown on `SIGINT/SIGTERM`.
- **Observability** — built-in Prometheus metrics and flexible structured logging: by default standard `log`,  
  but optionally any compatible logger (e.g. `zap`) via `SetLogger`.
- **Flexible topology description** — exchanges, queues, bindings, DLQ, TTL, prefetch described in YAML/ENV.
- **Utilities** — UUID generation, MIME detection of attachments, and other useful functions.

---

## Tech stack

| Category             | Package / tool                          |
| -------------------- | -------------------------------------- |
| AMQP driver          | `github.com/rabbitmq/amqp091-go`       |
| Utilities            | `github.com/google/uuid`, `mimetype`   |

Full list of dependencies is in `go.mod`.

---

## Installation

```bash
go get github.com/GwynCerbin/go_rabbit@latest
```

---

## Quick start
> Configuration can be loaded from YAML or directly from environment variables.  
> For ENV, just set prefixes (see `envPrefix` tags) and use the package  
> [`github.com/caarlos0/env/v10`](https://github.com/caarlos0/env).

### YAML configuration:
```yaml
client:
  host: rabbitmq:5672
  vhost: /
  heartbeat: 10s           # TCP heartbeat
  reconnect: 30s           # max reconnect interval
  logging: true

exchange_binding:
  - name: exchange1_example
    type: topic
    durable: true
  - name: exchange2_example
    type: fanout
    durable: false
  - name: dlx
    type: direct
    durable: true

queue_binding:
  - name: queue1_example
    durable: true
    exchange_name: exchange1_example
    routing_key: routing_key1_example
  - name: queue2_example
    durable: true
    exchange_name: exchange1_example
    routing_key: routing_key2_example
  - name: queue3_example
    durable: true
    exchange_name: exchange2_example
    routing_key: ""
    args:
      x-dead-letter-exchange: dlx
      x-dead-letter-routing-key: jobs.dlq
  - name: jobs.dlq
    durable: true
    exchange_name: dlx
    routing_key: jobs.dlq


example_consumer:
  queue: queue1_example
  args:
    x‑prefetch-count: 20

example1_publisher:
  exchange: exchange1_example
  routing_key: routing_key1_example
  app_id: access-manager
  persistent: true

example2_publisher:
  exchange: exchange2_example
  routing_key: ""
  app_id: audit-service
  persistent: false
```

### ENV configuration:
```env
CLIENT_HOST=rabbitmq:5672
CLIENT_VHOST=/
CLIENT_HEARTBEAT=10s
CLIENT_RECONNECT=30s
CLIENT_LOGGING=true
CLIENT_USERNAME=guest
CLIENT_PASSWORD=guest
PUBLISHER1_EXCHANGE=exchange1_example
PUBLISHER1_ROUTING_KEY=routing_key1_example
PUBLISHER1_APP_ID=access-manager
PUBLISHER1_PERSISTENT=true
PUBLISHER2_EXCHANGE=exchange2_example
PUBLISHER2_ROUTING_KEY=
PUBLISHER2_APP_ID=audit-service
PUBLISHER2_PERSISTENT=false
QUEUE_NAME=queue1_example
QUEUE_DURABLE=true
QUEUE_EXCHANGE_NAME=exchange1_example
QUEUE_ROUTING_KEY=routing_key1_example
EXAMPLE_CONSUMER_QUEUE=queue1_example
EXAMPLE_CONSUMER_ARGS_X_PREFETCH_COUNT=20
```

### Go struct
* `Client` — connection parameters (see the `client` section in YAML / prefix `CLIENT_` in ENV).
* `ExampleExchanges` — exchange topology; corresponds to `example_exchanges`.
* `QueueBinding` — queue declarations and their bindings.
* `ExampleConsumer` — consumer configuration.
* `Example1Publisher`, `Example2Publisher` — settings of two different publishers.

> If you prefer ENV over YAML, set variables with the shown prefixes  
> (`CLIENT_HOST`, `PUBLISHER1_EXCHANGE`, etc.) and use the package  
> [`github.com/caarlos0/env/v10`](https://github.com/caarlos0/env).

```go
type Rabbit struct {
    Client            Client                `envPrefix:"CLIENT_"            yaml:"client"`
	ExchangeBinding   []ExchangeDeclare     `envPrefix:"EXAMPLE_EXCHANGES_" yaml:"exchange_binding"`
    QueueBinding      []QueueDeclareAndBind `envPrefix:"QUEUE_"             yaml:"queue_binding"`
    ExampleConsumer   ConsumerConfig        `envPrefix:"EXAMPLE_CONSUMER_"  yaml:"example_consumer"`
    Example1Publisher PublisherConfig       `envPrefix:"PUBLISHER1_"        yaml:"example1_publisher"`
    Example2Publisher PublisherConfig       `envPrefix:"PUBLISHER2_"        yaml:"example2_publisher"`
}
```

### Infrastructure initialization:
```go
rabCon, err := rabbit.Dial(cfg.Rabbit.Client)
if err != nil {
    zap.L().Error("rabbit init", zap.Error(err))
    return
}

defer func() {
    if err = rabCon.Close(); err != nil {
        zap.L().Error("rabbit close connection gracefully", zap.Error(err))
    }
}()

// declare queues if needed 
for i := range cfg.Rabbit.QueueBinding {
    if err = rabCon.QueueDeclareAndBind(&cfg.Rabbit.QueueBinding[i]); err != nil {
        zap.L().Error("rabbit declare queue and bind", zap.Error(err))
        
        return
    }
}

// declare exchanges if needed
for i := range cfg.Rabbit.ExchangeBinding {
    if err = rabCon.QueueDeclareAndBind(&cfg.Rabbit.ExchangeBinding[i]); err != nil {
        zap.L().Error("rabbit declare queue and bind", zap.Error(err))
        
        return
    }
}

pub, err := rabCon.CreatePublisher(cfg.Rabbit.Example1Publisher)
if err != nil {
    zap.L().Error("rabbit create publisher", zap.Error(err))
    return
}

pubWithConf, err := rabCon.CreatePublisherWithConfirmation(cfg.Rabbit.Example2Publisher)
if err != nil {
    zap.L().Error("rabbit create publisher", zap.Error(err))
    return
}

consumer, err := con.CreateConsumer(cfg.Rabbit.ExampleConsumer)
if err != nil {
    zap.L().Error("create consumer", zap.Any("error", err))

    return
}

collector, err := infra.Init(infra.Connectors{
    Consumer: consumer,
    Publisher: pub,
    PublisherWithConf: pubWithConf,
})
if err != nil {
    zap.L().Error("init infra", zap.Any("error", err))

    return
}

var ctx, cancel = context.WithCancel(context.Background())

go func() {
    if err = broker.ListenAndServe(cfg.Broker ,collector.BrokerInfra()); err != nil {
        zap.L().Error("broker server closed", zap.Any("error", err))
    }

    cancel()
}()

if err = rest.ListenAndServe(cfg.RestServer, collector.RestInfra()); err != nil {
    zap.L().Error("rest server closed", zap.Any("error", err))
}

<-ctx.Done()
```

---
## Architecture

| Element     | Role in message processing                                                                                                                                                                                                                                          |
|-------------|---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| **Producer** | Creates and publishes messages to an _Exchange_.                                                                                                                                                                                                                     |
| **Exchange** | Receives messages from producers and, according to binding rules, routes them to the corresponding queues.                                                                                                                                                           |
| **Queue**    | Buffer from which consumers pull messages. Supports FIFO behavior and holds messages until `Ack` / `Expire`.                                                                                                                                                          |
| **Binding**  | Link _Exchange → Queue_ with a `routing_key` defining which message goes to which queue.                                                                                                                                                                              |
| **Consumer** | Receives messages, executes business logic, and reports the result to the broker (`Ack`, `Nack`, `Reject`).                                                                                                                                                           |
| **Ack / Nack** | <ul><li>**Ack** — confirms successful processing, message is removed.</li><li>**Nack** (requeue=true) — message is returned to the queue.</li><li>**Reject** (requeue=true) — final rejection (or sent to DLQ if `x-dead-letter-exchange` is set).</li></ul> |


```
┌────────┐   publish   ┌────────────┐
│Producer│ ───────────▶│  Exchange  │
└────────┘             └────┬───────┘
                             │ bind
                       ┌─────▼──────┐  pull
                       │   Queue    │◀───┐
                       └────┬───────┘    │ ack / nack
                            │ deliver    │ (requeue if nack)
                       ┌────▼─────┐      │
                       │ Consumer │──────┘
                       └──────────┘
```
---
## Error‑handling guide

> Any handler MUST end with one of three actions:  
> `Ack()` — acknowledgment, `Nack()` — negative acknowledgment (with **secondary publishing** of the message back to the same queue),  
> `Reject()` — final rejection without redelivery.  
> If none of these methods are called, the message will remain in the `unacked` state and will be redelivered after consumer restart.

| Handling scenario | Action        | Broker behavior                                                            |
|-------------------|---------------|----------------------------------------------------------------------------|
| Successful business logic | `Ack()`  | Message is removed from the queue.                                         |
| Error but want to retry later (temporary DB, external service) | `Nack()` | RabbitMQ sets **requeue=true** → message returns to the tail of the queue. |
| Format/business validation error (retry useless) | `Reject()` | Message is rejected (or goes to DLQ if configured).                        |

### Handler example

```go
func (hs *HandlerSet) TagDeleted(msg broker.Message) {
	var tags []string
	if err := json.Unmarshal(msg.Body(), &tags); err != nil {
		// Invalid format → Reject
		hs.log.Error("invalid body", zap.Error(err))
		_ = msg.Reject()
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := hs.manager.DeleteTagPowers(ctx, tags); err != nil {
		// Temporary error → Nack (requeue)
		hs.log.Error("delete tag powers", zap.Error(err))
		_ = msg.Nack() // automatically requeue = true
		return
	}

	// All good → Ack
	_ = msg.Ack()
}
```

---

## Starting listener with graceful shutdown

```go
func ListenAndServe(consumer broker.Consumer, infra manager.BrokerManager) error {
	const timeOut = 5 * time.Second

	quit := make(chan os.Signal, 1)
	sv   := broker.NewListener(consumer).Init(router(infra))

	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		if err := sv.ListenAndServe(); err != nil {
			logrus.Error("listener stopped", zap.Error(err))
			quit <- syscall.SIGCHLD
		}
	}()
	logrus.Info("Broker listener started")

	switch <-quit {
	case syscall.SIGINT, syscall.SIGTERM:
		logrus.Info("Shutdown ...")
		ctx, cancel := context.WithTimeout(context.Background(), timeOut)
		defer cancel()
		return sv.Shutdown(ctx)
	case syscall.SIGCHLD:
		proc, _ := os.FindProcess(os.Getpid())
		_ = proc.Signal(syscall.SIGTERM)
	}
	return nil
}
```

* `SIGINT/SIGTERM` — initiate smooth shutdown.  
* `SIGCHLD` — internal signal if `ListenAndServe` stopped with error: process restarts via own `SIGTERM`.
