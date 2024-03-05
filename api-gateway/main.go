package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"

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
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithResource(res),
		sdktrace.WithBatcher(spanExporter),
	)

	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	return tp, err
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

func (s Server) retrieveResponse(ctx context.Context) ([]byte, error) {
	url := os.Getenv("BACKEND_ENDPOINT")
	if url == "" {
		panic("BACKEND_ENDPOINT not set")
	}
	client := http.Client{Transport: otelhttp.NewTransport(http.DefaultTransport)}
	var body []byte
	ctx, span := s.cfg.tracer.Start(ctx, "client request")
	defer span.End()
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	s.cfg.logger.Info("TraceID: " + span.SpanContext().TraceID().String())
	s.cfg.logger.Info("Sending request...\n")
	res, err := client.Do(req)
	if err != nil {
		panic(err)
	}
	body, err = io.ReadAll(res.Body)
	_ = res.Body.Close()

	return body, err
}

func (s Server) proxyHandler(w http.ResponseWriter, req *http.Request) {
	s.cfg.logger.Info("Get /hello")
	s.cfg.logger.Info(fmt.Sprintf("Headers is %+v\n", req.Header))
	parentSpanContext := trace.SpanContextFromContext(req.Context())
	printSpanContextInfo("Parent Span", parentSpanContext)
	ctx, span := s.cfg.tracer.Start(req.Context(), "proxy request")
	defer span.End()
	printSpanContextInfo("Child Span", span.SpanContext())
	body, _ := s.retrieveResponse(ctx)
	response := "Response from backend is " + string(body)
	_, _ = io.WriteString(w, response)
}

func (s Server) healthCheckHandler(w http.ResponseWriter, req *http.Request) {
	_, _ = io.WriteString(w, "Healthy\n")
}

func (s Server) registerHandlers() {
	otelProxyHandler := otelhttp.NewHandler(http.HandlerFunc(s.proxyHandler), "proxy request to backend")
	http.HandleFunc("/health", s.healthCheckHandler)
	http.Handle("/", otelProxyHandler)
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

	tracer := otel.Tracer("api-gateway")

	server := New(Config{logger: logger, tracer: tracer, port: 8080})
	server.registerHandlers()
	server.run()

}
