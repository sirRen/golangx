package trace

import (
	"github.com/opentracing/opentracing-go"
	"github.com/opentracing/opentracing-go/ext"
	"github.com/opentracing/opentracing-go/log"
	"io"
	"net/http"
)

const defaultComponentName = "test"

// Transport wraps a RoundTripper. If a request is being traced with
// Tracer, Transport will inject the current span into the headers,
// and set HTTP related tags on the span.
type Transport struct {
	// The actual RoundTripper to use for the request. A nil
	// RoundTripper defaults to http.DefaultTransport.
	http.RoundTripper
}

type contextKey struct{}

var keyTracer = contextKey{}

// Tracer holds tracing details for one HTTP request.
type RequestTracer struct {
	tr opentracing.Tracer
	// root opentracing.Span
	sp   opentracing.Span
	opts *clientOptions
}

type clientOptions struct {
	operationName      string
	componentName      string
	disableClientTrace bool
	spanName           string
	peerService        string
}

// ClientOption contols the behavior of TraceRequest.
type ClientOption func(*clientOptions)

// RoundTrip implements the RoundTripper interface.
func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	rt := t.RoundTripper
	if rt == nil {
		rt = http.DefaultTransport
	}
	tracer, ok := req.Context().Value(keyTracer).(*RequestTracer)
	if !ok {
		return rt.RoundTrip(req)
	}

	tracer.start(req)

	ext.HTTPMethod.Set(tracer.sp, req.Method)
	ext.HTTPUrl.Set(tracer.sp, req.URL.String())

	carrier := opentracing.HTTPHeadersCarrier(req.Header)
	tracer.sp.Tracer().Inject(tracer.sp.Context(), opentracing.HTTPHeaders, carrier)
	resp, err := rt.RoundTrip(req)

	if err != nil {
		return resp, err
	}
	ext.HTTPStatusCode.Set(tracer.sp, uint16(resp.StatusCode))
	if req.Method == "HEAD" {
	} else {
		resp.Body = closeTracker{resp.Body, tracer.sp}
	}
	return resp, nil
}

func (h *RequestTracer) start(req *http.Request) opentracing.Span {
	if h.sp != nil {
		return h.sp
	}
	if h.sp == nil {
		parent := opentracing.SpanFromContext(req.Context())
		var spanctx opentracing.SpanContext
		if parent != nil {
			spanctx = parent.Context()
		}
		operationName := h.opts.operationName
		if operationName == "" {
			operationName = "HTTP Client"
		}
		root := h.tr.StartSpan(operationName, opentracing.ChildOf(spanctx))
		h.sp = root
	}

	// ctx := h.root.Context()
	// h.sp = h.tr.StartSpan("HTTP "+req.Method, opentracing.ChildOf(ctx))
	ext.SpanKindRPCClient.Set(h.sp)

	componentName := h.opts.componentName
	if componentName == "" {
		componentName = defaultComponentName
	}
	ext.Component.Set(h.sp, componentName)

	return h.sp
}

type closeTracker struct {
	io.ReadCloser
	sp opentracing.Span
}

func (c closeTracker) Close() error {
	err := c.ReadCloser.Close()
	c.sp.LogFields(log.String("event", "ClosedBody"))
	return err
}