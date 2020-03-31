// Copyright 2019 New Relic Corporation. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package transform

import (
	"errors"

	"github.com/newrelic/newrelic-telemetry-sdk-go/telemetry"
	"go.opentelemetry.io/otel/api/core"
	"go.opentelemetry.io/otel/api/metric"
	metricsdk "go.opentelemetry.io/otel/sdk/export/metric"
	"go.opentelemetry.io/otel/sdk/export/metric/aggregator"
)

// ErrUnimplementedAgg is returned when a transformation of an unimplemented
// aggregator is attempted.
var ErrUnimplementedAgg = errors.New("unimplemented aggregator")

// Record transforms an OpenTelemetry Record into a Metric.
//
// An ErrUnimplementedAgg error is returned for unimplemented Aggregators.
func Record(service string, r metricsdk.Record) (telemetry.Metric, error) {
	desc := r.Descriptor()
	attrs := attributes(service, desc, r.Labels())
	switch a := r.Aggregator().(type) {
	case aggregator.MinMaxSumCount:
		return minMaxSumCount(desc, attrs, a)
	case aggregator.Sum:
		return sum(desc, attrs, a)
	}
	return nil, ErrUnimplementedAgg
}

// sum transforms a Sum Aggregator aggregation into a Count Metric.
func sum(desc *metric.Descriptor, attrs map[string]interface{}, a aggregator.Sum) (telemetry.Metric, error) {
	sum, err := a.Sum()
	if err != nil {
		return nil, err
	}

	return telemetry.Count{
		Name:       desc.Name(),
		Attributes: attrs,
		Value:      sum.CoerceToFloat64(desc.NumberKind()),
	}, nil
}

// minMaxSumCountValue returns the values of the MinMaxSumCount Aggregator
// as discret values or any error returned from parsing any of the values.
func minMaxSumCountValues(a aggregator.MinMaxSumCount) (min, max, sum core.Number, count int64, err error) {
	if min, err = a.Min(); err != nil {
		return
	}
	if max, err = a.Max(); err != nil {
		return
	}
	if sum, err = a.Sum(); err != nil {
		return
	}
	if count, err = a.Count(); err != nil {
		return
	}
	return
}

// minMaxSumCount transforms a MinMaxSumCount aggregation into a Summary Metric.
func minMaxSumCount(desc *metric.Descriptor, attrs map[string]interface{}, a aggregator.MinMaxSumCount) (telemetry.Metric, error) {
	min, max, sum, count, err := minMaxSumCountValues(a)
	if err != nil {
		return nil, err
	}

	return telemetry.Summary{
		Name:       desc.Name(),
		Attributes: attrs,
		Count:      float64(count),
		Sum:        sum.CoerceToFloat64(desc.NumberKind()),
		Min:        min.CoerceToFloat64(desc.NumberKind()),
		Max:        max.CoerceToFloat64(desc.NumberKind()),
	}, nil
}

func attributes(service string, desc *metric.Descriptor, labels metricsdk.Labels) map[string]interface{} {
	resAttrs := desc.Resource().Attributes()
	iter := labels.Iter()

	attrs := make(map[string]interface{}, 5+iter.Len()+len(resAttrs))

	// TODO (MrAlias): Add desc.LibrayName information here added in v0.4.
	// Should be sent as instrumentation.name and instrumentation.version attributes.
	attrs["unit"] = string(desc.Unit())
	attrs["description"] = desc.Description()

	for _, kv := range resAttrs {
		attrs[string(kv.Key)] = kv.Value.AsInterface()
	}

	for iter.Next() {
		kv := iter.Label()
		attrs[string(kv.Key)] = kv.Value.AsInterface()
	}

	// New Relic registered attributes to identify where this data came from.
	attrs[serviceNameAttrKey] = service
	attrs[instrumentationProviderAttrKey] = instrumentationProviderAttrValue
	attrs[collectorNameAttrKey] = collectorNameAttrValue

	return attrs
}
