package temporalworkflow

import (
	"context"

	"github.com/asklit/valorant-vod-coach/internal/app"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.temporal.io/sdk/converter"
	"go.temporal.io/sdk/workflow"
)

var propagatedHeaderKeys = []string{"traceparent", "tracestate", "baggage"}

type workflowTraceCarrierKey struct{}

type TraceContextPropagator struct{}

var _ workflow.ContextPropagator = TraceContextPropagator{}

func (TraceContextPropagator) Inject(ctx context.Context, writer workflow.HeaderWriter) error {
	carrier := propagation.MapCarrier{}
	otel.GetTextMapPropagator().Inject(ctx, carrier)
	return writeTemporalHeaders(carrier, writer)
}

func (TraceContextPropagator) Extract(ctx context.Context, reader workflow.HeaderReader) (context.Context, error) {
	carrier, err := readTemporalHeaders(reader)
	if err != nil {
		return ctx, err
	}
	return otel.GetTextMapPropagator().Extract(ctx, carrier), nil
}

func (TraceContextPropagator) InjectFromWorkflow(ctx workflow.Context, writer workflow.HeaderWriter) error {
	carrier, _ := ctx.Value(workflowTraceCarrierKey{}).(propagation.MapCarrier)
	return writeTemporalHeaders(carrier, writer)
}

func (TraceContextPropagator) ExtractToWorkflow(ctx workflow.Context, reader workflow.HeaderReader) (workflow.Context, error) {
	carrier, err := readTemporalHeaders(reader)
	if err != nil {
		return ctx, err
	}
	if len(carrier) == 0 {
		return ctx, nil
	}
	return workflow.WithValue(ctx, workflowTraceCarrierKey{}, carrier), nil
}

func CaptureRequestTraceContext(ctx context.Context, request *app.AnalysisJobRequest) {
	if request == nil {
		return
	}
	carrier := propagation.MapCarrier{}
	otel.GetTextMapPropagator().Inject(ctx, carrier)
	request.TraceParent = carrier.Get("traceparent")
	request.TraceState = carrier.Get("tracestate")
	request.Baggage = carrier.Get("baggage")
}

func RestoreRequestTraceContext(ctx context.Context, request app.AnalysisJobRequest) context.Context {
	carrier := propagation.MapCarrier{
		"traceparent": request.TraceParent,
		"tracestate":  request.TraceState,
		"baggage":     request.Baggage,
	}
	return otel.GetTextMapPropagator().Extract(ctx, carrier)
}

func writeTemporalHeaders(carrier propagation.MapCarrier, writer workflow.HeaderWriter) error {
	for _, key := range propagatedHeaderKeys {
		value := carrier.Get(key)
		if value == "" {
			continue
		}
		payload, err := converter.GetDefaultDataConverter().ToPayload(value)
		if err != nil {
			return err
		}
		writer.Set(key, payload)
	}
	return nil
}

func readTemporalHeaders(reader workflow.HeaderReader) (propagation.MapCarrier, error) {
	carrier := propagation.MapCarrier{}
	for _, key := range propagatedHeaderKeys {
		payload, found := reader.Get(key)
		if !found {
			continue
		}
		var value string
		if err := converter.GetDefaultDataConverter().FromPayload(payload, &value); err != nil {
			return nil, err
		}
		carrier.Set(key, value)
	}
	return carrier, nil
}
