package harness

import (
	"bytes"
	"fmt"
	"testing"

	opentracing "github.com/opentracing/opentracing-go"
	"github.com/opentracing/opentracing-go/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

// APICheckCapabilities describes options used by APICheckSuite when testing a Tracer.
type APICheckCapabilities struct {
	CheckBaggageValues bool          // whether to check for propagation of baggage values
	CheckExtract       bool          // whether to check if extracting contexts from carriers works
	CheckInject        bool          // whether to check if injecting contexts works
	Probe              APICheckProbe // optional interface providing methods to check recorded data
}

// APICheckProbe exposes methods for testing data recorded by a Tracer.
type APICheckProbe interface {
	SameTrace(first, second opentracing.Span) bool // whether two spans are from the same trace
	SameTraceContext(opentracing.Span, opentracing.SpanContext) bool
}

// APICheckSuite is a testify suite for checking a Tracer against the OpenTracing API.
type APICheckSuite struct {
	suite.Suite
	opts      APICheckCapabilities
	newTracer func() (tracer opentracing.Tracer, closer func())
	tracer    opentracing.Tracer
	closer    func()
}

// BeforeTest creates a tracer for this specific test invocation.
func (s *APICheckSuite) BeforeTest(suiteName, testName string) {
	s.tracer, s.closer = s.newTracer()
	if s.tracer == nil {
		panic(fmt.Sprintf("newTracer returned nil Tracer before running %s, %s", suiteName, testName))
	}
}

// AfterTest closes the tracer, and clears the test-specific tracer.
func (s *APICheckSuite) AfterTest(suiteName, testName string) {
	if s.closer != nil {
		s.closer()
	}
	s.tracer, s.closer = nil, nil
}

// NewAPICheckSuite returns a testify suite for checking a Tracer against the OpenTracing API.
// It is provided a function that will be executed to create and destroy a tracer for each test
// in the suite, and API test options described by APICheckCapabilities.
func NewAPICheckSuite(
	newTracer func() (tracer opentracing.Tracer, closer func()),
	opts APICheckCapabilities) *APICheckSuite {
	return &APICheckSuite{newTracer: newTracer, opts: opts}
}

func (s *APICheckSuite) TestStartSpan() {
	span := s.tracer.StartSpan(
		"Fry",
		opentracing.Tag{Key: "birthday", Value: "August 14 1974"})
	span.LogFields(
		log.String("hospital", "Brooklyn Pre-Med Hospital"),
		log.String("city", "Old New York"))
	span.Finish()
}

func (s *APICheckSuite) TestStartSpanWithParent() {
	parentSpan := s.tracer.StartSpan("parent")
	assert.NotNil(s.T(), parentSpan)

	span := s.tracer.StartSpan(
		"Leela",
		opentracing.ChildOf(parentSpan.Context()))
	span.Finish()

	span = s.tracer.StartSpan(
		"Leela",
		opentracing.FollowsFrom(parentSpan.Context()),
		opentracing.Tag{Key: "birthplace", Value: "sewers"})
	span.Finish()

	parentSpan.Finish()
}

func (s *APICheckSuite) TestSetOperationName() {
	span := s.tracer.StartSpan("").SetOperationName("Farnsworth")
	span.Finish()
}

func (s *APICheckSuite) TestSpanTagValueTypes() {
	span := s.tracer.StartSpan("ManyTypes")
	span.
		SetTag("an_int", 9).
		SetTag("a_bool", true).
		SetTag("a_string", "aoeuidhtns")
}

func (s *APICheckSuite) TestSpanTagsWithChaining() {
	span := s.tracer.StartSpan("Farnsworth")
	span.
		SetTag("birthday", "9 April, 2841").
		SetTag("loves", "different lengths of wires")
	span.
		SetTag("unicode_val", "non-ascii: \u200b").
		SetTag("unicode_key_\u200b", "ascii val")
	span.Finish()
}

func (s *APICheckSuite) TestSpanLogs() {
	span := s.tracer.StartSpan("Fry")
	span.LogKV(
		"frozen.year", 1999,
		"frozen.place", "Cryogenics Labs")
	span.LogKV(
		"defrosted.year", 2999,
		"defrosted.place", "Cryogenics Labs")

	// XXX add LogFields
	// XXX add LogRecords FinishOptions with timestamp
	span.Finish()
}

func assertEmptyBaggage(t *testing.T, spanContext opentracing.SpanContext) {
	if !assert.NotNil(t, spanContext, "assertEmptyBaggage got empty context") {
		return
	}
	spanContext.ForeachBaggageItem(func(k, v string) bool {
		assert.Fail(t, "new span shouldn't have baggage")
		return false
	})
}

func (s *APICheckSuite) TestSpanBaggage() {
	span := s.tracer.StartSpan("Fry")
	assertEmptyBaggage(s.T(), span.Context())

	spanRef := span.SetBaggageItem("Kiff-loves", "Amy")
	assert.Exactly(s.T(), spanRef, span)

	val := span.BaggageItem("Kiff-loves")
	if s.opts.CheckBaggageValues {
		assert.Equal(s.T(), "Amy", val)
	} else {
		s.T().Log("Baggage propagation not supported, not checking")
	}
	span.Finish()
}

func (s *APICheckSuite) TestContextBaggage() {
	span := s.tracer.StartSpan("Fry")
	assertEmptyBaggage(s.T(), span.Context())

	span.SetBaggageItem("Kiff-loves", "Amy")
	if s.opts.CheckBaggageValues {
		called := false
		span.Context().ForeachBaggageItem(func(k, v string) bool {
			assert.False(s.T(), called)
			called = true
			assert.Equal(s.T(), "Kiff-loves", k)
			assert.Equal(s.T(), "Amy", v)
			return true
		})
	} else {
		s.T().Log("Baggage propagation not supported, not checking")
	}
	span.Finish()
}

func (s *APICheckSuite) TestTextPropagation() {
	span := s.tracer.StartSpan("Bender")
	textCarrier := opentracing.TextMapCarrier{}
	err := span.Tracer().Inject(span.Context(), opentracing.TextMap, textCarrier)
	assert.NoError(s.T(), err)

	extractedContext, err := s.tracer.Extract(opentracing.TextMap, textCarrier)
	if s.opts.CheckExtract {
		assert.NoError(s.T(), err)
		assertEmptyBaggage(s.T(), extractedContext)
	} else {
		s.T().Log("Tracer.Extract not supported, not checking")
	}
	// XXX add option to check if propagation "works"
	span.Finish()
}

func (s *APICheckSuite) TestHTTPPropagation() {
	span := s.tracer.StartSpan("Bender")
	textCarrier := opentracing.HTTPHeadersCarrier{}
	// XXX add same test cases around valid HTTP header characters, casing
	err := span.Tracer().Inject(span.Context(), opentracing.HTTPHeaders, textCarrier)
	assert.NoError(s.T(), err)

	extractedContext, err := s.tracer.Extract(opentracing.HTTPHeaders, textCarrier)
	if s.opts.CheckExtract {
		assert.NoError(s.T(), err)
		assertEmptyBaggage(s.T(), extractedContext)
	} else {
		s.T().Log("Tracer.Extract not supported, skipping")
	}
	// XXX add option to check if propagation "works"
	span.Finish()
}

func (s *APICheckSuite) TestBinaryPropagation() {
	span := s.tracer.StartSpan("Bender")
	buf := new(bytes.Buffer)
	err := span.Tracer().Inject(span.Context(), opentracing.Binary, buf)
	assert.NoError(s.T(), err)

	extractedContext, err := s.tracer.Extract(opentracing.Binary, buf)
	if s.opts.CheckExtract {
		assert.NoError(s.T(), err)
		assertEmptyBaggage(s.T(), extractedContext)
	} else {
		s.T().Log("Tracer.Extract not supported, skipping")
	}
	// XXX add option to check if propagation "works"
	span.Finish()
}

func (s *APICheckSuite) TestMandatoryFormats() {
	formats := []struct{ Format, Carrier interface{} }{
		{opentracing.TextMap, opentracing.TextMapCarrier{}},
		{opentracing.HTTPHeaders, opentracing.HTTPHeadersCarrier{}},
		{opentracing.Binary, new(bytes.Buffer)},
	}
	span := s.tracer.StartSpan("Bender")
	for _, fmtCarrier := range formats {
		err := span.Tracer().Inject(span.Context(), fmtCarrier.Format, fmtCarrier.Carrier)
		assert.NoError(s.T(), err)
		spanCtx, err := s.tracer.Extract(fmtCarrier.Format, fmtCarrier.Carrier)
		if s.opts.CheckExtract {
			assert.NoError(s.T(), err)
			assertEmptyBaggage(s.T(), spanCtx)
		} else {
			s.T().Log("Tracer.Extract not supported, skipping")
		}
	}
}

func (s *APICheckSuite) TestUnknownFormat() {
	customFormat := "kiss my shiny metal ..."
	span := s.tracer.StartSpan("Bender")

	err := span.Tracer().Inject(span.Context(), customFormat, nil)
	if s.opts.CheckInject {
		assert.Equal(s.T(), opentracing.ErrUnsupportedFormat, err)
	} else {
		s.T().Log("Tracer.Inject not supported, not checking")
	}
	ctx, err := s.tracer.Extract(customFormat, nil)
	assert.Nil(s.T(), ctx)
	if s.opts.CheckExtract {
		assert.Equal(s.T(), opentracing.ErrUnsupportedFormat, err)
	} else {
		s.T().Log("Tracer.Inject not supported, not checking")
	}
}