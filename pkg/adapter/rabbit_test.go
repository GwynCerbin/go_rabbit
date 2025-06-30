package adapter

import (
	"context"
	"log"
	"os"
	"strings"
	"testing"
)

func TestRabbitConnector(t *testing.T) {
	val, ok := os.LookupEnv("CONNECTOR")
	if !ok {
		t.Skip("Skipping RabbitMQ connector test")
		return
	}
	arg := strings.Split(val, "|")
	if len(arg) != 3 {
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

	defer func() {
		con.Close()
	}()

	queueCfg := &QueueDeclareAndBind{
		Name:    "test",
		NoBind:  true,
		Durable: true,
	}
	if err = con.QueueDeclareAndBind(queueCfg); err != nil {
		t.Errorf("failed to create Queue: %v", err)
		return
	}
	defer func() {
		if err := con.DeleteQueue(queueCfg.Name); err != nil {
			log.Print(err)
		}
	}()
	publisher, err := con.CreatePublisherWithConfirmation(&PublisherConfig{
		ExchangeName: "",
		RoutingKey:   queueCfg.Name,
	})
	if err != nil {
		t.Errorf("failed to create publisher: %v", err)
		return
	}
	consumer, err := con.CreateConsumer(&ConsumerConfig{
		QueueName: queueCfg.Name,
	})
	if err != nil {
		t.Errorf("failed to create consumer: %v", err)
		return
	}
	var ch = make(chan struct {
		ContentType string
		Data        []byte
	}, 1)
	go func() {
		for {
			msg, err := consumer.Consume()
			if err != nil {
				log.Print(err)
				return
			}
			if err = msg.Ack(); err != nil {
				log.Print(err)
			}
			ch <- struct {
				ContentType string
				Data        []byte
			}{ContentType: msg.ContentType(), Data: msg.Body()}
		}
	}()

	type args struct {
		msg string
	}
	var tests = []struct {
		name        string
		args        args
		want        string
		wantContent string
	}{
		{
			name: "case 1",
			args: args{
				msg: "test",
			},
			want:        "test",
			wantContent: "text/plain; charset=utf-8",
		},
		{
			name: "case 2",
			args: args{
				msg: `{"json":"swagging"}`,
			},
			want:        `{"json":"swagging"}`,
			wantContent: "application/json",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err = publisher.Publish(context.Background(), []byte(tt.args.msg)); err != nil {
				t.Errorf("failed to publish: %v", err)
				return
			}
			data := <-ch
			if string(data.Data) != tt.want {
				t.Errorf("got %s, want %s", string(data.Data), tt.want)
				return
			}

			log.Print(data.ContentType)
		})
	}

}
