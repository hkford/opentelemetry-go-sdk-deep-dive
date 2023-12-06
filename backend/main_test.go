package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func TestHelloHandler(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithSyncer(exporter),
	)
	otel.SetTracerProvider(tp)
	defer func() {
		if err := tp.Shutdown(context.Background()); err != nil {
			fmt.Println("Error shutting down tracer provider: ", err)
		}
	}()
	ctx := context.Background()
	tr := otel.Tracer("test-tracer")
	ctx, span := tr.Start(ctx, "test request")
	w := httptest.NewRecorder()
	req, _ := http.NewRequestWithContext(ctx, "GET", "http://example.com", nil)
	server := New(Config{logger: logger, tracer: tr, port: 3000})
	server.helloHandler(w, req)
	span.End()
	spans := exporter.GetSpans()
	actualSpan := spans[0]
	wantTraceID := trace.SpanContextFromContext(ctx).TraceID()
	gotTraceID := actualSpan.SpanContext.TraceID()
	assert.Equal(t, wantTraceID, gotTraceID)
	wantEvent := "handling backend"
	gotEvent := actualSpan.Events[0].Name
	assert.Equal(t, wantEvent, gotEvent)
}
