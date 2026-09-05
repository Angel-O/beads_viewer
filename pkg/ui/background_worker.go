// Package ui provides the terminal user interface for beads_viewer.
// This file implements the BackgroundWorker for off-thread data processing.
package ui

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"runtime"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Dicklesworthstone/beads_viewer/pkg/analysis"
	dbg "github.com/Dicklesworthstone/beads_viewer/pkg/debug"
	"github.com/Dicklesworthstone/beads_viewer/pkg/loader"
	"github.com/Dicklesworthstone/beads_viewer/pkg/model"
	"github.com/Dicklesworthstone/beads_viewer/pkg/recipe"
	repositorypkg "github.com/Dicklesworthstone/beads_viewer/pkg/repository"
	"github.com/Dicklesworthstone/beads_viewer/pkg/watcher"
)

// WorkerState represents the current state of the background worker.
type WorkerState int

const (
	// WorkerIdle means the worker is waiting for file changes.
	WorkerIdle WorkerState = iota
	// WorkerProcessing means the worker is building a new snapshot.
	WorkerProcessing
	// WorkerStopped means the worker has been stopped.
	WorkerStopped
)

const (
	minWorkerMessageBuffer = 2
)

// WorkerLogLevel controls background worker log verbosity.
type WorkerLogLevel int

const (
	LogLevelNone WorkerLogLevel = iota
	LogLevelError
	LogLevelWarn
	LogLevelInfo
	LogLevelDebug
	LogLevelTrace
)

func (l WorkerLogLevel) String() string {
	switch l {
	case LogLevelError:
		return "error"
	case LogLevelWarn:
		return "warn"
	case LogLevelInfo:
		return "info"
	case LogLevelDebug:
		return "debug"
	case LogLevelTrace:
		return "trace"
	default:
		return "none"
	}
}

func parseWorkerLogLevel(raw string) WorkerLogLevel {
	value := strings.TrimSpace(strings.ToLower(raw))
	switch value {
	case "none", "off", "0":
		return LogLevelNone
	case "error", "err", "1":
		return LogLevelError
	case "warn", "warning", "2":
		return LogLevelWarn
	case "info", "3":
		return LogLevelInfo
	case "debug", "4":
		return LogLevelDebug
	case "trace", "5":
		return LogLevelTrace
	default:
		return LogLevelWarn
	}
}

// WorkerError wraps errors with phase and retry context.
type WorkerError struct {
	Phase   string    // "load", "parse", "analyze_phase1", "analyze_phase2"
	Cause   error     // The underlying error
	Time    time.Time // When the error occurred
	Retries int       // Number of retry attempts
}

func (e WorkerError) Error() string {
	return fmt.Sprintf("%s failed: %v (retries: %d)", e.Phase, e.Cause, e.Retries)
}

func (e WorkerError) Unwrap() error {
	return e.Cause
}

type WorkerHealth struct {
	Started       bool
	Alive         bool
	LastHeartbeat time.Time
	RecoveryCount int
	UptimeSince   time.Time

	IdleGCEnabled      bool
	IdleGCCount        uint64
	IdleGCTotal        time.Duration
	IdleGCLastDuration time.Duration
	IdleGCLastAt       time.Time
}

// WorkerMetrics captures the most recent metrics snapshot.
type WorkerMetrics struct {
	ProcessingCount      uint64
	ProcessingDuration   time.Duration
	Phase1Duration       time.Duration
	Phase2Duration       time.Duration
	CoalesceCount        int64
	QueueDepth           int64
	SnapshotVersion      uint64
	SnapshotSizeBytes    int64
	PoolHits             uint64
	PoolMisses           uint64
	GCPauseDelta         time.Duration
	SwapLatency          time.Duration
	UIUpdateLatency      time.Duration
	LastFileChangeAt     time.Time
	LastSnapshotReadyAt  time.Time
	IncrementalListCount uint64
	FullListCount        uint64
	IncrementalListRatio float64
}

type workerMetrics struct {
	processingCount        atomic.Uint64
	lastProcessingNs       atomic.Int64
	lastPhase1Ns           atomic.Int64
	lastPhase2Ns           atomic.Int64
	lastCoalesceCount      atomic.Int64
	lastQueueDepth         atomic.Int64
	lastSnapshotSizeBytes  atomic.Int64
	lastGCPauseDeltaNs     atomic.Int64
	lastSwapLatencyNs      atomic.Int64
	lastUIUpdateLatencyNs  atomic.Int64
	lastSnapshotReadyUnix  atomic.Int64
	lastFileChangeUnixNano atomic.Int64
	poolHits               atomic.Uint64
	poolMisses             atomic.Uint64
	snapshotVersion        atomic.Uint64
	incrementalListCount   atomic.Uint64
	fullListCount          atomic.Uint64
}

// BackgroundWorker manages background processing of beads data.
// It owns the file watcher, implements coalescing, and builds snapshots
// off the UI thread.
type BackgroundWorker struct {
	// Configuration
	beadsPath           string
	issueChangePath     string
	metadataChangePaths []string
	catalogPath         string
	debounceDelay       time.Duration
	heartbeatInterval   time.Duration
	watchdogInterval    time.Duration
	heartbeatTimeout    time.Duration
	processingTimeout   time.Duration
	maxRecoveries       int
	sourceRetryBase     time.Duration
	sourceRetryMax      time.Duration

	// State
	// SAFETY: never invoke logging, callbacks, channel operations, or other
	// caller-controlled work while holding mu. Go's package logger ultimately
	// calls a replaceable io.Writer, which may re-enter worker accessors.
	mu                  sync.RWMutex
	sendMu              sync.Mutex // Serializes priority-aware mailbox replacement.
	state               WorkerState
	dirty               bool // True if a change came in while processing
	processScheduled    bool // True after process() is queued but before it starts processing
	snapshot            *DataSnapshot
	started             bool // True if Start() has been called
	watchdogStarted     bool
	startTime           time.Time
	lastHeartbeat       time.Time
	processingStart     time.Time
	recoveryCount       int
	recovering          bool
	generation          uint64
	lastHash            string // Content hash of last processed snapshot (for dedup)
	forceNext           bool   // Force the next snapshot build even if content hash matches
	refreshBDExportNext bool   // Refresh the bd compatibility export before the next snapshot
	sourceRetryTimer    *time.Timer
	catalogRetryTimer   *time.Timer
	catalogGeneration   uint64
	catalog             repositorypkg.Catalog
	catalogLoader       func(string, []model.Issue) (repositorypkg.Catalog, error)
	hubScopeMemberIDs   func(context.Context) ([]string, error)
	catalogFailed       bool
	currentRecipe       *recipe.Recipe
	currentRecipeID     string // Recipe identifier for snapshot rebuild keys
	currentRecipeHash   string // Recipe fingerprint for rebuild keys (bv-4ilb)
	logLevel            WorkerLogLevel
	logJSON             bool
	metricsEnabled      bool
	tracePath           string
	traceFile           *os.File
	traceMu             sync.Mutex

	// Idle-time GC management (bv-4yje).
	idleGCEnabled     bool
	idleGCThreshold   time.Duration
	idleGCMinInterval time.Duration
	idleGCCheckEvery  time.Duration
	idleGCGCPercent   int

	lastActivityUnixNano atomic.Int64
	lastIdleGCUnixNano   atomic.Int64

	idleGCCount             atomic.Uint64
	idleGCTotalNanos        atomic.Int64
	idleGCLastDurationNanos atomic.Int64
	idleGCLastAtUnixNano    atomic.Int64

	idleGCAppliedGCPercent bool
	idleGCPrevGCPercent    int
	idleGCFunc             func()

	pendingChanges            atomic.Int64
	sourceRefreshChangeCutoff atomic.Int64
	coalesceCount             atomic.Int64
	metrics                   workerMetrics

	// Error tracking
	lastError  *WorkerError // Most recent error (nil if last operation succeeded)
	errorCount int          // Consecutive error count for backoff

	// Components
	watcher          *watcher.Watcher
	hubChangeWatcher *watcher.Watcher
	hubConfigWatcher *watcher.Watcher
	issueSource      ChangeSource
	metadataSources  []ChangeSource
	sourceSource     ChangeSource
	catalogSource    ChangeSource
	msgCh            chan tea.Msg

	// Lifecycle
	ctx         context.Context
	cancel      context.CancelFunc
	loopCtx     context.Context
	loopCancel  context.CancelFunc
	done        chan struct{}
	loopStarted bool
	workWG      sync.WaitGroup
}

type IdleGCConfig struct {
	Enabled     bool
	Threshold   time.Duration
	CheckEvery  time.Duration
	MinInterval time.Duration
	GCPercent   int
}

// WorkerConfig configures the BackgroundWorker.
type WorkerConfig struct {
	BeadsPath           string
	SelectedIssuePath   string
	IssueChangePath     string
	MetadataChangePaths []string
	DebounceDelay       time.Duration
	MessageBuffer       int // Buffer size for worker -> UI messages (default: 8)
	CatalogLoader       RepositoryMetadataProvider
	IssueSource         ChangeSource   // issue content changes rebuild snapshots
	MetadataSources     []ChangeSource // metadata changes invalidate external-only views
	SourceChangeSource  ChangeSource   // source/export changes use source-refresh semantics
	CatalogChangeSource ChangeSource   // catalog changes rebuild repository metadata only

	IdleGC *IdleGCConfig

	// Watchdog configuration (bv-03h1). Zero values use defaults.
	HeartbeatInterval time.Duration // default: 5s
	WatchdogInterval  time.Duration // default: 10s
	HeartbeatTimeout  time.Duration // default: 30s
	ProcessingTimeout time.Duration // default: 30s
	MaxRecoveries     int           // default: 3
	HubChangeSignal   string        // application-owned Hub generation file
	CatalogPath       string        // Resolved repository catalog source
	HubScopeMemberIDs func(context.Context) ([]string, error)
	SourceRetryBase   time.Duration // default: 1s
	SourceRetryMax    time.Duration // default: 30s
}

