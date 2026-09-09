package monitoring

import (
	"context"
	"log"
	"net/http"
	"time"

	"pdc/internal/instance"

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
	rawFramesPublished = prometheus.NewCounter(
		prometheus.CounterOpts{Name: "pdc_raw_frames_published_total", Help: "Total raw C37.118 frames published to Kafka."},
	)
	rawFramesConsumed = prometheus.NewCounter(
		prometheus.CounterOpts{Name: "pdc_raw_frames_consumed_total", Help: "Total raw C37.118 frames consumed from Kafka."},
	)
	readingsConsumed = prometheus.NewCounter(
		prometheus.CounterOpts{Name: "pdc_readings_consumed_total", Help: "Total parsed readings consumed from Kafka (all fan-out groups)."},
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
		prometheus.CounterOpts{Name: "pdc_store_errors_total", Help: "Total Redis/Postgres store errors."},
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
	rawKafkaQueueDropped = prometheus.NewCounter(
		prometheus.CounterOpts{Name: "pdc_raw_kafka_queue_dropped_total", Help: "Raw frames dropped because the Kafka ingress queue was full."},
	)
	dashboardQueueDropped = prometheus.NewCounter(
		prometheus.CounterOpts{Name: "pdc_dashboard_queue_dropped_total", Help: "Readings dropped because the dashboard update queue was full."},
	)
	connectionReconnects = prometheus.NewCounterVec(
		prometheus.CounterOpts{Name: "pdc_connection_reconnects_total", Help: "TCP session reconnect attempts after connection errors."},
		[]string{"pmu"},
	)
)

func init() {
	prometheus.MustRegister(
		framesReceived,
		framesDropped,
		rawFramesPublished,
		rawFramesConsumed,
		readingsConsumed,
		framesParsed,
		parseErrors,
		qualityRejected,
		queuePublishErrors,
		storeErrors,
		spoolQueued,
		spoolReplayed,
		processingLatency,
		sinkInflight,
		rawKafkaQueueDropped,
		dashboardQueueDropped,
		connectionReconnects,
	)
}

func IncFramesReceived()     { framesReceived.Inc() }
func IncFramesDropped()      { framesDropped.Inc() }
func IncRawFramesPublished() { rawFramesPublished.Inc() }
func IncRawFramesConsumed()  { rawFramesConsumed.Inc() }
func IncReadingsConsumed()   { readingsConsumed.Inc() }
func IncFramesParsed()       { framesParsed.Inc() }
func IncSinkInflight()       { sinkInflight.Inc() }
func DecSinkInflight()       { sinkInflight.Dec() }

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

func IncRawKafkaQueueDropped()  { rawKafkaQueueDropped.Inc() }
func IncDashboardQueueDropped() { dashboardQueueDropped.Inc() }

func IncConnectionReconnect(pmu string) {
	if pmu == "" {
		pmu = "unknown"
	}
	connectionReconnects.WithLabelValues(pmu).Inc()
}

func ObserveLatency(d time.Duration) {
	processingLatency.Observe(d.Seconds())
}

func StartServer(ctx context.Context, addr string, registerExtra ...func(*http.ServeMux)) error {
	ln, err := instance.ListenOrExit("metrics", instance.NormalizeAddr(addr))
	if err != nil {
		return err
	}

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	registerConversationHandlers(mux)
	for _, fn := range registerExtra {
		if fn != nil {
			fn(mux)
		}
	}
	StartLatencyReporter(ctx.Done())

	srv := &http.Server{Handler: mux}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	go func() {
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Printf("metrics server error: %v", err)
		}
	}()
	return nil
}
