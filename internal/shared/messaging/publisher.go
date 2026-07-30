package messaging

import (
	"context"
	"encoding/json"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

type Publisher struct {
	conn *Connection
}

func NewPublisher(conn *Connection) *Publisher {
	return &Publisher{conn: conn}
}

// Publish сериализует payload в JSON и публикует в exchange с routing key.
// Открывает свой канал на каждый вызов (см. комментарий в NewChannel) —
// дороже, чем держать один канал, но безопасно при конкурентных публикациях
// из разных горутин (а у нас в matching.go именно так и происходит).
func (p *Publisher) Publish(exchange, routingKey string, payload interface{}) error {
	ch, err := p.conn.NewChannel()
	if err != nil {
		return err
	}
	defer ch.Close()

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	return ch.PublishWithContext(ctx, exchange, routingKey, false, false, amqp.Publishing{
		ContentType: "application/json",
		Body:        body,
		Timestamp:   time.Now(),
	})
}
