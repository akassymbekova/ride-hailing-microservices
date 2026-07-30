# Переменные по умолчанию
BINARY_NAME=ride-hail-system
DOCKER_COMPOSE=docker-compose

.PHONY: all build run-infra migrate stop-infra clean fmt vet test test-e2e help

# Цель по умолчанию (вызовется, если просто написать `make`)
all: fmt build

## help: Показать список доступных команд
help:
	@echo "Доступные команды:"
	@sed -n 's/^##//p' $(MAKEFILE_LIST) | column -t -s ':' |  sed -e 's/^/ /'

## build: Сборка проекта строго по ТЗ (go build -o ride-hail-system .)
build: fmt
	@echo "==> Сборка исполняемого файла..."
	go build -o $(BINARY_NAME) .
	@echo "==> Сборка завершена успешно!"

## run-infra: Запуск инфраструктуры (Postgres + PostGIS, RabbitMQ) в Docker
run-infra:
	@echo "==> Запуск контейнеров базы данных и брокера сообщений..."
	$(DOCKER_COMPOSE) up -d
	@echo "==> Окружение запущено. Проверьте статус через: docker ps"
	@echo "==> Примените миграции: make migrate"

## migrate: Применить SQL-миграции к PostgreSQL в Docker
migrate:
	@echo "==> Применение миграций..."
	@for f in migrations/01_init.sql migrations/02_ride_service.sql migrations/04_location_tracking.sql migrations/05_driver_sessions.sql migrations/06_driver_vehicle_attrs.sql migrations/03_seed_demo.sql; do \
		echo "    -> $$f"; \
		docker exec -i ride_hail_postgres psql -U $(or $(DB_USER),ridehail_user) -d $(or $(DB_NAME),ridehail_db) < $$f; \
	done
	@echo "==> Миграции применены"


## stop-infra: Остановка инфраструктуры и сохранение данных
stop-infra:
	@echo "==> Остановка контейнеров..."
	$(DOCKER_COMPOSE) stop

## clean: Полная очистка проекта (удаление бинарников и сброс вольюмов Docker)
clean:
	@echo "==> Удаление собранного бинарника..."
	rm -f $(BINARY_NAME)
	@echo "==> Удаление контейнеров и томов данных (полный сброс БД)..."
	$(DOCKER_COMPOSE) down -v
	@echo "==> Проект полностью очищен!"

## fmt: Автоматическое форматирование кода через gofumpt (ОБЯЗАТЕЛЬНО ПО ТЗ)
fmt:
	@echo "==> Форматирование кода через gofumpt..."
	@if ! command -v gofumpt >/dev/null; then \
		echo "Предупреждение: gofumpt не установлен. Установка..."; \
		go install mvdan.cc/gofumpt@latest; \
	fi
	gofumpt -l -w .

## vet: Статический анализ кода стандартным гошным линтером
vet:
	@echo "==> Проверка кода линтером (go vet)..."
	go vet ./...

## test: Запуск всех unit-тестов в проекте
test:
	@echo "==> Запуск unit-тестов..."
	go test -v -race ./...

## test-e2e: E2E-тест полного flow (нужны запущенные сервисы + make migrate)
test-e2e:
	@echo "==> Запуск E2E-тестов (integration tag)..."
	go test -v -tags=integration -timeout 5m ./tests/e2e/...