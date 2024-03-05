package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"

	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.17.0"

	"google.golang.org/grpc"
)

func fatalLog(err error, message string) {
	const fatalLevel slog.Level = 10
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	logger.Log(context.Background(), fatalLevel, fmt.Sprintf("%s: %v", message, err))
	os.Exit(1)
}

func registerTracerProvider() (*sdktrace.TracerProvider, error) {

	res := resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceNameKey.String("client"),
	)

	ctx := context.Background()

	endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	if endpoint == "" {
		endpoint = "0.0.0.0:4317"
	}
	spanExporter, err := otlptracegrpc.New(ctx, otlptracegrpc.WithInsecure(), otlptracegrpc.WithEndpoint(endpoint), otlptracegrpc.WithDialOption(grpc.WithBlock()))
	if err != nil {
		fatalLog(err, "failed to create new OTLP trace exporter")
		return nil, err
	}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithResource(res),
		sdktrace.WithBatcher(spanExporter),
	)

	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	return tp, err
}

func retrieveResponse() ([]byte, error) {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	url := "http://localhost:8080/"
	client := http.Client{Transport: otelhttp.NewTransport(http.DefaultTransport)}
	var body []byte
	tr := otel.Tracer("client-tracer")
	ctx, span := tr.Start(context.Background(), "client request")
	defer span.End()
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	logger.Info("TraceID: " + span.SpanContext().TraceID().String())
	logger.Info("Sending request...\n")
	res, err := client.Do(req)
	if err != nil {
		fatalLog(err, "Accessing backend endpoint failed")
	}
	body, err = io.ReadAll(res.Body)
	_ = res.Body.Close()

	return body, err
}

func main() {

	tp, err := registerTracerProvider()
	if err != nil {
		fatalLog(err, "RegisterTravreProvider failed")
	}
	defer func() {
		if err := tp.Shutdown(context.Background()); err != nil {
			fatalLog(err, "Error shutting down tracer provider")
		}
	}()
	body, err := retrieveResponse()

	if err != nil {
		fatalLog(err, "Request failed")
	}

	fmt.Printf("Response Received: %s\n", body)
	fmt.Printf("Waiting for few seconds to export spans ...\n")
	time.Sleep(10 * time.Second)
	fmt.Printf("Inspect traces on stdout\n")
}