// NewBackgroundWorker creates a new background worker.
func NewBackgroundWorker(cfg WorkerConfig) (*BackgroundWorker, error) {
	ctx, cancel := context.WithCancel(context.Background())
	initialized := false
	defer func() {
		if !initialized {
			cancel()
		}
	}()

	if cfg.DebounceDelay == 0 {
		cfg.DebounceDelay = envDurationMilliseconds("BV_DEBOUNCE_MS", 200*time.Millisecond)
	}
	if cfg.MessageBuffer <= 0 {
		cfg.MessageBuffer = envPositiveIntOr("BV_CHANNEL_BUFFER", 8)
	}
	if cfg.MessageBuffer < minWorkerMessageBuffer {
		cfg.MessageBuffer = minWorkerMessageBuffer
	}
	if cfg.HeartbeatInterval == 0 {
		cfg.HeartbeatInterval = envDurationSeconds("BV_HEARTBEAT_INTERVAL_S", 5*time.Second)
	}
	if cfg.WatchdogInterval == 0 {
		cfg.WatchdogInterval = envDurationSeconds("BV_WATCHDOG_INTERVAL_S", 10*time.Second)
	}
	if cfg.HeartbeatTimeout == 0 {
		cfg.HeartbeatTimeout = 30 * time.Second
	}
	if cfg.ProcessingTimeout == 0 {
		cfg.ProcessingTimeout = 30 * time.Second
	}
	if cfg.MaxRecoveries == 0 {
		cfg.MaxRecoveries = 3
	}
	if cfg.SourceRetryBase <= 0 {
		cfg.SourceRetryBase = time.Second
	}
	if cfg.SourceRetryMax <= 0 {
		cfg.SourceRetryMax = 30 * time.Second
	}
	if cfg.SourceRetryMax < cfg.SourceRetryBase {
		cfg.SourceRetryMax = cfg.SourceRetryBase
	}

	logLevel := parseWorkerLogLevel(os.Getenv("BV_WORKER_LOG_LEVEL"))
	metricsEnabled := envBool("BV_WORKER_METRICS")
	tracePath := strings.TrimSpace(os.Getenv("BV_WORKER_TRACE"))
	logJSON := os.Getenv("BV_ROBOT") == "1"

	idleGCConfig := IdleGCConfig{
		Enabled:     true,
		Threshold:   5 * time.Second,
		CheckEvery:  1 * time.Second,
		MinInterval: 30 * time.Second,
		GCPercent:   200,
	}
	if cfg.IdleGC != nil {
		idleGCConfig = *cfg.IdleGC
		if idleGCConfig.Threshold == 0 {
			idleGCConfig.Threshold = 5 * time.Second
		}
		if idleGCConfig.CheckEvery == 0 {
			idleGCConfig.CheckEvery = 1 * time.Second
		}
		if idleGCConfig.MinInterval == 0 {
			idleGCConfig.MinInterval = 30 * time.Second
		}
		if idleGCConfig.GCPercent == 0 {
			idleGCConfig.GCPercent = 200
		}
	}

	selectedIssuePath := cfg.SelectedIssuePath
	if selectedIssuePath == "" {
		selectedIssuePath = cfg.BeadsPath
	}
	issueChangePath := cfg.IssueChangePath
	if issueChangePath == "" {
		issueChangePath = selectedIssuePath
	}
	w := &BackgroundWorker{
		beadsPath:           selectedIssuePath,
		issueChangePath:     issueChangePath,
		metadataChangePaths: append([]string(nil), cfg.MetadataChangePaths...),
		catalogPath:         cfg.CatalogPath,
		debounceDelay:       cfg.DebounceDelay,
		heartbeatInterval:   cfg.HeartbeatInterval,
		watchdogInterval:    cfg.WatchdogInterval,
		heartbeatTimeout:    cfg.HeartbeatTimeout,
		processingTimeout:   cfg.ProcessingTimeout,
		maxRecoveries:       cfg.MaxRecoveries,
		sourceRetryBase:     cfg.SourceRetryBase,
		sourceRetryMax:      cfg.SourceRetryMax,
		logLevel:            logLevel,
		logJSON:             logJSON,
		metricsEnabled:      metricsEnabled,
		tracePath:           tracePath,
		catalogLoader:       cfg.CatalogLoader,
		hubScopeMemberIDs:   cfg.HubScopeMemberIDs,
		generation:          1, // Generation zero is reserved for non-worker messages.
		state:               WorkerIdle,
		msgCh:               make(chan tea.Msg, cfg.MessageBuffer),
		ctx:                 ctx,
		cancel:              cancel,
		done:                make(chan struct{}),

		idleGCEnabled:     idleGCConfig.Enabled,
		idleGCThreshold:   idleGCConfig.Threshold,
		idleGCMinInterval: idleGCConfig.MinInterval,
		idleGCCheckEvery:  idleGCConfig.CheckEvery,
		idleGCGCPercent:   idleGCConfig.GCPercent,
		idleGCFunc:        runtime.GC,
	}
	if w.catalogLoader == nil {
		w.catalogLoader = defaultRepositoryMetadataProvider
	}
	w.lastActivityUnixNano.Store(time.Now().UnixNano())

	// Initialize neutral change sources. Paths remain a convenience for the
	// normal file-backed adapter; tests and other callers can inject sources.
	w.issueSource = cfg.IssueSource
	if w.issueSource == nil && w.issueChangePath != "" {
		fw, err := watcher.NewWatcher(w.issueChangePath,
			watcher.WithDebounceDuration(cfg.DebounceDelay),
		)
		if err != nil {
			return nil, err
		}
		w.watcher = fw
		w.issueSource = fw
	}
	w.metadataSources = append([]ChangeSource(nil), cfg.MetadataSources...)
	for _, path := range w.metadataChangePaths {
		if strings.TrimSpace(path) == "" {
			continue
		}
		metadataWatcher, err := watcher.NewWatcher(path,
			watcher.WithDebounceDuration(cfg.DebounceDelay),
			watcher.WithContentCheck(true),
		)
		if err != nil {
			return nil, err
		}
		w.metadataSources = append(w.metadataSources, metadataWatcher)
	}
	w.sourceSource = cfg.SourceChangeSource
	if w.sourceSource == nil && cfg.HubChangeSignal != "" {
		hubWatcher, err := watcher.NewWatcher(cfg.HubChangeSignal,
			watcher.WithDebounceDuration(cfg.DebounceDelay),
			watcher.WithContentCheck(true),
		)
		if err != nil {
			return nil, err
		}
		w.hubChangeWatcher = hubWatcher
		w.sourceSource = hubWatcher
	}
	w.catalogSource = cfg.CatalogChangeSource
	if w.catalogSource == nil && cfg.CatalogPath != "" {
		configWatcher, err := watcher.NewWatcher(cfg.CatalogPath,
			watcher.WithDebounceDuration(cfg.DebounceDelay),
			watcher.WithContentCheck(true),
		)
		if err != nil {
			return nil, err
		}
		w.hubConfigWatcher = configWatcher
		w.catalogSource = configWatcher
	}

	initialized = true
	return w, nil
}

// Messages returns a channel of Bubble Tea messages emitted by the worker.
// The channel is owned by the worker and is never closed; use Done() to stop waiting.
func (w *BackgroundWorker) Messages() <-chan tea.Msg {
	if w == nil {
		return nil
	}
	return w.msgCh
}

// Done is closed when the worker is stopped.
func (w *BackgroundWorker) Done() <-chan struct{} {
	if w == nil {
		ch := make(chan struct{})
		close(ch)
		return ch
	}
	return w.ctx.Done()
}

// Metrics returns the latest metrics snapshot.
func (w *BackgroundWorker) Metrics() WorkerMetrics {
	if w == nil {
		return WorkerMetrics{}
	}
	lastSnapshotReady := time.Time{}
	if unix := w.metrics.lastSnapshotReadyUnix.Load(); unix > 0 {
		lastSnapshotReady = time.Unix(0, unix)
	}
	lastFileChange := time.Time{}
	if unix := w.metrics.lastFileChangeUnixNano.Load(); unix > 0 {
		lastFileChange = time.Unix(0, unix)
	}
	incremental := w.metrics.incrementalListCount.Load()
	full := w.metrics.fullListCount.Load()
	ratio := 0.0
	if total := incremental + full; total > 0 {
		ratio = float64(incremental) / float64(total)
	}

	return WorkerMetrics{
		ProcessingCount:      w.metrics.processingCount.Load(),
		ProcessingDuration:   time.Duration(w.metrics.lastProcessingNs.Load()),
		Phase1Duration:       time.Duration(w.metrics.lastPhase1Ns.Load()),
		Phase2Duration:       time.Duration(w.metrics.lastPhase2Ns.Load()),
		CoalesceCount:        w.metrics.lastCoalesceCount.Load(),
		QueueDepth:           w.metrics.lastQueueDepth.Load(),
		SnapshotVersion:      w.metrics.snapshotVersion.Load(),
		SnapshotSizeBytes:    w.metrics.lastSnapshotSizeBytes.Load(),
		PoolHits:             w.metrics.poolHits.Load(),
		PoolMisses:           w.metrics.poolMisses.Load(),
		GCPauseDelta:         time.Duration(w.metrics.lastGCPauseDeltaNs.Load()),
		SwapLatency:          time.Duration(w.metrics.lastSwapLatencyNs.Load()),
		UIUpdateLatency:      time.Duration(w.metrics.lastUIUpdateLatencyNs.Load()),
		LastSnapshotReadyAt:  lastSnapshotReady,
		LastFileChangeAt:     lastFileChange,
		IncrementalListCount: incremental,
		FullListCount:        full,
		IncrementalListRatio: ratio,
	}
}

func (w *BackgroundWorker) openTraceFile() {
	if w == nil || w.tracePath == "" {
		return
	}
	w.traceMu.Lock()
	if w.traceFile != nil {
		w.traceMu.Unlock()
		return
	}
	f, err := os.OpenFile(w.tracePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		w.traceMu.Unlock()
		log.Printf("background worker: trace_open_failed path=%s error=%v", w.tracePath, err)
		return
	}
	w.traceFile = f
	w.traceMu.Unlock()
}

func (w *BackgroundWorker) closeTraceFile() {
	if w == nil {
		return
	}
	w.traceMu.Lock()
	f := w.traceFile
	if f == nil {
		w.traceMu.Unlock()
		return
	}
	w.traceFile = nil
	w.traceMu.Unlock()
	if err := f.Close(); err != nil {
		w.logEvent(LogLevelWarn, "trace_close_failed", map[string]any{
			"path":  w.tracePath,
			"error": err.Error(),
		})
	}
}

func (w *BackgroundWorker) logEvent(level WorkerLogLevel, event string, fields map[string]any) {
	if w == nil || level == LogLevelNone {
		return
	}
	w.traceMu.Lock()
	traceEnabled := w.traceFile != nil
	w.traceMu.Unlock()
	if !traceEnabled && (w.logLevel == LogLevelNone || level > w.logLevel) {
		return
	}

	payload := map[string]any{
		"ts":        time.Now().UTC().Format(time.RFC3339Nano),
		"level":     level.String(),
		"component": "background_worker",
		"event":     event,
	}
	for k, v := range fields {
		payload[k] = v
	}
	b, err := json.Marshal(payload)
	if err != nil {
		log.Printf("background worker: failed to marshal log event %s: %v", event, err)
		return
	}

	if w.logLevel != LogLevelNone && level <= w.logLevel {
		log.Printf("%s", b)
	}
	if traceEnabled {
		w.traceMu.Lock()
		if w.traceFile != nil {
			_, _ = w.traceFile.Write(append(b, '\n'))
		}
		w.traceMu.Unlock()
	}
}

func (w *BackgroundWorker) noteFileChange(t time.Time) {
	if w == nil {
		return
	}
	w.metrics.lastFileChangeUnixNano.Store(t.UnixNano())
	depth := w.pendingChanges.Add(1)
	w.logEvent(LogLevelTrace, "file_change", map[string]any{
		"queue_depth": depth,
	})
}

