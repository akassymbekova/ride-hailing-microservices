package messaging

import (
	"context"

	"ride-hail/internal/shared/logging"
)

type HandlerFunc func(body []byte) error

// Consume подписывается на очередь своим отдельным каналом (не общим
// с publisher'ом — см. NewChannel) и обрабатывает сообщения по одному.
func Consume(conn *Connection, queueName string, log *logging.Logger, handler HandlerFunc) error {
	ch, err := conn.NewChannel()
	if err != nil {
		return err
	}

	if err := ch.Qos(1, 0, false); err != nil {
		return err
	}

	msgs, err := ch.Consume(queueName, "", false, false, false, false, nil)
	if err != nil {
		return err
	}

	ctx := context.Background()
	for msg := range msgs {
		if err := handler(msg.Body); err != nil {
			log.Error(ctx, "message_processing_failed", "handler returned error, requeueing", err)
			_ = msg.Nack(false, true)
			continue
		}
		_ = msg.Ack(false)
	}

	return nil
}

// RunConsumer подписывается на очередь и перезапускает consumer после reconnect RabbitMQ.
func RunConsumer(ctx context.Context, conn *Connection, queueName string, log *logging.Logger, handler HandlerFunc) {
	go func() {
		for {
			if err := Consume(conn, queueName, log, handler); err != nil && ctx.Err() == nil && !conn.IsClosed() {
				log.Error(ctx, "consumer_stopped", queueName+" consumer stopped", err)
			}
			if ctx.Err() != nil || conn.IsClosed() {
				return
			}

			log.Info(ctx, "consumer_waiting_reconnect", "consumer waiting for rabbitmq reconnect", "queue", queueName)
			if err := conn.WaitReconnect(ctx); err != nil {
				return
			}
			log.Info(ctx, "consumer_restarted", "consumer resubscribed after reconnect", "queue", queueName)
		}
	}()
}
