package app

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"xrayview/go-backend/internal/cache"
	"xrayview/go-backend/internal/config"
	"xrayview/go-backend/internal/contracts"
	"xrayview/go-backend/internal/httpapi"
	"xrayview/go-backend/internal/jobs"
	"xrayview/go-backend/internal/logging"
	"xrayview/go-backend/internal/persistence"
	"xrayview/go-backend/internal/studies"
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

func New(cfg config.Config, logger *slog.Logger) (*App, error) {
	return newApp(cfg, logger, nil, nil, nil)
}

func newApp(
	cfg config.Config,
	logger *slog.Logger,
	cacheStore *cache.Store,
	studyRegistry *studies.Registry,
	jobService *jobs.Service,
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
	router := httpapi.NewRouter(httpapi.Dependencies{
		Config:      cfg,
		Logger:      logger,
		Cache:       cacheStore,
		Persistence: persistenceCatalog,
		Jobs:        jobService,
		Studies:     studyRegistry,
		StartedAt:   startedAt,
	})

	return &App{
		config:      cfg,
		logger:      logger,
		cache:       cacheStore,
		persistence: persistenceCatalog,
		jobs:        jobService,
		studies:     studyRegistry,
		startedAt:   startedAt,
		server: &http.Server{
			Addr:              cfg.ListenAddress(),
			Handler:           router,
			ReadHeaderTimeout: 5 * time.Second,
		},
	}, nil
}

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

	return newApp(cfg, logger, cacheStore, studyRegistry, jobService)
}

func (app *App) Handler() http.Handler {
	return app.server.Handler
}

func (app *App) Config() config.Config {
	return app.config
}

func (app *App) Run(ctx context.Context) error {
	if err := app.bootstrap(); err != nil {
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
		app.logger.Info("shutting down go backend")
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

func (app *App) bootstrap() error {
	if err := app.cache.Ensure(); err != nil {
		return err
	}

	if err := app.persistence.Ensure(); err != nil {
		return err
	}

	app.logger.Info(
		"go backend ready",
		slog.String("listen_address", app.config.ListenAddress()),
		slog.String("cache_dir", app.cache.RootDir()),
		slog.String("persistence_dir", app.persistence.RootDir()),
		slog.Int("backend_contract_version", contracts.BackendContractVersion),
	)

	return nil
}