func (w *BackgroundWorker) recordUIUpdateLatency(d time.Duration) {
	if w == nil {
		return
	}
	w.metrics.lastUIUpdateLatencyNs.Store(d.Nanoseconds())
	w.logEvent(LogLevelDebug, "ui_update_latency", map[string]any{
		"latency_ms": float64(d.Microseconds()) / 1000.0,
	})
}

func (w *BackgroundWorker) changeSources() []ChangeSource {
	if w == nil {
		return nil
	}
	sources := make([]ChangeSource, 0, 3+len(w.metadataSources))
	for _, source := range []ChangeSource{w.issueSource, w.sourceSource, w.catalogSource} {
		if source != nil {
			sources = append(sources, source)
		}
	}
	sources = append(sources, w.metadataSources...)
	return sources
}

// Start begins watching for file changes and processing in the background.
// Start is idempotent - calling it multiple times has no effect.
// Returns error if the worker has been stopped.
func (w *BackgroundWorker) Start() error {
	w.mu.Lock()
	if w.state == WorkerStopped {
		w.mu.Unlock()
		return fmt.Errorf("worker has been stopped")
	}
	if w.started {
		w.mu.Unlock()
		return nil // Already started
	}
	w.started = true
	// Publish the latch that belongs to this Start attempt before releasing the
	// mutex. Stop must never capture the constructor's never-closed placeholder
	// while Start is between watcher startup and process-loop launch.
	done := make(chan struct{})
	w.done = done
	w.workWG.Add(1)
	defer w.workWG.Done()
	now := time.Now()
	if w.startTime.IsZero() {
		w.startTime = now
	}
	w.lastHeartbeat = now
	w.recordActivityAt(now)
	idleGCEnabled := w.idleGCEnabled
	idleGCGCPercent := w.idleGCGCPercent
	idleGCCheckEvery := w.idleGCCheckEvery
	w.mu.Unlock()

	w.openTraceFile()
	w.logEvent(LogLevelInfo, "worker_start", map[string]any{
		"beads_path":        w.beadsPath,
		"hub_change_signal": cfgString(w.hubChangeWatcher),
	})

	// Avoid mutating global GC percent in tests (it can interfere with parallel test execution).
	if os.Getenv("BV_TEST_MODE") != "" {
		idleGCGCPercent = 0
	}

	startedSources := make([]ChangeSource, 0)
	for _, source := range w.changeSources() {
		if err := source.Start(); err != nil {
			for _, startedSource := range startedSources {
				startedSource.Stop()
			}
			w.mu.Lock()
			w.started = false
			w.mu.Unlock()
			w.closeTraceFile()
			return err
		}
		startedSources = append(startedSources, source)
	}
	w.mu.Lock()
	if w.state == WorkerStopped || w.ctx.Err() != nil {
		w.started = false
		w.mu.Unlock()
		for _, startedSource := range startedSources {
			startedSource.Stop()
		}
		w.closeTraceFile()
		return fmt.Errorf("worker was stopped during start")
	}
	if idleGCEnabled && idleGCGCPercent > 0 {
		if w.state != WorkerStopped && w.started && !w.idleGCAppliedGCPercent {
			w.idleGCPrevGCPercent = debug.SetGCPercent(idleGCGCPercent)
			w.idleGCAppliedGCPercent = true
		}
	}
	if idleGCEnabled && idleGCCheckEvery > 0 {
		go w.idleGCLoop(idleGCCheckEvery)
	}
	w.lastHeartbeat = time.Now()
	w.startProcessLoopLocked(done)
	w.mu.Unlock()
	w.startWatchdog()
	return nil
}

// Stop halts the background worker and cleans up resources.
// Stop is idempotent - calling it multiple times has no effect.
func (w *BackgroundWorker) Stop() {
	w.mu.Lock()
	if w.state == WorkerStopped {
		loopStarted := w.loopStarted
		done := w.done
		w.mu.Unlock()
		if loopStarted {
			<-done
		}
		w.workWG.Wait()
		return
	}
	w.state = WorkerStopped
	w.processScheduled = false
	wasStarted := w.started
	loopStarted := w.loopStarted
	loopCancel := w.loopCancel
	done := w.done
	retryTimer := w.sourceRetryTimer
	w.sourceRetryTimer = nil
	catalogRetryTimer := w.catalogRetryTimer
	w.catalogRetryTimer = nil
	w.loopCancel = nil
	restoreGCPercent := w.idleGCAppliedGCPercent
	prevGCPercent := w.idleGCPrevGCPercent
	w.idleGCAppliedGCPercent = false
	w.mu.Unlock()

	if restoreGCPercent {
		debug.SetGCPercent(prevGCPercent)
	}

	w.cancel()
	if loopCancel != nil {
		loopCancel()
	}
	if retryTimer != nil {
		retryTimer.Stop()
	}
	if catalogRetryTimer != nil {
		catalogRetryTimer.Stop()
	}

	for _, source := range w.changeSources() {
		source.Stop()
	}

	// Wait for the processing loop only after it has been launched. A failed
	// Start can leave the worker started without ever creating that loop.
	if wasStarted && loopStarted {
		<-done
	}
	w.workWG.Wait()
	retainedMessages := make([]tea.Msg, 0, cap(w.msgCh))
	for {
		select {
		case msg := <-w.msgCh:
			if _, isSnapshot := msg.(SnapshotReadyMsg); isSnapshot {
				w.releaseDroppedMessage(msg)
			} else {
				retainedMessages = append(retainedMessages, msg)
			}
		default:
			goto drainedMessages
		}
	}

drainedMessages:
	for _, msg := range retainedMessages {
		select {
		case w.msgCh <- msg:
		default:
		}
	}

	w.mu.Lock()
	snapshot := w.snapshot
	w.snapshot = nil
	w.mu.Unlock()
	if snapshot != nil {
		snapshot.releasePooledIssues()
	}

	w.logEvent(LogLevelInfo, "worker_stop", nil)
	w.closeTraceFile()
}

// takeSnapshotPooledIssues transfers ownership of a snapshot's pooled issue
// references while holding the worker lock. Snapshot clones must not mutate
// pooledIssues directly because the worker may be stopping or draining a
// dropped snapshot concurrently.
func (w *BackgroundWorker) takeSnapshotPooledIssues(snapshot *DataSnapshot) *pooledIssueLease {
	if w == nil || snapshot == nil {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	pooled := snapshot.pooledIssues
	snapshot.pooledIssues = nil
	return pooled
}

func (w *BackgroundWorker) snapshotHasPooledIssues(snapshot *DataSnapshot) bool {
	if w == nil || snapshot == nil {
		return false
	}
	w.mu.RLock()
	defer w.mu.RUnlock()
	return snapshot.pooledIssues.active()
}

// startProcessLoopLocked publishes the loop's cancellation handle before the
// goroutine can be observed by Stop or recovery. The caller must hold w.mu.
func (w *BackgroundWorker) startProcessLoopLocked(done chan struct{}) {
	loopCtx, loopCancel := context.WithCancel(w.ctx)
	w.loopCtx = loopCtx
	w.loopCancel = loopCancel
	w.loopStarted = true
	go func() {
		defer loopCancel()
		w.processLoop(loopCtx, done)
	}()
}

func (w *BackgroundWorker) startWatchdog() {
	if w == nil {
		return
	}

	w.mu.Lock()
	if w.watchdogStarted || w.state == WorkerStopped {
		w.mu.Unlock()
		return
	}
	w.watchdogStarted = true
	interval := w.watchdogInterval
	w.mu.Unlock()

	go w.watchdogLoop(interval)
}

func (w *BackgroundWorker) watchdogLoop(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-w.ctx.Done():
			return
		case now := <-ticker.C:
			w.checkHealth(now)
		}
	}
}

func (w *BackgroundWorker) recordHeartbeat(t time.Time) {
	if w == nil {
		return
	}
	w.mu.Lock()
	w.lastHeartbeat = t
	w.mu.Unlock()
}

func (w *BackgroundWorker) recordActivity() {
	w.recordActivityAt(time.Now())
}

func (w *BackgroundWorker) recordActivityAt(t time.Time) {
	if w == nil {
		return
	}
	w.lastActivityUnixNano.Store(t.UnixNano())
}

func (w *BackgroundWorker) checkHealth(now time.Time) {
	if w == nil {
		return
	}

	w.mu.RLock()
	if !w.started || w.state == WorkerStopped || w.recovering {
		w.mu.RUnlock()
		return
	}
	state := w.state
	lastHeartbeat := w.lastHeartbeat
	heartbeatTimeout := w.heartbeatTimeout
	processingStart := w.processingStart
	processingTimeout := w.processingTimeout
	w.mu.RUnlock()

	if state == WorkerProcessing && !processingStart.IsZero() && now.Sub(processingStart) > processingTimeout {
		w.attemptRecovery(fmt.Sprintf("processing exceeded %s", processingTimeout))
		return
	}

	if !lastHeartbeat.IsZero() && now.Sub(lastHeartbeat) > heartbeatTimeout {
		w.attemptRecovery(fmt.Sprintf("missed heartbeat for %s", heartbeatTimeout))
	}
}

func (w *BackgroundWorker) attemptRecovery(reason string) {
	if w == nil {
		return
	}

	w.mu.Lock()
	if w.state == WorkerStopped || !w.started || w.recovering {
		w.mu.Unlock()
		return
	}

	w.recovering = true
	w.recoveryCount++
	attempt := w.recoveryCount
	maxRecoveries := w.maxRecoveries

	// Invalidate any in-flight processing and reset to an idle baseline.
	w.generation++
	generation := w.generation
	w.state = WorkerIdle
	w.dirty = false
	w.processScheduled = false
	w.processingStart = time.Time{}
	w.lastHeartbeat = time.Now()

	loopCancel := w.loopCancel
	done := w.done
	w.loopCancel = nil
	w.mu.Unlock()

	defer func() {
		w.mu.Lock()
		w.recovering = false
		w.mu.Unlock()
	}()

	if maxRecoveries > 0 && attempt > maxRecoveries {
		w.send(SnapshotErrorMsg{
			Err:              fmt.Errorf("background worker unresponsive (giving up): %s", reason),
			Recoverable:      false,
			WorkerGeneration: generation,
		})
		w.Stop()
		return
	}

	w.logEvent(LogLevelWarn, "recovery_attempt", map[string]any{
		"attempt": attempt,
		"max":     maxRecoveries,
		"reason":  reason,
	})

	if loopCancel != nil {
		loopCancel()
	}
	if done != nil {
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			w.logEvent(LogLevelWarn, "recovery_loop_shutdown_timeout", nil)
		}
	}

	for _, source := range w.changeSources() {
		source.Stop()
		if err := source.Start(); err != nil {
			w.send(SnapshotErrorMsg{Err: fmt.Errorf("background worker recovery failed (change source %s start): %w", source.Path(), err), Recoverable: false})
			w.Stop()
			return
		}
	}
	w.mu.Lock()
	if w.state == WorkerStopped || w.ctx.Err() != nil {
		w.mu.Unlock()
		return
	}
	nextDone := make(chan struct{})
	w.done = nextDone
	w.lastHeartbeat = time.Now()
	w.startProcessLoopLocked(nextDone)
	w.mu.Unlock()

	w.ForceRefresh()
}

