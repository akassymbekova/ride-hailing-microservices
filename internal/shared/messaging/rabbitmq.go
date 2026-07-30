package messaging

import (
	"context"
	"fmt"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	"ride-hail/internal/shared/logging"
)

// Config содержит параметры подключения к RabbitMQ.
type Config struct {
	Host     string
	Port     string
	User     string
	Password string
}

// Connection управляет жизненным циклом подключения к RabbitMQ:
// первичное подключение, объявление топологии (exchanges/queues/bindings)
// и автоматический реконнект при обрыве связи.
type Connection struct {
	cfg      Config
	log      *logging.Logger
	mu       sync.RWMutex
	conn     *amqp.Connection
	isClosed bool

	reconnectMu sync.Mutex
	reconnectCh chan struct{}
}

// NewConnection создает новый экземпляр управления RabbitMQ.
func NewConnection(cfg Config, log *logging.Logger) *Connection {
	return &Connection{
		cfg:         cfg,
		log:         log,
		reconnectCh: make(chan struct{}),
	}
}

// Connect устанавливает соединение и объявляет топологию. При обрыве
// связи автоматически переподключается (см. handleDisconnect).
func (c *Connection) Connect(ctx context.Context) error {
	url := fmt.Sprintf("amqp://%s:%s@%s:%s/", c.cfg.User, c.cfg.Password, c.cfg.Host, c.cfg.Port)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		c.log.Info(ctx, "rabbitmq_connecting", "connecting to RabbitMQ")
		conn, err := amqp.Dial(url)
		if err != nil {
			c.log.Error(ctx, "rabbitmq_connect_failed", "failed to connect, retrying", err)
			time.Sleep(5 * time.Second)
			continue
		}

		c.mu.Lock()
		c.conn = conn
		c.isClosed = false
		c.mu.Unlock()

		if err := c.setupTopology(); err != nil {
			c.log.Error(ctx, "rabbitmq_topology_failed", "failed to setup topology, retrying", err)
			_ = conn.Close()
			time.Sleep(5 * time.Second)
			continue
		}

		c.log.Info(ctx, "rabbitmq_connected", "connected to RabbitMQ and topology is ready")
		go c.handleDisconnect(ctx)
		return nil
	}
}

// setupTopology объявляет все exchanges, queues и bindings согласно ТЗ.
// Использует СВОЙ отдельный канал, который закрывает сразу после
// объявления — топология объявляется один раз, канал для этого не
// нужно держать живым постоянно.
func (c *Connection) setupTopology() error {
	ch, err := c.NewChannel()
	if err != nil {
		return fmt.Errorf("failed to open setup channel: %w", err)
	}
	defer ch.Close()

	exchanges := []struct{ name, kind string }{
		{"ride_topic", "topic"},
		{"driver_topic", "topic"},
		{"location_fanout", "fanout"},
	}
	for _, ex := range exchanges {
		if err := ch.ExchangeDeclare(ex.name, ex.kind, true, false, false, false, nil); err != nil {
			return fmt.Errorf("failed to declare exchange %s: %w", ex.name, err)
		}
	}

	queues := []string{
		"ride_requests", "ride_status", "driver_matching",
		"driver_responses", "driver_status", "location_updates_ride",
	}
	for _, q := range queues {
		if _, err := ch.QueueDeclare(q, true, false, false, false, nil); err != nil {
			return fmt.Errorf("failed to declare queue %s: %w", q, err)
		}
	}

	bindings := []struct{ queue, key, exchange string }{
		{"ride_requests", "ride.request.*", "ride_topic"},
		{"ride_status", "ride.status.*", "ride_topic"},
		// ИСПРАВЛЕНО: driver_matching должен слушать ride_topic (где реально
		// публикуются ride.request.* из Ride Service), а не driver_topic —
		// в оригинале коллеги было указано driver_topic, из-за чего
		// Driver & Location Service никогда не получил бы заявки на подбор.
		{"driver_matching", "ride.request.*", "ride_topic"},
		{"driver_responses", "driver.response.*", "driver_topic"},
		{"driver_status", "driver.status.*", "driver_topic"},
		{"location_updates_ride", "", "location_fanout"},
	}
	for _, b := range bindings {
		if err := ch.QueueBind(b.queue, b.key, b.exchange, false, nil); err != nil {
			return fmt.Errorf("failed to bind %s to %s: %w", b.queue, b.exchange, err)
		}
	}

	return nil
}

// NewChannel создаёт НОВЫЙ канал на текущем соединении при каждом вызове.
//
// ИСПРАВЛЕНО: в оригинальной версии был единственный метод Channel(),
// который возвращал ОДИН закэшированный канал всем подряд (publisher'ам,
// consumer'ам, HTTP-хендлерам). Согласно документации amqp091-go, методы
// *amqp.Channel НЕ безопасны для конкурентного вызова разными горутинами
// одновременно (кроме Close). Publisher и Consumer работают в разных
// горутинах — при реальной нагрузке это гонка данных. Поэтому каждый
// вызывающий код должен запрашивать СВОЙ канал через NewChannel().
func (c *Connection) NewChannel() (*amqp.Channel, error) {
	c.mu.RLock()
	conn := c.conn
	c.mu.RUnlock()

	if conn == nil {
		return nil, fmt.Errorf("rabbitmq connection is not established yet")
	}
	return conn.Channel()
}

func (c *Connection) handleDisconnect(ctx context.Context) {
	c.mu.RLock()
	conn := c.conn
	c.mu.RUnlock()

	notifyClose := conn.NotifyClose(make(chan *amqp.Error))

	select {
	case <-ctx.Done():
		return
	case err := <-notifyClose:
		if err != nil {
			c.log.Error(ctx, "rabbitmq_disconnected", "connection closed, attempting reconnect", err)
		}
	}

	c.mu.RLock()
	closedByUs := c.isClosed
	c.mu.RUnlock()
	if closedByUs {
		return
	}

	if err := c.Connect(ctx); err == nil {
		c.notifyReconnect()
	}
}

// IsClosed сообщает, что соединение закрыто намеренно (graceful shutdown).
func (c *Connection) IsClosed() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.isClosed
}

// WaitReconnect блокируется до следующего успешного переподключения RabbitMQ.
func (c *Connection) WaitReconnect(ctx context.Context) error {
	c.reconnectMu.Lock()
	ch := c.reconnectCh
	c.reconnectMu.Unlock()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-ch:
		return nil
	}
}

func (c *Connection) notifyReconnect() {
	c.reconnectMu.Lock()
	close(c.reconnectCh)
	c.reconnectCh = make(chan struct{})
	c.reconnectMu.Unlock()
}

// Close закрывает соединение окончательно (для graceful shutdown).
func (c *Connection) Close() error {
	c.mu.Lock()
	c.isClosed = true
	conn := c.conn
	c.mu.Unlock()

	if conn == nil {
		return nil
	}
	return conn.Close()
}
