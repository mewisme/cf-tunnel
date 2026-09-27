//lint:file-ignore SA1019 Pinned upstream-compatible no-op trace API.
package tracing

import (
	"context"
	"net/http"

	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel/trace"
)

const (
	TracerContextName           = "cf-trace-id"
	IntCloudflaredTracingHeader = "cf-int-cloudflared-tracing"
	traceID128bitsWidth         = 128 / 4
	separator                   = ":"
)

var CanonicalCloudflaredTracingHeader = http.CanonicalHeaderKey(IntCloudflaredTracingHeader)

var (
	Http2TransportAttribute = trace.WithAttributes()
	QuicTransportAttribute  = trace.WithAttributes()
)

func Init(string) {}

type cfdTracer struct {
	trace.TracerProvider
}

type TracedHTTPRequest struct {
	*http.Request
	*cfdTracer
	ConnIndex uint8
}

func NewTracedHTTPRequest(req *http.Request, connIndex uint8, _ *zerolog.Logger) *TracedHTTPRequest {
	return &TracedHTTPRequest{Request: req, cfdTracer: newNoopTracer(), ConnIndex: connIndex}
}

func (tr *TracedHTTPRequest) ToTracedContext() *TracedContext {
	return &TracedContext{Context: tr.Context(), cfdTracer: tr.cfdTracer}
}

type TracedContext struct {
	context.Context
	*cfdTracer
}

func NewTracedContext(ctx context.Context, _ string, _ *zerolog.Logger) *TracedContext {
	return &TracedContext{Context: ctx, cfdTracer: newNoopTracer()}
}

func newNoopTracer() *cfdTracer {
	return &cfdTracer{TracerProvider: trace.NewNoopTracerProvider()}
}

func (cft *cfdTracer) Tracer() trace.Tracer {
	return cft.TracerProvider.Tracer("origin")
}

func (*cfdTracer) GetSpans() string      { return "" }
func (*cfdTracer) GetProtoSpans() []byte { return nil }
func (*cfdTracer) AddSpans(http.Header)  {}

func End(span trace.Span) {
	if span != nil {
		span.End()
	}
}

func EndWithErrorStatus(span trace.Span, _ error) { End(span) }
func EndWithStatusCode(span trace.Span, _ int)    { End(span) }

func NewNoopSpan() trace.Span {
	return trace.SpanFromContext(context.Background())
}