// TriggerRefresh manually triggers a refresh of the data.
// Has no effect if the worker is stopped. Concurrent refresh requests are
// coalesced while processing is active or already scheduled.
func (w *BackgroundWorker) TriggerRefresh() {
	w.mu.Lock()
	if w.state == WorkerStopped {
		w.mu.Unlock()
		return
	}
	if w.state == WorkerProcessing || w.processScheduled {
		w.dirty = true
		coalesced := w.coalesceCount.Add(1)
		w.mu.Unlock()
		w.logEvent(LogLevelDebug, "coalesce", map[string]any{
			"count": coalesced,
		})
		return
	}
	w.processScheduled = true
	w.workWG.Add(1)
	w.mu.Unlock()

	// Trigger processing
	go w.runProcess()
}

// TriggerSourceRefresh schedules a non-forced refresh of the Hub compatibility
// export. Content-hash dedup still suppresses unchanged snapshots.
func (w *BackgroundWorker) TriggerSourceRefresh() {
	w.markCatalogDirty()
	w.mu.Lock()
	if w.state == WorkerStopped {
		w.mu.Unlock()
		return
	}
	if w.sourceRetryTimer != nil {
		w.sourceRetryTimer.Stop()
		w.sourceRetryTimer = nil
	}
	w.refreshBDExportNext = true
	if w.state == WorkerProcessing || w.processScheduled {
		w.dirty = true
		w.coalesceCount.Add(1)
		w.mu.Unlock()
		return
	}
	w.processScheduled = true
	w.workWG.Add(1)
	w.mu.Unlock()
	go w.runProcess()
}

// ForceRefresh triggers immediate processing, bypassing debounce and content-hash
// dedup so the UI can deterministically refresh even when the data is "fresh".
func (w *BackgroundWorker) ForceRefresh() {
	w.forceRefresh(false)
}

// ForceSourceRefresh bypasses dedup and refreshes external compatibility data.
// It is reserved for explicit user refreshes; internal rebuilds use ForceRefresh.
func (w *BackgroundWorker) ForceSourceRefresh() {
	w.markCatalogDirty()
	w.forceRefresh(true)
}

func (w *BackgroundWorker) markCatalogDirty() {
	w.mu.Lock()
	w.catalogGeneration++
	w.mu.Unlock()
}

