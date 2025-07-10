package driver

import (
	"context"
	"errors"
	"math"
	"sync"
	"time"

	"github.com/signadot/hotrod/services/location"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	"github.com/signadot/hotrod/pkg/config"
	"github.com/signadot/hotrod/pkg/log"
	"github.com/signadot/hotrod/pkg/pool"
	"github.com/signadot/hotrod/services/route"
)

type bestETA struct {
	tracer trace.Tracer
	route  route.Interface
	pool   *pool.Pool
	logger log.Factory
}

// Response contains ETA for a trip.
type Response struct {
	DriverID string        `json:"driverIdentifier"` 
	ETA      time.Duration
}

func newBestETA(tracerProvider trace.TracerProvider, tracer trace.Tracer, logger log.Factory) *bestETA {
	return &bestETA{
		tracer: tracer,
		route:  route.NewClient(tracerProvider, logger, config.GetRouteAddr()),
		pool:   pool.New(config.GetDriverWorkerPoolSize()),
		logger: logger,
	}
}

func (eta *bestETA) Get(ctx context.Context, dispatchReq *DispatchRequest,
	drivers []*Driver) (*Response, error) {
	ctx, span := eta.tracer.Start(ctx, "CalculateBestRoute", trace.WithSpanKind(trace.SpanKindClient))
	defer span.End()

	// get all routes from the drivers to the pick up location
	results := eta.getRoutes(ctx, dispatchReq.PickupLocation, drivers)
	eta.logger.For(ctx).Info("Found routes", zap.Any("routes", results))

	// search the one with the best ETA
	resp := &Response{ETA: math.MaxInt64}
	for _, result := range results {
		if result.err != nil {
			span.SetStatus(codes.Error, result.err.E
