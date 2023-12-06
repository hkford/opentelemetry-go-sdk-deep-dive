package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"

	"go.opentelemetry.io/contrib/propagators/aws/xray"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"

	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
)

func fatalLog(err error, message string) {
	const fatalLevel slog.Level = 10
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	logger.Log(context.Background(), fatalLevel, fmt.Sprintf("%s: %v", message, err))
	os.Exit(1)
}

func registerTracerProvider() (*sdktrace.TracerProvider, error) {
	format := os.Getenv("FORMAT")

	res := resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceNameKey.String("gateway-service"),
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

	if format == "XRAY" {
		tp := newTracerProviderWithXRayExporter(res, spanExporter)
		otel.SetTextMapPropagator(xray.Propagator{})
		return tp, err
	} else if format == "OTEL" {
		tp := newTracerProviderWithOtlpExporter(res, spanExporter)
		otel.SetTextMapPropagator(propagation.TraceContext{})
		return tp, err
	} else {
		return nil, errors.New("environment variable FORMAT (XRAY or OTEL)not set")
	}
}

func newTracerProviderWithXRayExporter(res *resource.Resource, spanExporter *otlptrace.Exporter) *sdktrace.TracerProvider {
	idg := xray.NewIDGenerator()

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithResource(res),
		sdktrace.WithBatcher(spanExporter),
		sdktrace.WithIDGenerator(idg),
	)

	otel.SetTracerProvider(tp)
	return tp
}

func newTracerProviderWithOtlpExporter(res *resource.Resource, spanExporter *otlptrace.Exporter) *sdktrace.TracerProvider {
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithResource(res),
		sdktrace.WithBatcher(spanExporter),
	)

	otel.SetTracerProvider(tp)

	return tp
}

func printSpanContextInfo(name string, spanContext trace.SpanContext) {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	logger.Info(name, "TraceID", spanContext.TraceID().String(), "SpanID", spanContext.SpanID().String())
}

type Server struct {
	cfg Config
}

type Config struct {
	logger *slog.Logger
	tracer trace.Tracer
	port   int
}

func New(cfg Config) *Server {
	return &Server{cfg}
}

func (s Server) healthCheckHandler(w http.ResponseWriter, req *http.Request) {
	_, _ = io.WriteString(w, "Healthy\n")
}

func (s Server) helloHandler(w http.ResponseWriter, req *http.Request) {
	propagator := otel.GetTextMapPropagator()
	s.cfg.logger.Info("Get /hello")
	fmt.Printf("Headers is %+v\n", req.Header)
	ctx := propagator.Extract(req.Context(), propagation.HeaderCarrier(req.Header))
	parentSpanContext := trace.SpanContextFromContext(ctx)
	printSpanContextInfo("Parent Span", parentSpanContext)
	_, span := s.cfg.tracer.Start(ctx, "some operation")
	defer span.End()
	printSpanContextInfo("Child Span", span.SpanContext())
	span.AddEvent("handling backend")

	_, _ = io.WriteString(w, "Hello, world!\n")
}

func (s Server) registerHandlers() {
	http.HandleFunc("/health", s.healthCheckHandler)
	http.HandleFunc("/hello", s.helloHandler)
}

func (s Server) run() {
	s.cfg.logger.Info(fmt.Sprintf("Server is running at %v", s.cfg.port))
	err := http.ListenAndServe(fmt.Sprintf(":%v", s.cfg.port), nil)
	if err != nil {
		fatalLog(err, "Failed to launch server")
	}
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	tp, err := registerTracerProvider()
	if err != nil {
		fatalLog(err, "RegisterTravreProvider failed")
	}
	defer func() {
		if err := tp.Shutdown(context.Background()); err != nil {
			fatalLog(err, "Error shutting down tracer provider")
		}
	}()

	tracer := otel.Tracer("backend-tracer")
	server := New(Config{logger: logger, tracer: tracer, port: 3000})
	server.registerHandlers()
	server.run()

}