// SetCatalogPath configures repository catalog loading before the worker starts.
// watch controls live config replacement observation; manual refresh still loads
// the catalog when watching is disabled.
func (w *BackgroundWorker) SetCatalogPath(path string, watch bool) error {
	if w == nil || strings.TrimSpace(path) == "" {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.started {
		return errors.New("cannot configure Hub catalog after worker start")
	}
	w.catalogPath = path
	if watch {
		configWatcher, err := watcher.NewWatcher(path,
			watcher.WithDebounceDuration(w.debounceDelay),
			watcher.WithContentCheck(true),
		)
		if err != nil {
			return err
		}
		w.hubConfigWatcher = configWatcher
		w.catalogSource = configWatcher
	}
	return nil
}

func (w *BackgroundWorker) forceRefresh(refreshBDExport bool) {
	w.mu.Lock()
	if w.state == WorkerStopped {
		w.mu.Unlock()
		return
	}

	w.forceNext = true
	if refreshBDExport {
		if w.sourceRetryTimer != nil {
			w.sourceRetryTimer.Stop()
			w.sourceRetryTimer = nil
		}
		w.refreshBDExportNext = true
	}

	if w.state == WorkerProcessing || w.processScheduled {
		w.dirty = true
		coalesced := w.coalesceCount.Add(1)
		w.mu.Unlock()
		w.logEvent(LogLevelDebug, "coalesce", map[string]any{
			"count": coalesced,
		})
		return
	}
	w.processScheduled = true
	w.workWG.Add(1)
	w.mu.Unlock()

	go w.runProcess()
}

func (w *BackgroundWorker) runProcess() {
	defer w.workWG.Done()
	w.process()
}

// HandleRefreshRequest applies a UI refresh request on the worker side. Recipe
// changes and forced reloads are folded into one processing run.
func (w *BackgroundWorker) HandleRefreshRequest(msg RefreshRequestMsg) {
	recipeChanged := false
	if msg.Recipe != nil {
		recipeChanged = w.setRecipe(msg.Recipe)
	}
	if msg.Force || recipeChanged {
		w.ForceRefresh()
		return
	}
	w.TriggerRefresh()
}

func recipeFingerprint(r *recipe.Recipe) string {
	if r == nil {
		return ""
	}

	b, err := json.Marshal(r)
	if err != nil {
		// Fall back to name-only to preserve determinism.
		return r.Name
	}

	sum := sha256.Sum256(b)
	return fmt.Sprintf("%x", sum[:])
}

// SetRecipe updates the worker's current recipe and triggers a refresh (bv-2h40).
// This allows Phase 3 view builders to incorporate recipe/filter state off-thread.
func (w *BackgroundWorker) SetRecipe(r *recipe.Recipe) {
	if w.setRecipe(r) {
		w.ForceRefresh()
	}
}

func (w *BackgroundWorker) setRecipe(r *recipe.Recipe) bool {
	w.mu.Lock()
	if w.state == WorkerStopped {
		w.mu.Unlock()
		return false
	}

	nextID := ""
	nextHash := ""
	if r != nil {
		nextID = r.Name
		nextHash = recipeFingerprint(r)
	}

	changed := w.currentRecipeID != nextID || w.currentRecipeHash != nextHash
	w.currentRecipe = r
	w.currentRecipeID = nextID
	w.currentRecipeHash = nextHash
	w.mu.Unlock()
	return changed
}

// GetSnapshot returns the current snapshot (may be nil).
func (w *BackgroundWorker) GetSnapshot() *DataSnapshot {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.snapshot
}

// State returns the current worker state.
func (w *BackgroundWorker) State() WorkerState {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.state
}

// Generation returns the current worker lifecycle generation. Recovery bumps
// this value so callers can reject messages emitted by invalidated work.
func (w *BackgroundWorker) Generation() uint64 {
	if w == nil {
		return 0
	}
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.generation
}

// ProcessingDuration returns how long the worker has been in the processing state.
// Returns 0 if not currently processing.
func (w *BackgroundWorker) ProcessingDuration() time.Duration {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if w.state != WorkerProcessing || w.processingStart.IsZero() {
		return 0
	}
	return time.Since(w.processingStart)
}

// processLoop watches for file changes and triggers processing.
func (w *BackgroundWorker) processLoop(loopCtx context.Context, done chan struct{}) {
	defer close(done)
	defer func() {
		if r := recover(); r != nil {
			w.logEvent(LogLevelError, "process_loop_panic", map[string]any{
				"panic": fmt.Sprintf("%v", r),
				"stack": string(debug.Stack()),
			})
		}
	}()

	w.mu.RLock()
	heartbeatInterval := w.heartbeatInterval
	issueSource := w.issueSource
	sourceSource := w.sourceSource
	catalogSource := w.catalogSource
	metadataSources := append([]ChangeSource(nil), w.metadataSources...)
	w.mu.RUnlock()

	if issueSource == nil && sourceSource == nil && catalogSource == nil && len(metadataSources) == 0 {
		return
	}
	var fileChanges, sourceChanges, catalogChanges <-chan struct{}
	if issueSource != nil {
		fileChanges = issueSource.Changed()
	}
	if sourceSource != nil {
		sourceChanges = sourceSource.Changed()
	}
	if catalogSource != nil {
		catalogChanges = catalogSource.Changed()
	}
	metadataChanges := make(chan struct{}, 1)
	for _, source := range metadataSources {
		go func(source ChangeSource) {
			for {
				select {
				case <-source.Changed():
					select {
					case metadataChanges <- struct{}{}:
					case <-loopCtx.Done():
						return
					}
				case <-loopCtx.Done():
					return
				}
			}
		}(source)
	}

	heartbeatTicker := time.NewTicker(heartbeatInterval)
	defer heartbeatTicker.Stop()

	for {
		select {
		case <-loopCtx.Done():
			return

		case <-heartbeatTicker.C:
			w.recordHeartbeat(time.Now())

		case <-fileChanges:
			w.noteFileChange(time.Now())
			w.markCatalogDirty()
			w.TriggerRefresh()

		// BEGIN UPSTREAM INTEGRATION BOUNDARY: distinct change-source semantics
		case <-sourceChanges:
			w.noteFileChange(time.Now())
			w.TriggerSourceRefresh()

		case <-catalogChanges:
			w.noteFileChange(time.Now())
			w.markCatalogDirty()
			w.TriggerRefresh()
		// END UPSTREAM INTEGRATION BOUNDARY

		case <-metadataChanges:
			w.noteFileChange(time.Now())
			w.send(MetadataChangedMsg{})
		}
	}
}

// process builds a new snapshot from the current file.
func (w *BackgroundWorker) process() {
	w.processWithSnapshotBuilder(nil)
}

// processWithSnapshotBuilder keeps the generation-fenced completion path
// independently testable without relying on filesystem timing.
func (w *BackgroundWorker) processWithSnapshotBuilder(build func(bool) snapshotBuildResult) {
	w.mu.Lock()
	w.processScheduled = false
	if w.state != WorkerIdle {
		// Already stopped or processing
		if w.state == WorkerProcessing {
			// Mark dirty so current processor will re-run when done
			w.dirty = true
		}
		w.mu.Unlock()
		return
	}
	w.state = WorkerProcessing
	w.dirty = false
	forceNext := w.forceNext
	w.forceNext = false
	refreshBDExport := w.refreshBDExportNext
	w.refreshBDExportNext = false
	now := time.Now()
	w.processingStart = now
	w.lastHeartbeat = now
	gen := w.generation
	catalogGeneration := w.catalogGeneration
	w.mu.Unlock()
	w.logEvent(LogLevelDebug, "state_change", map[string]any{
		"state": "processing",
	})
	w.send(WorkerProcessingMsg{Worker: w})

	processStart := time.Now()
	queueDepth := w.pendingChanges.Swap(0)
	w.metrics.lastQueueDepth.Store(queueDepth)
	w.coalesceCount.Store(0)
	w.logEvent(LogLevelInfo, "process_start", map[string]any{
		"queue_depth": queueDepth,
	})

	// Load and build snapshot. Error publication is deferred until after the
	// generation fence so a timed-out build cannot publish stale results.
	result := snapshotBuildResult{}
	if build != nil {
		result = build(forceNext)
	} else {
		result = w.buildSnapshotResult(forceNext, refreshBDExport)
	}
	snapshot := result.snapshot
	catalog, contextlessBeadCount, contextlessCountReady, catalogErr := w.buildRepositoryCatalog(snapshot)
	if refreshBDExport {
		if result.err != nil {
			w.scheduleSourceRetry()
		} else {
			w.cancelSourceRetry()
		}
	}

	w.mu.Lock()
	// If we recovered while processing, ignore this stale result.
	if w.generation != gen {
		w.mu.Unlock()
		if snapshot != nil {
			snapshot.releasePooledIssues()
		}
		return
	}
	catalogStale := w.catalogGeneration != catalogGeneration
	// Check if stopped while we were processing - don't overwrite stopped state
	if w.state == WorkerStopped {
		w.mu.Unlock()
		if snapshot != nil {
			snapshot.releasePooledIssues()
		}
		return
	}
	if result.err != nil || result.clearError {
		w.recordErrorLocked(result.err)
	}
	w.processingStart = time.Time{}
	// Only update snapshot if we got a new one (nil means deduped or error)
	var swapLatency time.Duration
	var version uint64
	var previousSnapshot *DataSnapshot
	if snapshot != nil {
		swapStart := time.Now()
		w.lastHash = snapshot.DataHash
		previousSnapshot = w.snapshot
		w.snapshot = snapshot
		swapLatency = time.Since(swapStart)
		version = w.metrics.snapshotVersion.Add(1)
		if snapshot.IncrementalListUsed {
			w.metrics.incrementalListCount.Add(1)
		} else {
			w.metrics.fullListCount.Add(1)
		}
	}
	sourceRefreshUnchanged := refreshBDExport && snapshot == nil && w.lastError == nil
	catalogChanged := false
	catalogRecovered := false
	if !catalogStale && w.catalogPath != "" {
		if catalogErr != nil {
			w.catalogFailed = true
		} else {
			catalogRecovered = w.catalogFailed
			w.catalogFailed = false
			if !slices.Equal(w.catalog, catalog) {
				w.catalog = catalog
				catalogChanged = true
			}
		}
	}
	// Drop file events caused by this export, but preserve queued forced rebuilds.
	if refreshBDExport && !w.forceNext && !w.refreshBDExportNext &&
		w.metrics.lastFileChangeUnixNano.Load() <= w.sourceRefreshChangeCutoff.Load() {
		w.dirty = false
	}
	wasDirty := w.dirty
	coalesced := w.coalesceCount.Load()
	w.state = WorkerIdle
	w.lastHeartbeat = time.Now()
	w.mu.Unlock()
	w.logEvent(LogLevelDebug, "state_change", map[string]any{
		"state": "idle",
	})
	if previousSnapshot != nil && previousSnapshot != snapshot {
		previousSnapshot.releasePooledIssues()
	}

	processingDuration := time.Since(processStart)
	w.metrics.processingCount.Add(1)
	w.metrics.lastProcessingNs.Store(processingDuration.Nanoseconds())
	if swapLatency > 0 {
		w.metrics.lastSwapLatencyNs.Store(swapLatency.Nanoseconds())
	}
	w.metrics.lastCoalesceCount.Store(coalesced)

	w.recordActivity()
	deliveredCatalogErr := catalogErr
	if catalogStale {
		deliveredCatalogErr = nil
	}

	if result.err != nil {
		fields := map[string]any{
			"phase": result.err.Phase,
			"error": result.err.Error(),
		}
		if result.err.Phase == "load" {
			fields["path"] = w.beadsPath
		}
		w.logEvent(LogLevelError, "snapshot_build_failed", fields)
		w.send(SnapshotErrorMsg{
			Err:              result.err,
			Recoverable:      true,
			WorkerGeneration: gen,
		})
	}

	// Notify UI only if we have a new snapshot
	if snapshot != nil {
		readyAt := time.Now()
		w.metrics.lastSnapshotReadyUnix.Store(readyAt.UnixNano())
		var fileChangeAt time.Time
		if unix := w.metrics.lastFileChangeUnixNano.Load(); unix > 0 {
			fileChangeAt = time.Unix(0, unix)
		}
		w.logEvent(LogLevelInfo, "snapshot_ready", map[string]any{
			"issues":      len(snapshot.Issues),
			"hash":        hashPrefix(snapshot.DataHash),
			"version":     version,
			"swap_us":     float64(swapLatency.Microseconds()),
			"process_ms":  float64(processingDuration.Microseconds()) / 1000.0,
			"coalesced":   coalesced,
			"queue_depth": queueDepth,
		})
		w.send(SnapshotReadyMsg{
			Snapshot:              snapshot,
			FileChangeAt:          fileChangeAt,
			SentAt:                readyAt,
			SnapshotVer:           version,
			WorkerGeneration:      gen,
			QueueDepth:            queueDepth,
			CoalesceCount:         coalesced,
			Catalog:               catalog,
			ContextlessBeadCount:  contextlessBeadCount,
			ContextlessCountReady: contextlessCountReady,
			CatalogGeneration:     catalogGeneration,
			CatalogAvailable:      !catalogStale && catalogErr == nil && w.catalogPath != "",
			CatalogChanged:        !catalogStale && catalogChanged,
			CatalogRecovered:      !catalogStale && catalogRecovered,
			CatalogError:          deliveredCatalogErr,
		})
	} else if sourceRefreshUnchanged {
		w.send(HubSourceRefreshCompleteMsg{})
	}
	if !catalogStale {
		if catalogErr != nil {
			w.scheduleCatalogRetry()
		} else {
			w.cancelCatalogRetry()
		}
		if snapshot == nil {
			if catalogErr != nil {
				w.send(RepositoryCatalogErrorMsg{
					Err:                   catalogErr,
					ContextlessBeadCount:  contextlessBeadCount,
					ContextlessCountReady: contextlessCountReady,
					Generation:            catalogGeneration,
				})
			} else if catalogChanged || catalogRecovered {
				w.send(RepositoryCatalogReadyMsg{
					Catalog:               catalog,
					ContextlessBeadCount:  contextlessBeadCount,
					ContextlessCountReady: w.catalogPath != "",
					Generation:            catalogGeneration,
					Recovered:             catalogRecovered,
				})
			}
		}
	}

	// If dirty, process again immediately
	if wasDirty {
		w.TriggerRefresh()
	}
}

// safeCompute executes fn and recovers from any panics.
// Returns a WorkerError if fn panics, nil otherwise.
func (w *BackgroundWorker) safeCompute(phase string, fn func() error) *WorkerError {
	var result *WorkerError
	func() {
		defer func() {
			if r := recover(); r != nil {
				result = &WorkerError{
					Phase: phase,
					Cause: fmt.Errorf("panic: %v\n%s", r, debug.Stack()),
					Time:  time.Now(),
				}
			}
		}()
		if err := fn(); err != nil {
			result = &WorkerError{
				Phase: phase,
				Cause: err,
				Time:  time.Now(),
			}
		}
	}()
	return result
}

// recordError tracks an error and updates error state.
func (w *BackgroundWorker) recordError(err *WorkerError) {
	w.mu.Lock()
	w.recordErrorLocked(err)
	w.mu.Unlock()
}

// recordErrorLocked updates error state while the caller holds w.mu. process
// uses this form so generation validation and error-state publication are one
// atomic decision.
func (w *BackgroundWorker) recordErrorLocked(err *WorkerError) {
	w.lastError = err
	if err != nil {
		w.errorCount++
		err.Retries = w.errorCount
	} else {
		w.errorCount = 0
	}
}

func (w *BackgroundWorker) cancelSourceRetry() {
	w.mu.Lock()
	if w.sourceRetryTimer != nil {
		w.sourceRetryTimer.Stop()
		w.sourceRetryTimer = nil
	}
	w.mu.Unlock()
}

func (w *BackgroundWorker) scheduleSourceRetry() {
	w.mu.Lock()
	if w.state == WorkerStopped {
		w.mu.Unlock()
		return
	}
	attempt := w.errorCount
	if attempt < 1 {
		attempt = 1
	}
	delay := w.sourceRetryBase
	for i := 1; i < attempt && delay < w.sourceRetryMax; i++ {
		if delay > w.sourceRetryMax/2 {
			delay = w.sourceRetryMax
			break
		}
		delay *= 2
	}
	if delay > w.sourceRetryMax {
		delay = w.sourceRetryMax
	}
	if w.sourceRetryTimer != nil {
		w.sourceRetryTimer.Stop()
	}
	var timer *time.Timer
	timer = time.AfterFunc(delay, func() {
		w.mu.Lock()
		if w.sourceRetryTimer != timer || w.state == WorkerStopped {
			w.mu.Unlock()
			return
		}
		w.sourceRetryTimer = nil
		w.mu.Unlock()
		w.TriggerSourceRefresh()
	})
	w.sourceRetryTimer = timer
	w.mu.Unlock()
}

func (w *BackgroundWorker) buildRepositoryCatalog(snapshot *DataSnapshot) (repositorypkg.Catalog, int, bool, error) {
	w.mu.RLock()
	path := w.catalogPath
	current := w.snapshot
	w.mu.RUnlock()
	if path == "" {
		return nil, 0, false, nil
	}
	var issues []model.Issue
	if snapshot != nil {
		issues = snapshot.Issues
	} else if current != nil {
		issues = current.Issues
	}
	if (snapshot != nil && snapshot.LoadedOpenOnly) || (snapshot == nil && current != nil && current.LoadedOpenOnly) {
		loaded, err := loadIssuesForReload(w.beadsPath, loader.ParseOptions{BufferSize: envMaxLineSizeBytes()})
		if err != nil {
			return nil, 0, false, &WorkerError{Phase: "catalog", Cause: fmt.Errorf("loading complete issue set: %w", err), Time: time.Now()}
		}
		issues = loaded.Issues
		defer loader.ReturnIssuePtrsToPool(loaded.PoolRefs)
	}
	contextlessBeadCount := contextlessIssueCount(issues)
	catalog, err := w.catalogLoader(path, issues)
	if err != nil {
		return nil, contextlessBeadCount, true, &WorkerError{Phase: "catalog", Cause: err, Time: time.Now()}
	}
	return catalog, contextlessBeadCount, true, nil
}

func (w *BackgroundWorker) cancelCatalogRetry() {
	w.mu.Lock()
	if w.catalogRetryTimer != nil {
		w.catalogRetryTimer.Stop()
		w.catalogRetryTimer = nil
	}
	w.mu.Unlock()
}

func (w *BackgroundWorker) scheduleCatalogRetry() {
	w.mu.Lock()
	if w.state == WorkerStopped {
		w.mu.Unlock()
		return
	}
	if w.catalogRetryTimer != nil {
		w.catalogRetryTimer.Stop()
	}
	delay := w.sourceRetryBase
	var timer *time.Timer
	timer = time.AfterFunc(delay, func() {
		w.mu.Lock()
		if w.catalogRetryTimer != timer || w.state == WorkerStopped {
			w.mu.Unlock()
			return
		}
		w.catalogRetryTimer = nil
		w.mu.Unlock()
		w.markCatalogDirty()
		w.TriggerRefresh()
	})
	w.catalogRetryTimer = timer
	w.mu.Unlock()
}

// LastError returns the most recent error (nil if last operation succeeded).
func (w *BackgroundWorker) LastError() *WorkerError {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.lastError
}

func (w *BackgroundWorker) WatcherInfo() (polling bool, fsType watcher.FilesystemType, pollInterval time.Duration) {
	if w == nil || w.watcher == nil {
		return false, watcher.FSTypeUnknown, 0
	}
	return w.watcher.IsPolling(), w.watcher.FilesystemType(), w.watcher.PollInterval()
}

func (w *BackgroundWorker) Health() WorkerHealth {
	if w == nil {
		return WorkerHealth{}
	}

	w.mu.RLock()
	started := w.started
	state := w.state
	lastHeartbeat := w.lastHeartbeat
	recoveryCount := w.recoveryCount
	startTime := w.startTime
	timeout := w.heartbeatTimeout
	w.mu.RUnlock()

	alive := started && state != WorkerStopped && !lastHeartbeat.IsZero() && time.Since(lastHeartbeat) <= timeout

	idleGCCount := w.idleGCCount.Load()
	idleGCTotalNanos := w.idleGCTotalNanos.Load()
	idleGCLastDurationNanos := w.idleGCLastDurationNanos.Load()
	idleGCLastAtUnixNano := w.idleGCLastAtUnixNano.Load()

	idleGCLastAt := time.Time{}
	if idleGCLastAtUnixNano != 0 {
		idleGCLastAt = time.Unix(0, idleGCLastAtUnixNano)
	}

	return WorkerHealth{
		Started:       started,
		Alive:         alive,
		LastHeartbeat: lastHeartbeat,
		RecoveryCount: recoveryCount,
		UptimeSince:   startTime,

		IdleGCEnabled:      w.idleGCEnabled,
		IdleGCCount:        idleGCCount,
		IdleGCTotal:        time.Duration(idleGCTotalNanos),
		IdleGCLastDuration: time.Duration(idleGCLastDurationNanos),
		IdleGCLastAt:       idleGCLastAt,
	}
}

func (w *BackgroundWorker) idleGCLoop(checkEvery time.Duration) {
	ticker := time.NewTicker(checkEvery)
	defer ticker.Stop()

	for {
		select {
		case <-w.ctx.Done():
			return
		case now := <-ticker.C:
			w.maybeIdleGC(now)
		}
	}
}

func (w *BackgroundWorker) maybeIdleGC(now time.Time) {
	if w == nil {
		return
	}

	// Hold the worker lock while triggering GC to ensure it never overlaps with processing.
	w.mu.Lock()
	enabled := w.idleGCEnabled
	state := w.state
	processScheduled := w.processScheduled
	threshold := w.idleGCThreshold
	minInterval := w.idleGCMinInterval
	w.mu.Unlock()

	if !enabled || state != WorkerIdle || processScheduled {
		return
	}

	w.mu.Lock()
	// Re-check under lock for correctness and to prevent racing with process() state transitions.
	if !w.idleGCEnabled || w.state != WorkerIdle || w.processScheduled {
		w.mu.Unlock()
		return
	}

	lastActivityUnixNano := w.lastActivityUnixNano.Load()
	if lastActivityUnixNano == 0 {
		w.recordActivityAt(now)
		w.mu.Unlock()
		return
	}
	lastActivity := time.Unix(0, lastActivityUnixNano)
	if now.Sub(lastActivity) < threshold {
		w.mu.Unlock()
		return
	}

	lastIdleGCUnixNano := w.lastIdleGCUnixNano.Load()
	if lastIdleGCUnixNano != 0 && now.Sub(time.Unix(0, lastIdleGCUnixNano)) < minInterval {
		w.mu.Unlock()
		return
	}

	gcFunc := w.idleGCFunc
	if gcFunc == nil {
		gcFunc = runtime.GC
	}
	start := time.Now()
	gcFunc()
	duration := time.Since(start)

	ranAt := time.Now()
	w.lastIdleGCUnixNano.Store(ranAt.UnixNano())
	w.idleGCCount.Add(1)
	w.idleGCTotalNanos.Add(duration.Nanoseconds())
	w.idleGCLastDurationNanos.Store(duration.Nanoseconds())
	w.idleGCLastAtUnixNano.Store(ranAt.UnixNano())
	w.mu.Unlock()
}

type snapshotBuildResult struct {
	snapshot   *DataSnapshot
	err        *WorkerError
	clearError bool
}

// buildSnapshot loads data and constructs a new DataSnapshot. The optional
// refresh flag is retained for callers that need compatibility-export reloads.
func (w *BackgroundWorker) buildSnapshot(forceNext bool, refresh ...bool) *DataSnapshot {
	return w.buildSnapshotResult(forceNext, refresh...).snapshot
}

// buildSnapshotResult is called from the worker goroutine (NOT the UI thread).
// It is intentionally side-effect free with respect to worker error state and
// UI messages: process owns those effects after its generation fence.
func (w *BackgroundWorker) buildSnapshotResult(forceNext bool, refresh ...bool) snapshotBuildResult {
	refreshBDExport := len(refresh) > 0 && refresh[0]
	if w.beadsPath == "" {
		return snapshotBuildResult{}
	}
	refreshBDExport = refreshBDExport && shouldRefreshBDExport(w.beadsPath)
	watcherPaused := refreshBDExport && w.watcher != nil && w.watcher.IsStarted()
	if watcherPaused {
		w.watcher.Stop()
		select {
		case <-w.watcher.Changed():
		default:
		}
	}
	reloadPath, err := preparePathForReloadContext(w.ctx, w.beadsPath, refreshBDExport)
	if refreshBDExport {
		w.sourceRefreshChangeCutoff.Store(w.metrics.lastFileChangeUnixNano.Load())
	}
	if watcherPaused && w.ctx.Err() == nil {
		if restartErr := w.watcher.Start(); restartErr != nil {
			if err != nil {
				err = fmt.Errorf("%v; restarting file watcher: %w", err, restartErr)
			} else {
				err = fmt.Errorf("restarting file watcher: %w", restartErr)
			}
		}
	}
	if err != nil {
		loadErr := &WorkerError{Phase: "load", Cause: err}
		return snapshotBuildResult{err: loadErr}
	}

	start := time.Now()
	profileSnapshot := dbg.Enabled()
	var snapshotTimings map[string]time.Duration
	recordTiming := func(name string, d time.Duration) {
		if !profileSnapshot {
			return
		}
		if snapshotTimings == nil {
			snapshotTimings = make(map[string]time.Duration, 8)
		}
		snapshotTimings[name] = d
		dbg.LogTiming("worker."+name, d)
	}
	if profileSnapshot {
		dbg.Log("worker.snapshot_start path=%s", w.beadsPath)
	}
	metricsEnabled := w.metricsEnabled
	var memBefore runtime.MemStats
	if metricsEnabled {
		runtime.ReadMemStats(&memBefore)
	}

	// Capture recipe state for this snapshot before loading (bv-2h40).
	w.mu.RLock()
	currentRecipe := w.currentRecipe
	recipeID := w.currentRecipeID
	recipeHash := w.currentRecipeHash
	w.mu.RUnlock()

	// Determine dataset tier using a fast source-record count (bv-9thm).
	sourceLineCount := 0
	tier := datasetTierUnknown
	var countStart time.Time
	if profileSnapshot {
		countStart = time.Now()
	}
	countErr := w.safeCompute("count_lines", func() error {
		n, err := countIssuesForReload(reloadPath)
		if err != nil {
			return err
		}
		sourceLineCount = n
		tier = datasetTierForIssueCount(n)
		return nil
	})
	if profileSnapshot {
		recordTiming("line_count", time.Since(countStart))
	}
	if countErr != nil {
		w.logEvent(LogLevelDebug, "snapshot_line_count_failed", map[string]any{
			"path":  w.beadsPath,
			"error": countErr.Error(),
		})
	}

	// Huge tier: default to open-only unless the recipe explicitly includes closed/tombstone.
	loadOpenOnly := w.hubScopeMemberIDs == nil && tier == datasetTierHuge && !recipeIncludesClosedStatuses(currentRecipe)

	// Load issues from file with panic recovery
	var issues []model.Issue
	var pooledRefs []*model.Issue
	loadWarningCount := 0
	var loadStart time.Time
	if profileSnapshot {
		loadStart = time.Now()
	}
	loadErr := w.safeCompute("load", func() error {
		var err error
		var loaded loader.PooledIssues
		opts := loader.ParseOptions{
			WarningHandler: func(string) {
				loadWarningCount++
			},
			BufferSize: envMaxLineSizeBytes(),
		}
		if loadOpenOnly {
			opts.IssueFilter = func(i *model.Issue) bool {
				return i.Status != model.StatusClosed && i.Status != model.StatusTombstone
			}
		}
		loaded, err = loadIssuesForReload(reloadPath, opts)
		if err == nil {
			issues = loaded.Issues
			pooledRefs = loaded.PoolRefs
		}
		return err
	})
	if profileSnapshot {
		recordTiming("load_issues", time.Since(loadStart))
	}

	if loadErr != nil {
		return snapshotBuildResult{err: loadErr}
	}
	if w.hubScopeMemberIDs != nil {
		var memberIDs []string
		scopeErr := w.safeCompute("load", func() error {
			var err error
			memberIDs, err = w.hubScopeMemberIDs(w.ctx)
			if err != nil {
				return fmt.Errorf("loading active Hub scope: %w", err)
			}
			issues = filterIssuesByIDs(issues, memberIDs)
			return nil
		})
		if scopeErr != nil {
			loader.ReturnIssuePtrsToPool(pooledRefs)
			return snapshotBuildResult{err: scopeErr}
		}
		// The snapshot tier and reload metadata describe the bounded dataset,
		// not the unbounded source read used to obtain it.
		sourceLineCount = len(issues)
		tier = datasetTierForIssueCount(sourceLineCount)
	}

	loadDuration := time.Since(start)

	// Compute content hash for dedup
	hash := analysis.ComputeDataHash(issues)

	// Check if content is unchanged (dedup optimization)
	w.mu.RLock()
	lastHash := w.lastHash
	prevSnapshot := w.snapshot
	w.mu.RUnlock()

	loadMetadataUnchanged := prevSnapshot != nil &&
		prevSnapshot.SourceIssueCountHint == sourceLineCount &&
		prevSnapshot.LoadWarningCount == loadWarningCount &&
		prevSnapshot.DatasetTier == tier &&
		prevSnapshot.LoadedOpenOnly == loadOpenOnly &&
		prevSnapshot.RecipeName == recipeID &&
		prevSnapshot.RecipeHash == recipeHash
	if !forceNext && hash == lastHash && lastHash != "" && loadMetadataUnchanged {
		w.logEvent(LogLevelDebug, "snapshot_deduped", map[string]any{
			"hash": hashPrefix(hash),
		})
		loader.ReturnIssuePtrsToPool(pooledRefs)
		return snapshotBuildResult{clearError: true}
	}

	var diff *analysis.IssueDiff
	if prevSnapshot != nil {
		diffValue := analysis.ComputeIssueDiff(prevSnapshot.Issues, issues)
		diff = &diffValue
		w.logEvent(LogLevelDebug, "snapshot_diff", map[string]any{
			"added":              len(diffValue.Added),
			"removed":            len(diffValue.Removed),
			"modified":           len(diffValue.Modified),
			"content_changed":    len(diffValue.ContentChanged),
			"dependency_changed": len(diffValue.DependencyChanged),
			"unchanged":          len(diffValue.Unchanged),
			"total_prev":         len(prevSnapshot.Issues),
			"total_new":          len(issues),
		})
	}

	// Build snapshot (includes Phase 1 analysis) with panic recovery
	var snapshot *DataSnapshot
	analyzeStart := time.Now()
	analyzeErr := w.safeCompute("analyze_phase1", func() error {
		builder := NewSnapshotBuilder(issues).
			WithRecipe(currentRecipe).
			WithWeights(feedbackWeightsForBeadsPath(w.beadsPath)).
			WithBuildConfig(snapshotBuildConfigForTier(tier))
		if prevSnapshot != nil {
			builder.WithPreviousSnapshot(prevSnapshot, diff)
		}
		snapshot = builder.Build()
		return nil
	})

	analyzeDuration := time.Since(analyzeStart)
	if profileSnapshot {
		recordTiming("phase1", analyzeDuration)
	}
	if metricsEnabled {
		w.metrics.lastPhase1Ns.Store(analyzeDuration.Nanoseconds())
	}

	if analyzeErr != nil {
		loader.ReturnIssuePtrsToPool(pooledRefs)
		return snapshotBuildResult{err: analyzeErr}
	}

	// Store hash in snapshot for external access
	if snapshot != nil {
		snapshot.DataHash = hash
		snapshot.LoadWarningCount = loadWarningCount
		snapshot.RecipeName = recipeID
		snapshot.RecipeHash = recipeHash
		snapshot.attachPooledIssues(pooledRefs)
		snapshot.DatasetTier = tier
		snapshot.SourceIssueCountHint = sourceLineCount
		snapshot.LoadedOpenOnly = loadOpenOnly
		if loadOpenOnly && sourceLineCount > len(snapshot.Issues) {
			snapshot.TruncatedCount = sourceLineCount - len(snapshot.Issues)
		}
		snapshot.LargeDatasetWarning = largeDatasetWarning(tier, sourceLineCount, len(snapshot.Issues), loadOpenOnly)
	} else {
		loader.ReturnIssuePtrsToPool(pooledRefs)
	}

	if metricsEnabled {
		if snapshot != nil {
			w.metrics.lastSnapshotSizeBytes.Store(estimateSnapshotBytes(snapshot.Issues))
		}
		poolHits, poolMisses := loader.IssuePoolStats()
		w.metrics.poolHits.Store(poolHits)
		w.metrics.poolMisses.Store(poolMisses)

		var memAfter runtime.MemStats
		runtime.ReadMemStats(&memAfter)
		gcPauseDelta := int64(memAfter.PauseTotalNs - memBefore.PauseTotalNs)
		w.metrics.lastGCPauseDeltaNs.Store(gcPauseDelta)
	}

	totalDuration := time.Since(start)
	if profileSnapshot {
		recordTiming("snapshot_total", totalDuration)
	}
	fields := map[string]any{
		"issues":    len(issues),
		"load_ms":   float64(loadDuration.Microseconds()) / 1000.0,
		"phase1_ms": float64(analyzeDuration.Microseconds()) / 1000.0,
		"total_ms":  float64(totalDuration.Microseconds()) / 1000.0,
		"hash":      hashPrefix(hash),
	}
	if metricsEnabled {
		fields["snapshot_bytes"] = w.metrics.lastSnapshotSizeBytes.Load()
		fields["pool_hits"] = w.metrics.poolHits.Load()
		fields["pool_misses"] = w.metrics.poolMisses.Load()
		fields["gc_pause_ms"] = float64(w.metrics.lastGCPauseDeltaNs.Load()) / 1e6
	}
	w.logEvent(LogLevelInfo, "snapshot_built", fields)
	if profileSnapshot && snapshotTimings != nil {
		dbg.Log("worker.snapshot_summary issues=%d total=%s load=%s phase1=%s",
			len(issues),
			formatReloadDuration(snapshotTimings["snapshot_total"]),
			formatReloadDuration(snapshotTimings["load_issues"]),
			formatReloadDuration(snapshotTimings["phase1"]),
		)
	}

	return snapshotBuildResult{snapshot: snapshot, clearError: true}
}

func filterIssuesByIDs(issues []model.Issue, memberIDs []string) []model.Issue {
	allowed := make(map[string]struct{}, len(memberIDs))
	for _, id := range memberIDs {
		allowed[id] = struct{}{}
	}
	filtered := make([]model.Issue, 0, len(allowed))
	for _, issue := range issues {
		if _, ok := allowed[issue.ID]; ok {
			filtered = append(filtered, issue)
		}
	}
	return filtered
}

func cfgString(w *watcher.Watcher) string {
	if w == nil {
		return ""
	}
	return w.Path()
}

func recipeIncludesClosedStatuses(r *recipe.Recipe) bool {
	if r == nil {
		return false
	}
	for _, s := range r.Filters.Status {
		switch strings.TrimSpace(strings.ToLower(s)) {
		case string(model.StatusClosed), string(model.StatusTombstone):
			return true
		}
	}
	return false
}

func largeDatasetWarning(tier datasetTier, sourceHint, loaded int, openOnly bool) string {
	switch tier {
	case datasetTierLarge:
		n := loaded
		if sourceHint > 0 {
			n = sourceHint
		}
		return fmt.Sprintf("⚠ large %s issues", compactCount(n))
	case datasetTierHuge:
		if openOnly && sourceHint > 0 {
			return fmt.Sprintf("⚠ huge open-only %s/%s", compactCount(loaded), compactCount(sourceHint))
		}
		n := loaded
		if sourceHint > 0 {
			n = sourceHint
		}
		return fmt.Sprintf("⚠ huge %s issues", compactCount(n))
	default:
		return ""
	}
}

func countJSONLLines(path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	const bufSize = 32 * 1024
	buf := make([]byte, bufSize)
	lines := 0
	sawAny := false
	lastByte := byte(0)

	for {
		n, readErr := f.Read(buf)
		if n > 0 {
			sawAny = true
			lastByte = buf[n-1]
			for _, b := range buf[:n] {
				if b == '\n' {
					lines++
				}
			}
		}
		if readErr == nil {
			continue
		}
		if readErr == io.EOF {
			break
		}
		return 0, readErr
	}

	if sawAny && lastByte != '\n' {
		lines++
	}
	return lines, nil
}

func envMaxLineSizeBytes() int {
	// One definition for every loading path (TUI, background worker, robot).
	return loader.MaxLineSizeFromEnv()
}

func envBool(name string) bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv(name)))
	if v == "" {
		return false
	}
	switch v {
	case "1", "true", "yes", "y", "on":
		return true
	default:
		return false
	}
}

