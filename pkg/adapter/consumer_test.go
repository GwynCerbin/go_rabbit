// SPDX-License-Identifier: MIT
// Copyright © 2024–2026 Alexander Demin

package adapter

import (
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"testing"
)

func TestConsumerExample(t *testing.T) {
	val, ok := os.LookupEnv("CONNECTOR")
	if !ok {
		t.Skip("Skipping RabbitMQ connector test")
		return
	}
	arg := strings.Split(val, "|")
	if len(arg) != 4 {
		t.Errorf("invalid args count: %d", len(arg))
		return
	}
	con, err := Dial(&Client{
		Host:     arg[0],
		Username: arg[1],
		Password: arg[2],
	})
	if err != nil {
		t.Errorf("failed to connect to rabbit: %v", err)
		return
	}

	cons, err := con.CreateConsumer(&ConsumerConfig{
		QueueName: arg[3],
	})

	if err != nil {
		t.Errorf("failed to create consumer: %v", err)
		return
	}
	t.Run("consume", func(t *testing.T) {
		var stopper = make(chan os.Signal, 1)
		signal.Notify(stopper, syscall.SIGINT, syscall.SIGTERM)

		go func() {
			for {
				msg, err := cons.Consume()
				if err != nil {
					t.Errorf("failed to consume: %v", err)
					return
				}
				msg.Ack()
				log.Printf("contet type: %s data:\n%s", msg.ContentType(), msg.Body())
				cons.Close()
				stopper <- syscall.SIGINT
				return
			}
		}()
		<-stopper
		if err := con.Close(); err != nil {
			log.Printf(err.Error())
		}
	})
}
