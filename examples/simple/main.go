// Copyright 2019 New Relic Corporation. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// This example script is the sample from the OpenTelemetry Go "Getting Started"
// guide, with the text-based exporter replaced with the New Relic OpenTelemetry
// Exporter.

// This example allows customers to override the Metrics and Spans endpoint URLs
// with these environment variables:
//   NEW_RELIC_METRIC_URL
//   NEW_RELIC_TRACE_URL

// For example, as of this writing, if using this in the EU, set these two
// environment variables to send data to the New Relic EU datacenter:
//   NEW_RELIC_METRIC_URL=https://metric-api.eu.newrelic.com/trace/v1
//   NEW_RELIC_TRACE_URL=https://trace-api.eu.newrelic.com/trace/v1

package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/newrelic/newrelic-telemetry-sdk-go/telemetry"
	"github.com/nicksherron/opentelemetry-exporter-go/newrelic"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/baggage"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/global"
	"go.opentelemetry.io/otel/propagation"
	controller "go.opentelemetry.io/otel/sdk/metric/controller/basic"
	processor "go.opentelemetry.io/otel/sdk/metric/processor/basic"
	"go.opentelemetry.io/otel/sdk/metric/selector/simple"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/semconv"
	"go.opentelemetry.io/otel/trace"
)

func main() {

	// Create a New Relic OpenTelemetry Exporter
	apiKey, ok := os.LookupEnv("NEW_RELIC_API_KEY")
	if !ok {
		fmt.Println("Missing NEW_RELIC_API_KEY required for New Relic OpenTelemetry Exporter")
		os.Exit(1)
	}

	serviceName := "Simple OpenTelemetry Service"
	exporter, err := newrelic.NewExporter(
		serviceName,
		apiKey,
		telemetry.ConfigBasicErrorLogger(os.Stderr),
		telemetry.ConfigBasicDebugLogger(os.Stderr),
		telemetry.ConfigBasicAuditLogger(os.Stderr),
		func(cfg *telemetry.Config) {
			cfg.MetricsURLOverride = os.Getenv("NEW_RELIC_METRIC_URL")
			cfg.SpansURLOverride = os.Getenv("NEW_RELIC_TRACE_URL")
			cfg.EventsURLOverride = os.Getenv("NEW_RELIC_EVENT_URL")
		},
	)
	if err != nil {
		fmt.Printf("Failed to instantiate New Relic OpenTelemetry exporter: %v\n", err)
		os.Exit(1)
	}

	ctx := context.Background()
	defer exporter.Shutdown(ctx)

	// Minimally default resource with a service name
	r := resource.NewWithAttributes(semconv.ServiceNameKey.String(serviceName))

	// Create a tracer provider
	bsp := sdktrace.NewBatchSpanProcessor(exporter)
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(bsp), sdktrace.WithResource(r))
	defer func() { _ = tp.Shutdown(ctx) }()

	// Create a meter provider
	pusher := controller.New(
		processor.New(
			simple.NewWithExactDistribution(),
			exporter,
		),
		controller.WithExporter(exporter),
	)

	err = pusher.Start(ctx)
	if err != nil {
		log.Fatalf("failed to initialize metric controller: %v", err)
	}
	pusher.Start(ctx)

	// Handle this error in a sensible manner where possible
	defer func() { _ = pusher.Stop(ctx) }()

	// Set global options
	otel.SetTracerProvider(tp)
	global.SetMeterProvider(pusher.MeterProvider())
	propagator := propagation.NewCompositeTextMapPropagator(propagation.Baggage{}, propagation.TraceContext{})
	otel.SetTextMapPropagator(propagator)

	// Sample metric instruments
	fooKey := attribute.Key("ex.com/foo")
	barKey := attribute.Key("ex.com/bar")
	lemonsKey := attribute.Key("ex.com/lemons")
	anotherKey := attribute.Key("ex.com/another")

	commonLabels := []attribute.KeyValue{lemonsKey.Int(10), attribute.String("A", "1"), attribute.String("B", "2"), attribute.String("C", "3")}

	meter := global.Meter("ex.com/basic")

	observerCallback := func(_ context.Context, result metric.Float64ObserverResult) {
		result.Observe(1, commonLabels...)
	}
	_ = metric.Must(meter).NewFloat64ValueObserver("ex.com.one", observerCallback,
		metric.WithDescription("A ValueObserver set to 1.0"),
	)

	valueRecorder := metric.Must(meter).NewFloat64ValueRecorder("ex.com.two")

	boundRecorder := valueRecorder.Bind(commonLabels...)
	defer boundRecorder.Unbind()

	// Create a trace and some measurements
	tracer := otel.Tracer("ex.com/basic")
	ctx = baggage.ContextWithValues(ctx,
		fooKey.String("foo1"),
		barKey.String("bar1"),
	)

	func(ctx context.Context) {
		var span trace.Span
		ctx, span = tracer.Start(ctx, "operation",
			trace.WithSpanKind(trace.SpanKindServer))
		defer span.End()

		span.AddEvent("Nice operation!", trace.WithAttributes(attribute.Int("bogons", 100)))
		span.SetAttributes(anotherKey.String("yes"))

		meter.RecordBatch(
			// Note: call-site variables added as context Entries:
			baggage.ContextWithValues(ctx, anotherKey.String("xyz")),
			commonLabels,

			valueRecorder.Measurement(2.0),
		)

		func(ctx context.Context) {
			var span trace.Span
			ctx, span = tracer.Start(ctx, "Sub operation...")
			defer span.End()

			span.SetAttributes(lemonsKey.String("five"))
			span.AddEvent("Sub span event")
			boundRecorder.Record(ctx, 1.3)
		}(ctx)
	}(ctx)

}
