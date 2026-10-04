// Command server runs the agent's HTTP API for the website.
//
// With ANTHROPIC_API_KEY set it talks to Claude. Without it (or with
// AGENT_DEMO=1) it runs in demo mode against a scripted stand-in, so the
// whole stack can be tried with no accounts and no cost.
//
// With SUPABASE_URL and SUPABASE_SECRET_KEY set it persists to Supabase.
// Without them it keeps everything in memory.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/anthropics/anthropic-sdk-go/option"

	"github.com/aurimas13/Code-Editing-Agent/internal/llm"
	"github.com/aurimas13/Code-Editing-Agent/internal/llm/fakellm"
	"github.com/aurimas13/Code-Editing-Agent/internal/server"
	"github.com/aurimas13/Code-Editing-Agent/internal/store"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg := server.ConfigFromEnv()

	var client llm.Client
	demo := os.Getenv("ANTHROPIC_API_KEY") == "" || os.Getenv("AGENT_DEMO") == "1"
	if demo {
		fake := fakellm.New(fakellm.DemoBrain)
		fake.ChunkDelay = 20 * time.Millisecond
		baseURL, closeFake, err := fake.Listen()
		if err != nil {
			log.Error("start demo model", "err", err)
			os.Exit(1)
		}
		defer closeFake()
		client = llm.NewAnthropic(option.WithBaseURL(baseURL), option.WithAPIKey("demo"), option.WithMaxRetries(0))
		log.Warn("demo mode: no ANTHROPIC_API_KEY, using the scripted stand-in model")
	} else {
		client = llm.NewAnthropic(option.WithMaxRetries(2))
	}

	var st store.Store = store.NewMemory()
	if url, key := os.Getenv("SUPABASE_URL"), os.Getenv("SUPABASE_SECRET_KEY"); url != "" && key != "" {
		st = store.NewSupabase(url, key)
	} else {
		log.Warn("no SUPABASE_URL/SUPABASE_SECRET_KEY: sessions and traces are kept in memory only")
	}

	api := server.New(cfg, client, st, demo, log)
	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           api.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       120 * time.Second,
		// No WriteTimeout: replies are streamed and each turn is bounded by
		// TURN_TIMEOUT instead.
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		log.Info("listening", "addr", cfg.Addr, "model", cfg.Model, "demo", demo, "store", st.Name(),
			"origins", cfg.AllowedOrigins, "daily_budget_usd", cfg.DailyBudgetUSD)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("server", "err", err)
			stop()
		}
	}()

	<-ctx.Done()
	log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
	api.Wait() // let pending database writes finish
}
