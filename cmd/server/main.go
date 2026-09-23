package main

import (
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/Tarat0r/namepoll/internal/database"
	sqlc "github.com/Tarat0r/namepoll/internal/database/sqlc"
	"github.com/Tarat0r/namepoll/internal/form"
	"github.com/Tarat0r/namepoll/internal/ratelimit"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	var (
		definition *form.Definition
		err        error
	)
	if configYAML, configured := os.LookupEnv("NAMEPOLL_CONFIG_YAML"); configured {
		definition, err = form.Parse(configYAML)
	} else {
		definition, err = form.Loader("forms/default.yaml")
	}
	if err != nil {
		return err
	}

	templates, err := template.ParseGlob("web/templates/*.html")
	if err != nil {
		return fmt.Errorf("parse templates: %w", err)
	}

	db, err := database.Open("data/namepoll.db")
	if err != nil {
		return err
	}
	defer db.Close()

	clientIPs, err := ratelimit.NewClientIPResolver(os.Getenv("TRUSTED_PROXY_CIDRS"))
	if err != nil {
		return err
	}

	h := Handler{
		definition: definition,
		template:   templates,
		db:         db,
		queries:    sqlc.New(db),
		limiter:    ratelimit.New(db, 30*time.Second),
		clientIPs:  clientIPs,
		logger:     logger,
	}

	mux := http.NewServeMux()
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.Dir("web/static"))))
	mux.Handle("GET /{$}", http.RedirectHandler("/poll", http.StatusSeeOther))
	mux.HandleFunc("GET /poll", h.poll)
	mux.HandleFunc("POST /poll/suggestions", h.suggestions)
	mux.HandleFunc("GET /submissions", h.submissions)
	mux.HandleFunc("GET /submissions/{id}", h.submission)
	mux.HandleFunc("GET /poll/thanks", h.thanks)
	mux.HandleFunc("GET /poll/statistics", h.statistics)

	server := http.Server{
		Addr:              ":8080",
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	logger.Info("server listening", "address", server.Addr)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
