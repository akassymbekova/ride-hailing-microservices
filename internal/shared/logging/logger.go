package logging

import (
	"context"
	"log/slog"
	"os"
	"runtime"
	"strconv"
	"time"
)

// Ключи контекста для сквозного логирования (correlation IDs).
type ctxKey string

const (
	RequestIDKey ctxKey = "request_id"
	RideIDKey    ctxKey = "ride_id"
)

// ContextHandler — обёртка над slog.Handler, вытаскивает request_id и
// ride_id из context.Context и добавляет их в JSON-вывод автоматически,
// чтобы не передавать их руками в каждый вызов Info/Error.
type ContextHandler struct {
	slog.Handler
}

func (h *ContextHandler) Handle(ctx context.Context, r slog.Record) error {
	if ctx != nil {
		if reqID, ok := ctx.Value(RequestIDKey).(string); ok && reqID != "" {
			r.AddAttrs(slog.String("request_id", reqID))
		}
		if rideID, ok := ctx.Value(RideIDKey).(string); ok && rideID != "" {
			r.AddAttrs(slog.String("ride_id", rideID))
		}
	}
	return h.Handler.Handle(ctx, r)
}

// Logger оборачивает *slog.Logger, чтобы гарантировать формат из ТЗ:
// timestamp, level, service, action, message, hostname, request_id, ride_id.
//
// ПОЧЕМУ обёртка, а не голый slog: у slog основной текст вызова
// (msg string) по умолчанию превращается в поле "msg". ТЗ требует ДВА
// разных поля — "action" (короткое имя события, типа ride_requested)
// и "message" (человекочитаемое описание). Голый slog.Info("текст")
// даёт только одно из двух. Поэтому Info/Error здесь принимают оба явно.
type Logger struct {
	slog *slog.Logger
}

// New создаёт логгер для сервиса с указанным именем.
func New(serviceName string) *Logger {
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown"
	}

	opts := &slog.HandlerOptions{
		Level: slog.LevelDebug,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			switch a.Key {
			case slog.TimeKey:
				return slog.String("timestamp", a.Value.Time().UTC().Format(time.RFC3339))
			case slog.LevelKey:
				return slog.String("level", a.Value.String())
			case slog.MessageKey:
				// ВАЖНО: слог по умолчанию называет это поле "msg",
				// а ТЗ требует именно "action" здесь — сам текст msg
				// используется как action (короткое имя события),
				// человекочитаемый текст передаётся отдельным атрибутом "message".
				return slog.String("action", a.Value.String())
			}
			return a
		},
	}

	handler := slog.NewJSONHandler(os.Stdout, opts).WithAttrs([]slog.Attr{
		slog.String("service", serviceName),
		slog.String("hostname", hostname),
	})

	return &Logger{slog: slog.New(&ContextHandler{Handler: handler})}
}

// Info пишет info-лог. action — короткое имя события (ride_requested),
// message — человекочитаемое описание. ctx может быть nil, если пока
// нет request_id/ride_id для добавления.
func (l *Logger) Info(ctx context.Context, action, message string, attrs ...any) {
	l.log(ctx, slog.LevelInfo, action, message, nil, attrs...)
}

// Debug пишет debug-лог.
func (l *Logger) Debug(ctx context.Context, action, message string, attrs ...any) {
	l.log(ctx, slog.LevelDebug, action, message, nil, attrs...)
}

// Error пишет error-лог с вложенным объектом error{msg, stack}, как требует ТЗ.
func (l *Logger) Error(ctx context.Context, action, message string, err error, attrs ...any) {
	l.log(ctx, slog.LevelError, action, message, err, attrs...)
}

func (l *Logger) log(ctx context.Context, level slog.Level, action, message string, err error, attrs ...any) {
	if ctx == nil {
		ctx = context.Background()
	}
	all := append([]any{"message", message}, attrs...)
	if err != nil {
		all = append(all, errGroup(err))
	}
	l.slog.Log(ctx, level, action, all...)
}

// errGroup формирует поле error{msg, stack} согласно спецификации.
func errGroup(err error) slog.Attr {
	const depth = 16
	var pcs [depth]uintptr
	n := runtime.Callers(3, pcs[:]) // пропускаем errGroup, log, Error

	var stack string
	frames := runtime.CallersFrames(pcs[:n])
	for {
		frame, more := frames.Next()
		// БЫЛО (баг): string(rune(frame.Line)) — превращает число в символ Unicode.
		// СТАЛО: strconv.Itoa — печатает число как текст.
		stack += frame.Function + "\n\t" + frame.File + ":" + strconv.Itoa(frame.Line) + "\n"
		if !more {
			break
		}
	}
	return slog.Group(
		"error",
		slog.String("msg", err.Error()),
		slog.String("stack", stack),
	)
}
