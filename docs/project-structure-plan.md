# ride-hail project plan

## 1. Общая структура проекта

```text
cmd/
  ride-service/
  driver-location-service/
  admin-service/

internal/
  shared/
    config/
    logging/
    db/
    messaging/
    ws/
  ride/
  driverlocation/
  admin/

docs/
```

## 2. Пакеты и назначение

### cmd/ride-service
- main.go — точка входа сервиса Ride Service

### cmd/driver-location-service
- main.go — точка входа сервиса Driver & Location Service

### cmd/admin-service
- main.go — точка входа сервиса Admin Service

### internal/shared/config
- config.go — загрузка и хранение конфигурации приложения

### internal/shared/logging
- logger.go — структурированное JSON-логирование

### internal/shared/db
- postgres.go — подключение к PostgreSQL через pgx/v5
- migrations.go — запуск миграций/инициализации схемы

### internal/shared/messaging
- rabbitmq.go — подключение к RabbitMQ
- publisher.go — публикация событий в обмены
- consumer.go — подписка на очереди и обработка сообщений

### internal/shared/ws
- hub.go — управление WebSocket-соединениями
- auth.go — аутентификация соединений

### internal/ride
- handler.go — HTTP-обработчики для /rides и /rides/{id}/cancel
- service.go — бизнес-логика создания и отмены поездки
- repository.go — доступ к данным поездок и событий
- events.go — публикация и обработка ride-related событий

### internal/driverlocation
- handler.go — HTTP-обработчики для /drivers/{id}/online, /offline, /location, /start, /complete
- service.go — логика водителя, matching, обновления локации
- matching.go — алгоритм поиска подходящих водителей
- repository.go — работа с драйверами, координатами, session/history
- events.go — обработка событий из RabbitMQ и WebSocket-уведомлений

### internal/admin
- handler.go — обработчики /admin/overview и /admin/rides/active
- service.go — сбор метрик и аналитики
- repository.go — запросы к БД для статистики

## 3. План разработки по этапам

### Этап 1. Базовая инфраструктура
- создать структуру каталогов
- добавить конфигурацию и логирование
- настроить подключение к PostgreSQL и RabbitMQ
- реализовать graceful shutdown

### Этап 2. База данных и схемы
- создать таблицы пользователей, поездок, координат, статусов, событий
- добавить индексы
- реализовать базовые транзакции

### Этап 3. Ride Service
- реализовать создание поездки
- реализовать расчет стоимости
- реализовать публикацию событий в RabbitMQ
- реализовать отмену поездки

### Этап 4. Driver & Location Service
- реализовать онлайн/оффлайн водителя
- реализовать обновление координат
- реализовать подбор водителей
- реализовать обработку офферов и ответов
- реализовать публикацию статусов и локаций

### Этап 5. WebSocket
- реализовать соединения пассажиров и водителей
- реализовать аутентификацию
- отправлять обновления по событиям

### Этап 6. Admin Service
- реализовать overview и active rides
- собрать базовую статистику и метрики

### Этап 7. Устойчивость и проверка
- добавить reconnect к RabbitMQ
- обработать ошибки и таймауты
- проверить сборку через go build
- протестировать полный цикл поездки

## 4. Рекомендации по реализации
- придерживаться gofumpt
- использовать только разрешённые пакеты
- делать код модульным и разделять бизнес-логику от транспорта
- сначала реализовать MVP, затем расширять функциональность
