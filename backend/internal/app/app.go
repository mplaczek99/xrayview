package app

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"xrayview/backend/internal/cache"
	"xrayview/backend/internal/config"
	"xrayview/backend/internal/contracts"
	"xrayview/backend/internal/httpapi"
	"xrayview/backend/internal/jobs"
	"xrayview/backend/internal/logging"
	"xrayview/backend/internal/persistence"
	"xrayview/backend/internal/studies"
)

type App struct {
	config      config.Config
	logger      *slog.Logger
	cache       *cache.Store
	persistence *persistence.Catalog
	jobs        *jobs.Service
	studies     *studies.Registry
	server      *http.Server
	startedAt   time.Time
}

// New builds a server-ready App. The HTTP server is wired but not listening;
// the caller drives lifecycle by calling Run (which also runs prepare).
func New(cfg config.Config, logger *slog.Logger) (*App, error) {
	return newApp(cfg, logger, nil, nil, nil, true)
}

// NewService builds an embedded App with no HTTP server attached. Used by
// backend/service.go when the desktop shell hosts the backend in-process.
// Because there is no Run loop to lazily call prepare, cache and persistence
// directories are created here so the returned App is immediately usable.
func NewService(cfg config.Config, logger *slog.Logger) (*App, error) {
	application, err := newApp(cfg, logger, nil, nil, nil, false)
	if err != nil {
		return nil, err
	}

	if err := application.prepare(); err != nil {
		return nil, err
	}

	return application, nil
}

// NewWithServices is the composition seam used by tests and by callers that
// want to share a cache / registry / job service across App instances.
// Anything passed as nil falls back to a default constructed from cfg.
func NewWithServices(
	cfg config.Config,
	logger *slog.Logger,
	cacheStore *cache.Store,
	studyRegistry *studies.Registry,
	jobService *jobs.Service,
) (*App, error) {
	return newApp(cfg, logger, cacheStore, studyRegistry, jobService, true)
}

func newApp(
	cfg config.Config,
	logger *slog.Logger,
	cacheStore *cache.Store,
	studyRegistry *studies.Registry,
	jobService *jobs.Service,
	createServer bool,
) (*App, error) {
	if logger == nil {
		logger = logging.New(cfg.ServiceName, cfg.Logging.Level)
	}
	if cacheStore == nil {
		cacheStore = cache.NewWithPaths(cfg.Paths.CacheDir, cfg.Paths.PersistenceDir)
	}
	catalogPath, err := cacheStore.PersistencePath("catalog.json")
	if err != nil {
		return nil, err
	}
	persistenceCatalog := persistence.NewAtPath(catalogPath)
	if studyRegistry == nil {
		studyRegistry = studies.New()
	}
	if jobService == nil {
		jobService = jobs.New(cacheStore, studyRegistry, logger)
	}
	startedAt := time.Now().UTC()
	application := &App{
		config:      cfg,
		logger:      logger,
		cache:       cacheStore,
		persistence: persistenceCatalog,
		jobs:        jobService,
		studies:     studyRegistry,
		startedAt:   startedAt,
	}
	if createServer {
		router := httpapi.NewRouter(httpapi.RouterDeps{
			Service:     application,
			Config:      cfg,
			Logger:      logger,
			Cache:       cacheStore,
			Persistence: persistenceCatalog,
			StartedAt:   startedAt,
		})
		application.server = &http.Server{
			Addr:              cfg.ListenAddress(),
			Handler:           router,
			ReadHeaderTimeout: 5 * time.Second,
			ReadTimeout:       15 * time.Second,
			WriteTimeout:      15 * time.Second,
			IdleTimeout:       60 * time.Second,
			MaxHeaderBytes:    1 << 20,
		}
	}

	return application, nil
}

// NewFromEnvironment is the production sidecar entry point used by
// cmd/xrayviewd. Config and per-subsystem knobs (worker counts, cache sizes,
// etc.) are read from XRAYVIEW_BACKEND_* env vars — so unlike New, callers
// don't build a config.Config themselves.
func NewFromEnvironment() (*App, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}

	logger := logging.New(cfg.ServiceName, cfg.Logging.Level)
	cacheStore := cache.NewWithPaths(cfg.Paths.CacheDir, cfg.Paths.PersistenceDir)
	studyRegistry := studies.New()
	jobService, err := jobs.NewFromEnvironment(cacheStore, studyRegistry, logger)
	if err != nil {
		return nil, err
	}

	return newApp(cfg, logger, cacheStore, studyRegistry, jobService, true)
}

func (app *App) Handler() http.Handler {
	if app.server == nil {
		return nil
	}

	return app.server.Handler
}

func (app *App) Config() config.Config {
	return app.config
}

func (app *App) Run(ctx context.Context) error {
	if app.server == nil {
		return errors.New("backend HTTP server is not configured")
	}

	if err := app.prepare(); err != nil {
		return err
	}

	errCh := make(chan error, 1)
	go func() {
		err := app.server.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}

		errCh <- nil
	}()

	select {
	case <-ctx.Done():
		app.logger.Info("shutting down backend")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), app.config.Server.ShutdownTimeout)
		defer cancel()

		if err := app.server.Shutdown(shutdownCtx); err != nil {
			return err
		}

		return <-errCh
	case err := <-errCh:
		return err
	}
}

// prepare ensures the cache and persistence directories exist and emits the
// "ready" log line. The log message intentionally differs between server
// mode ("backend ready") and embedded mode ("embedded backend ready") so
// the runtime flavor is obvious when triaging logs.
func (app *App) prepare() error {
	if err := app.cache.Ensure(); err != nil {
		return err
	}

	if err := app.persistence.Ensure(); err != nil {
		return err
	}

	if app.server != nil {
		app.logger.Info(
			"backend ready",
			slog.String("listen_address", app.config.ListenAddress()),
			slog.String("cache_dir", app.cache.RootDir()),
			slog.String("persistence_dir", app.persistence.RootDir()),
			slog.Int("backend_contract_version", contracts.BackendContractVersion),
		)
	} else {
		app.logger.Info(
			"embedded backend ready",
			slog.String("cache_dir", app.cache.RootDir()),
			slog.String("persistence_dir", app.persistence.RootDir()),
			slog.Int("backend_contract_version", contracts.BackendContractVersion),
		)
	}

	return nil
}
