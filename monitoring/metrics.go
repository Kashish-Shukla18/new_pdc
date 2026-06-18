package monitoring

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	framesReceived = prometheus.NewCounter(
		prometheus.CounterOpts{Name: "pdc_frames_received_total", Help: "Total C37.118 data frames received."},
	)
	framesDropped = prometheus.NewCounter(
		prometheus.CounterOpts{Name: "pdc_frames_dropped_total", Help: "Total frames dropped because handler pool was full."},
	)
	framesParsed = prometheus.NewCounter(
		prometheus.CounterOpts{Name: "pdc_frames_parsed_total", Help: "Total frames parsed into readings."},
	)
	parseErrors = prometheus.NewCounter(
		prometheus.CounterOpts{Name: "pdc_parse_errors_total", Help: "Total parser failures."},
	)
	qualityRejected = prometheus.NewCounter(
		prometheus.CounterOpts{Name: "pdc_quality_rejected_total", Help: "Total readings rejected by time/quality checks."},
	)
	queuePublishErrors = prometheus.NewCounter(
		prometheus.CounterOpts{Name: "pdc_queue_publish_errors_total", Help: "Total Kafka publish errors."},
	)
	storeErrors = prometheus.NewCounter(
		prometheus.CounterOpts{Name: "pdc_store_errors_total", Help: "Total Redis/Influx store errors."},
	)
	spoolQueued = prometheus.NewCounter(
		prometheus.CounterOpts{Name: "pdc_spool_queued_total", Help: "Total readings queued to local durable spool."},
	)
	spoolReplayed = prometheus.NewCounter(
		prometheus.CounterOpts{Name: "pdc_spool_replayed_total", Help: "Total readings replayed from local durable spool."},
	)
	processingLatency = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "pdc_frame_processing_seconds",
			Help:    "Frame processing latency from PMU timestamp to persistence.",
			Buckets: []float64{0.001, 0.005, 0.01, 0.02, 0.05, 0.1, 0.25, 0.5, 1.0, 2.0, 5.0},
		},
	)
	sinkInflight = prometheus.NewGauge(
		prometheus.GaugeOpts{Name: "pdc_sink_inflight", Help: "Current number of readings queued in sink channel."},
	)
)

func init() {
	prometheus.MustRegister(
		framesReceived,
		framesDropped,
		framesParsed,
		parseErrors,
		qualityRejected,
		queuePublishErrors,
		storeErrors,
		spoolQueued,
		spoolReplayed,
		processingLatency,
		sinkInflight,
	)
}

func IncFramesReceived()  { framesReceived.Inc() }
func IncFramesDropped()   { framesDropped.Inc() }
func IncFramesParsed()    { framesParsed.Inc() }
func IncSinkInflight()    { sinkInflight.Inc() }
func DecSinkInflight()    { sinkInflight.Dec() }

func IncParseErrors() { parseErrors.Inc() }

func IncQualityRejected() { qualityRejected.Inc() }

func IncQueuePublishErrors() { queuePublishErrors.Inc() }

func IncStoreErrors() { storeErrors.Inc() }

func IncSpoolQueued() { spoolQueued.Inc() }

func IncSpoolReplayed(n int) {
	if n <= 0 {
		return
	}
	spoolReplayed.Add(float64(n))
}

func ObserveLatency(d time.Duration) {
	processingLatency.Observe(d.Seconds())
}

func StartServer(ctx context.Context, addr string) {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	registerConversationHandlers(mux)

	srv := &http.Server{Addr: addr, Handler: mux}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("metrics server error: %v", err)
		}
	}()
}