func envPositiveInt(name string) (int, bool) {
	v := strings.TrimSpace(os.Getenv(name))
	if v == "" {
		return 0, false
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}

func envPositiveIntOr(name string, fallback int) int {
	value, ok := envPositiveInt(name)
	if !ok {
		return fallback
	}
	return value
}

func envDurationSeconds(name string, fallback time.Duration) time.Duration {
	n, ok := envPositiveInt(name)
	if !ok {
		return fallback
	}
	return time.Duration(n) * time.Second
}

func envDurationMilliseconds(name string, fallback time.Duration) time.Duration {
	n, ok := envPositiveInt(name)
	if !ok {
		return fallback
	}
	return time.Duration(n) * time.Millisecond
}

// runPhase2Analysis waits for Phase 2 analysis to complete and notifies the UI.
// This runs in a goroutine so it doesn't block snapshot delivery.
func (w *BackgroundWorker) runPhase2Analysis(snapshot *DataSnapshot, snapshotVer, workerGeneration uint64) {
	if snapshot == nil || snapshot.Analysis == nil {
		return
	}
	stats := snapshot.Analysis
	dataHash := snapshot.DataHash

	// Wait for Phase 2 to complete (blocking)
	phase2Start := time.Now()
	stats.WaitForPhase2()
	phase2Duration := time.Since(phase2Start)
	if w.metricsEnabled {
		w.metrics.lastPhase2Ns.Store(phase2Duration.Nanoseconds())
	}
	if dbg.Enabled() {
		dbg.LogTiming("worker.phase2", phase2Duration)
	}

	// Check if this Phase 2 completion still corresponds to the active snapshot.
	w.mu.RLock()
	stopped := w.state == WorkerStopped
	current := w.snapshot
	currentGeneration := w.generation
	w.mu.RUnlock()

	if stopped || current != snapshot || currentGeneration != workerGeneration {
		w.logEvent(LogLevelDebug, "phase2_skip", map[string]any{
			"hash": hashPrefix(dataHash),
		})
		return
	}
	w.logEvent(LogLevelInfo, "phase2_complete", map[string]any{
		"hash":      hashPrefix(dataHash),
		"phase2_ms": float64(phase2Duration.Microseconds()) / 1000.0,
	})

	// Notify UI that Phase 2 metrics are ready
	w.send(Phase2UpdateMsg{
		DataHash:         dataHash,
		Stats:            stats,
		Snapshot:         snapshot,
		SnapshotVer:      snapshotVer,
		WorkerGeneration: workerGeneration,
	})
}

// SnapshotReadyMsg is sent to the UI when a new snapshot is ready.
type SnapshotReadyMsg struct {
	Snapshot              *DataSnapshot
	FileChangeAt          time.Time
	SentAt                time.Time
	SnapshotVer           uint64
	WorkerGeneration      uint64
	QueueDepth            int64
	CoalesceCount         int64
	Catalog               repositorypkg.Catalog
	ContextlessBeadCount  int
	ContextlessCountReady bool
	CatalogGeneration     uint64
	CatalogAvailable      bool
	CatalogChanged        bool
	CatalogRecovered      bool
	CatalogError          error
}

// WorkerProcessingMsg notifies the UI that active refresh feedback should start.
type WorkerProcessingMsg struct {
	Worker *BackgroundWorker
}

// SnapshotErrorMsg is sent to the UI when snapshot building fails.
type SnapshotErrorMsg struct {
	Err              error
	Recoverable      bool // True if we expect to recover on next file change
	StartFailure     bool
	WorkerGeneration uint64
}

// HubSourceRefreshCompleteMsg reports a successful Hub source refresh whose
// issue content was unchanged, so consumers can refresh external-only data.
type HubSourceRefreshCompleteMsg struct{}

// MetadataChangedMsg invalidates external-only metadata without rebuilding the
// issue snapshot. It is deliberately separate from source refresh completion.
type MetadataChangedMsg struct{}

// RepositoryCatalogReadyMsg carries independently refreshed Hub metadata.
type RepositoryCatalogReadyMsg struct {
	Catalog               repositorypkg.Catalog
	ContextlessBeadCount  int
	ContextlessCountReady bool
	Generation            uint64
	Recovered             bool
}

// RepositoryCatalogErrorMsg reports a transient catalog load failure. The UI
// retains its last valid catalog while the worker retries.
type RepositoryCatalogErrorMsg struct {
	Err                   error
	ContextlessBeadCount  int
	ContextlessCountReady bool
	Generation            uint64
}

// Phase2UpdateMsg is sent when Phase 2 analysis completes.
// This allows the UI to update without waiting for full rebuild.
// The UI should check DataHash matches current snapshot before using.
type Phase2UpdateMsg struct {
	DataHash         string // Content hash to verify this matches current snapshot
	Stats            *analysis.GraphStats
	Snapshot         *DataSnapshot
	SnapshotVer      uint64
	WorkerGeneration uint64
}

// RefreshRequestMsg asks the BackgroundWorker to reload data. Force bypasses
// content-hash deduplication; Recipe applies new background view configuration.
// A nil Recipe keeps the worker's current recipe.
type RefreshRequestMsg struct {
	Force  bool
	Recipe *recipe.Recipe
}

func (w *BackgroundWorker) send(msg tea.Msg) {
	if w == nil || msg == nil {
		return
	}
	w.sendMu.Lock()
	defer w.sendMu.Unlock()

	// Reject invalidated work before it consumes queue capacity. The UI repeats
	// this fence because recovery can advance the generation immediately after
	// this check.
	if !w.workerMessageIsCurrent(msg) {
		w.releaseDroppedMessage(msg)
		return
	}

	select {
	case <-w.ctx.Done():
		w.releaseDroppedMessage(msg)
		return
	default:
	}

	queued := drainWorkerMailbox(w.msgCh)
	switch typed := msg.(type) {
	case SnapshotReadyMsg:
		queued = replaceWorkerSnapshot(queued, typed, w)
	default:
		queued = append(queued, msg)
	}

	// Catalog messages are delivery state, not independent work. Once a
	// snapshot is retained, fold the newest catalog state into it so the bounded
	// mailbox can retain the snapshot and the latest source error together.
	queued = mergeWorkerCatalogState(queued)
	for len(queued) > cap(w.msgCh) {
		idx := workerMessageDropIndex(queued)
		w.releaseDroppedMessage(queued[idx])
		queued = append(queued[:idx], queued[idx+1:]...)
	}

	for _, queuedMsg := range queued {
		select {
		case w.msgCh <- queuedMsg:
		case <-w.ctx.Done():
			w.releaseDroppedMessage(queuedMsg)
		}
	}
}

func drainWorkerMailbox(ch chan tea.Msg) []tea.Msg {
	messages := make([]tea.Msg, 0, len(ch))
	for {
		select {
		case msg := <-ch:
			messages = append(messages, msg)
		default:
			return messages
		}
	}
}

func replaceWorkerSnapshot(messages []tea.Msg, incoming SnapshotReadyMsg, w *BackgroundWorker) []tea.Msg {
	latest := -1
	for i, msg := range messages {
		if _, ok := msg.(SnapshotReadyMsg); ok {
			latest = i
		}
	}
	if latest < 0 {
		return append(messages, incoming)
	}

	queued := messages[latest].(SnapshotReadyMsg)
	if queued.SnapshotVer > 0 && incoming.SnapshotVer > 0 && queued.SnapshotVer > incoming.SnapshotVer {
		w.releaseDroppedMessage(incoming)
		return messages
	}
	// Preserve delivery state from the snapshot being replaced. Issue snapshot
	// versions and catalog generations advance independently, so the newer issue
	// snapshot may carry an older catalog payload.
	if queued.CatalogGeneration > incoming.CatalogGeneration {
		incoming.Catalog = queued.Catalog
		incoming.ContextlessBeadCount = queued.ContextlessBeadCount
		incoming.ContextlessCountReady = queued.ContextlessCountReady
		incoming.CatalogGeneration = queued.CatalogGeneration
		incoming.CatalogAvailable = queued.CatalogAvailable
		incoming.CatalogChanged = queued.CatalogChanged
		incoming.CatalogRecovered = queued.CatalogRecovered
		incoming.CatalogError = queued.CatalogError
	} else if queued.CatalogGeneration == incoming.CatalogGeneration {
		if queued.CatalogAvailable && !incoming.CatalogAvailable {
			incoming.Catalog = queued.Catalog
			incoming.ContextlessBeadCount = queued.ContextlessBeadCount
			incoming.ContextlessCountReady = queued.ContextlessCountReady
			incoming.CatalogAvailable = true
			incoming.CatalogError = nil
		}
		incoming.CatalogChanged = incoming.CatalogChanged || queued.CatalogChanged
		incoming.CatalogRecovered = incoming.CatalogRecovered || queued.CatalogRecovered
	}
	w.releaseDroppedMessage(queued)
	messages[latest] = incoming
	return messages
}

func mergeWorkerCatalogState(messages []tea.Msg) []tea.Msg {
	latestSnapshot := -1
	for i, msg := range messages {
		if _, ok := msg.(SnapshotReadyMsg); ok {
			latestSnapshot = i
		}
	}
	if latestSnapshot < 0 {
		return messages
	}

	snapshot := messages[latestSnapshot].(SnapshotReadyMsg)
	retained := make([]tea.Msg, 0, len(messages))
	retainedSnapshot := -1
	for i, msg := range messages {
		switch catalog := msg.(type) {
		case RepositoryCatalogReadyMsg:
			snapshot = mergeCatalogReady(snapshot, catalog)
		case RepositoryCatalogErrorMsg:
			snapshot = mergeCatalogError(snapshot, catalog)
		default:
			if i == latestSnapshot {
				retainedSnapshot = len(retained)
				retained = append(retained, nil)
			} else {
				retained = append(retained, msg)
			}
		}
	}
	retained[retainedSnapshot] = snapshot
	return retained
}

func mergeCatalogReady(snapshot SnapshotReadyMsg, catalog RepositoryCatalogReadyMsg) SnapshotReadyMsg {
	if catalog.Generation < snapshot.CatalogGeneration {
		return snapshot
	}
	newer := catalog.Generation > snapshot.CatalogGeneration
	if newer || !snapshot.CatalogAvailable {
		snapshot.Catalog = catalog.Catalog
		snapshot.CatalogGeneration = catalog.Generation
	}
	if newer || !snapshot.CatalogAvailable {
		snapshot.ContextlessBeadCount = catalog.ContextlessBeadCount
		snapshot.ContextlessCountReady = catalog.ContextlessCountReady
	}
	snapshot.CatalogAvailable = true
	snapshot.CatalogChanged = true
	snapshot.CatalogRecovered = snapshot.CatalogRecovered || catalog.Recovered
	snapshot.CatalogError = nil
	return snapshot
}

func mergeCatalogError(snapshot SnapshotReadyMsg, catalog RepositoryCatalogErrorMsg) SnapshotReadyMsg {
	if catalog.Generation < snapshot.CatalogGeneration ||
		(catalog.Generation == snapshot.CatalogGeneration && snapshot.CatalogAvailable) {
		return snapshot
	}
	snapshot.CatalogGeneration = catalog.Generation
	snapshot.CatalogAvailable = false
	snapshot.CatalogError = catalog.Err
	if catalog.ContextlessCountReady {
		snapshot.ContextlessBeadCount = catalog.ContextlessBeadCount
		snapshot.ContextlessCountReady = true
	}
	return snapshot
}

func workerMessageDropIndex(messages []tea.Msg) int {
	idx := 0
	priority := workerMessagePriority(messages[0])
	for i := 1; i < len(messages); i++ {
		candidate := workerMessagePriority(messages[i])
		if candidate < priority {
			idx = i
			priority = candidate
		}
	}
	return idx
}

func workerMessagePriority(msg tea.Msg) int {
	switch typed := msg.(type) {
	case SnapshotReadyMsg:
		return 2
	case SnapshotErrorMsg:
		// A recoverable error may be superseded by the last usable snapshot. A
		// terminal error must win, because it is the UI's signal to detach the
		// stopped worker and fall back to synchronous refreshes.
		if !typed.Recoverable {
			return 3
		}
		return 1
	case Phase2UpdateMsg:
		return 0
	default:
		// Ordinary notifications are disposable when the mailbox also holds the
		// current snapshot and a recoverable source error.
		return 0
	}
}

func workerMessageGeneration(msg tea.Msg) (uint64, bool) {
	switch typed := msg.(type) {
	case SnapshotReadyMsg:
		return typed.WorkerGeneration, true
	case SnapshotErrorMsg:
		return typed.WorkerGeneration, true
	case Phase2UpdateMsg:
		return typed.WorkerGeneration, true
	default:
		return 0, false
	}
}

func (w *BackgroundWorker) workerMessageIsCurrent(msg tea.Msg) bool {
	generation, tagged := workerMessageGeneration(msg)
	// Generation zero denotes a manually constructed or non-worker message and
	// preserves direct Model.Update/test compatibility.
	return !tagged || generation == 0 || generation == w.Generation()
}

func (w *BackgroundWorker) releaseDroppedMessage(msg tea.Msg) {
	ready, ok := msg.(SnapshotReadyMsg)
	if !ok || ready.Snapshot == nil {
		return
	}
	w.mu.RLock()
	current := w.snapshot
	w.mu.RUnlock()
	// The worker retains its current snapshot until replacement/Stop. Older
	// dropped messages have no remaining owner unless the UI already installed
	// them, whose release path shares the same one-shot lease.
	if ready.Snapshot != current {
		ready.Snapshot.releasePooledIssues()
	}
}

// WatcherChanged returns the watcher's change notification channel.
// This is useful for integration with existing code.
func (w *BackgroundWorker) WatcherChanged() <-chan struct{} {
	if w.watcher == nil {
		return nil
	}
	return w.watcher.Changed()
}

// LastHash returns the content hash from the last successful snapshot build.
// Useful for testing and debugging.
func (w *BackgroundWorker) LastHash() string {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.lastHash
}

func estimateSnapshotBytes(issues []model.Issue) int64 {
	const (
		baseIssueBytes      = 256
		baseDependencyBytes = 64
		baseCommentBytes    = 128
		baseLabelBytes      = 16
	)

	var total int64
	for i := range issues {
		issue := &issues[i]
		total += baseIssueBytes
		total += int64(len(issue.ID) + len(issue.Title) + len(issue.Description) + len(issue.Design) +
			len(issue.AcceptanceCriteria) + len(issue.Notes) + len(issue.Assignee) + len(issue.SourceRepo))
		if issue.ExternalRef != nil {
			total += int64(len(*issue.ExternalRef))
		}

		for _, label := range issue.Labels {
			total += baseLabelBytes + int64(len(label))
		}
		for _, dep := range issue.Dependencies {
			if dep == nil {
				continue
			}
			total += baseDependencyBytes + int64(len(dep.IssueID)+len(dep.DependsOnID)+len(dep.CreatedBy))
		}
		for _, c := range issue.Comments {
			if c == nil {
				continue
			}
			total += baseCommentBytes + int64(len(c.Text)+len(c.Author))
		}
	}

	return total
}

// hashPrefix returns a safe prefix of the hash for logging.
// Returns up to 16 characters, or the full hash if shorter.
func hashPrefix(hash string) string {
	if len(hash) > 16 {
		return hash[:16]
	}
	return hash
}

// ResetHash clears the stored content hash, forcing the next buildSnapshot
// to process even if content is unchanged. Useful for testing.
func (w *BackgroundWorker) ResetHash() {
	w.mu.Lock()
	w.lastHash = ""
	w.mu.Unlock()
}
