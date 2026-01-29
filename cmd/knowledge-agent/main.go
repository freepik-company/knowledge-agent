package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"knowledge-agent/internal/agent"
	"knowledge-agent/internal/config"
	"knowledge-agent/internal/launcher"
	"knowledge-agent/internal/logger"
	"knowledge-agent/internal/server"
	"knowledge-agent/internal/slack"
)

// Mode represents the operational mode of the service
type Mode string

const (
	ModeAll      Mode = "all"       // Run both agent and slack-bot
	ModeAgent    Mode = "agent"     // Run only agent
	ModeSlackBot Mode = "slack-bot" // Run only slack-bot
)

func main() {
	// Parse command line flags
	configPath := flag.String("config", "", "Path to configuration file (default: config.yaml or environment variables)")
	modeFlag := flag.String("mode", "all", "Operating mode: all, agent, or slack-bot")
	flag.Parse()

	mode := Mode(*modeFlag)

	// Validate mode
	if mode != ModeAll && mode != ModeAgent && mode != ModeSlackBot {
		log.Fatalf("Invalid mode: %s (must be: all, agent, or slack-bot)", mode)
	}

	ctx := context.Background()

	// Load configuration
	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Initialize logger
	logConfig := logger.Config{
		Level:      cfg.Log.Level,
		Format:     cfg.Log.Format,
		OutputPath: cfg.Log.OutputPath,
	}
	if err := logger.Initialize(logConfig); err != nil {
		log.Fatalf("Failed to initialize logger: %v", err)
	}
	defer logger.Sync()

	log := logger.Get()

	log.Infow("Knowledge Agent starting",
		"mode", mode,
		"version", "unified-binary",
	)

	// Set up graceful shutdown
	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

	// Run based on mode
	switch mode {
	case ModeAgent:
		runAgentOnly(ctx, cfg, done)
	case ModeSlackBot:
		runSlackBotOnly(ctx, cfg, done)
	case ModeAll:
		runBothServices(ctx, cfg, done)
	}
}

// runAgentOnly runs only the Knowledge Agent service
func runAgentOnly(ctx context.Context, cfg *config.Config, done chan os.Signal) {
	log := logger.Get()
	log.Info("Running in Agent-only mode")

	// Initialize agent with ADK
	agentInstance, err := agent.New(ctx, cfg)
	if err != nil {
		log.Fatalw("Failed to initialize agent", "error", err)
	}
	// Agent will be closed explicitly in shutdown sequence (don't defer)

	// Create HTTP server with handlers
	agentServer := server.NewAgentServer(agentInstance, cfg)

	// Create HTTP server
	addr := fmt.Sprintf(":%d", cfg.Server.AgentPort)
	httpServer := &http.Server{
		Addr:         addr,
		Handler:      agentServer.Handler(),
		ReadTimeout:  time.Duration(cfg.Server.ReadTimeout) * time.Second,
		WriteTimeout: time.Duration(cfg.Server.WriteTimeout) * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Create cancelable context for graceful shutdown
	launcherCtx, cancelLauncher := context.WithCancel(ctx)
	defer cancelLauncher()

	go func() {
		log.Infow("Knowledge Agent service starting",
			"addr", addr,
			"port", cfg.Server.AgentPort,
		)
		log.Infow("Endpoints configured",
			"ingest_thread", fmt.Sprintf("POST http://localhost%s/api/ingest-thread", addr),
			"query", fmt.Sprintf("POST http://localhost%s/api/query", addr),
		)
		logAuthMode(cfg)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalw("Server error", "error", err)
		}
	}()

	// Start ADK Launcher in parallel (if enabled)
	if cfg.Launcher.Enabled {
		go func() {
			launcherCfg := launcher.NewConfigFromAppConfig(&cfg.Launcher, cfg.APIKeys)
			log.Infow("ADK Launcher starting",
				"port", launcherCfg.Port,
				"webui", launcherCfg.EnableWebUI,
				"a2a_endpoint", fmt.Sprintf("http://localhost:%d/a2a/invoke", launcherCfg.Port),
				"agent_card", fmt.Sprintf("http://localhost:%d/.well-known/agent-card.json", launcherCfg.Port),
			)
			if launcherCfg.EnableWebUI {
				log.Infow("WebUI available", "url", fmt.Sprintf("http://localhost:%d/ui/", launcherCfg.Port))
			}

			if err := launcher.Run(launcherCtx, launcherCfg, agentInstance.GetLLMAgent(), agentInstance.GetSessionService()); err != nil {
				if launcherCtx.Err() == nil {
					// Only log error if not cancelled
					log.Errorw("ADK Launcher error", "error", err)
				}
			}
		}()
	}

	<-done
	log.Info("Shutdown signal received, starting graceful shutdown...")

	// 1. Cancel launcher context first
	log.Info("Stopping ADK Launcher...")
	cancelLauncher()

	// 2. Shutdown HTTP server (with timeout)
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	log.Info("Shutting down HTTP server...")
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Warnw("Server shutdown error", "error", err)
	}
	log.Info("HTTP server stopped")

	// 3. Close agent server resources (rate limiter cleanup)
	log.Info("Closing agent server resources...")
	if err := agentServer.Close(); err != nil {
		log.Warnw("Error closing agent server", "error", err)
	}

	// 4. Close agent resources (with timeout for safety)
	closeAgentWithTimeout(agentInstance, 5*time.Second)

	log.Info("Knowledge Agent service stopped")
}

// runSlackBotOnly runs only the Slack Bridge service
func runSlackBotOnly(ctx context.Context, cfg *config.Config, done chan os.Signal) {
	log := logger.Get()
	log.Info("Running in Slack-Bot-only mode")

	// Agent service URL
	agentURL := fmt.Sprintf("http://localhost:%d", cfg.Server.AgentPort)

	// Determine authentication mode
	authMode := "Open (no auth)"
	if cfg.Auth.InternalToken != "" {
		authMode = "Secured (internal token)"
	}

	log.Infow("Configuration loaded",
		"agent_url", agentURL,
		"slack_mode", cfg.Slack.Mode,
		"agent_auth", authMode,
	)

	// Initialize Slack handler
	handler := slack.NewHandler(cfg, agentURL)

	// Run in different modes
	if cfg.Slack.Mode == "socket" {
		// Socket Mode - No HTTP server needed
		log.Info("Starting in Socket Mode (no public endpoint required)")

		socketHandler := slack.NewSocketHandler(handler, cfg.Slack.AppToken)

		go func() {
			if err := socketHandler.Start(ctx); err != nil {
				log.Fatalw("Socket mode error", "error", err)
			}
		}()

		<-done
		log.Info("Shutting down Socket Mode client...")

		// Close handler resources (including cache cleanup)
		if err := handler.Close(); err != nil {
			log.Warnw("Error closing Slack handler", "error", err)
		}

		log.Info("Slack socket mode stopped")

	} else {
		// Webhook Mode - HTTP server required
		log.Info("Starting in Webhook Mode (requires public endpoint)")

		// Create HTTP server with handlers
		slackServer := server.NewSlackServer(handler)

		// Create HTTP server
		addr := fmt.Sprintf(":%d", cfg.Server.SlackBotPort)
		httpServer := &http.Server{
			Addr:         addr,
			Handler:      slackServer.Handler(),
			ReadTimeout:  time.Duration(cfg.Server.ReadTimeout) * time.Second,
			WriteTimeout: time.Duration(cfg.Server.WriteTimeout) * time.Second,
			IdleTimeout:  60 * time.Second,
		}

		go func() {
			log.Infow("Slack Webhook Bridge starting",
				"addr", addr,
				"port", cfg.Server.SlackBotPort,
				"events_endpoint", fmt.Sprintf("http://localhost%s/slack/events", addr),
				"agent_url", agentURL,
			)
			if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Fatalw("Server error", "error", err)
			}
		}()

		<-done
		log.Info("Shutting down Slack bot service...")

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			log.Fatalw("Server shutdown error", "error", err)
		}

		// Close handler resources (including cache cleanup)
		if err := handler.Close(); err != nil {
			log.Warnw("Error closing Slack handler", "error", err)
		}

		log.Info("Slack webhook bridge stopped")
	}
}

// runBothServices runs both Agent and Slack Bridge in parallel
func runBothServices(ctx context.Context, cfg *config.Config, done chan os.Signal) {
	log := logger.Get()
	log.Info("Running in All mode (both Agent and Slack Bridge)")

	var wg sync.WaitGroup
	errors := make(chan error, 3) // Increased for launcher

	// Create cancelable context for graceful shutdown
	shutdownCtx, cancelShutdown := context.WithCancel(ctx)
	defer cancelShutdown()

	// Agent service URL for Slack Bridge
	agentURL := fmt.Sprintf("http://localhost:%d", cfg.Server.AgentPort)

	// Initialize agent
	agentInstance, err := agent.New(ctx, cfg)
	if err != nil {
		log.Fatalw("Failed to initialize agent", "error", err)
	}
	// Agent will be closed explicitly in shutdown sequence (don't defer)

	// Create Agent HTTP server
	agentServer := server.NewAgentServer(agentInstance, cfg)
	agentAddr := fmt.Sprintf(":%d", cfg.Server.AgentPort)
	agentHTTPServer := &http.Server{
		Addr:         agentAddr,
		Handler:      agentServer.Handler(),
		ReadTimeout:  time.Duration(cfg.Server.ReadTimeout) * time.Second,
		WriteTimeout: time.Duration(cfg.Server.WriteTimeout) * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start Agent service
	wg.Add(1)
	go func() {
		defer wg.Done()
		log.Infow("Knowledge Agent service starting",
			"addr", agentAddr,
			"port", cfg.Server.AgentPort,
		)
		log.Infow("Endpoints configured",
			"ingest_thread", fmt.Sprintf("POST http://localhost%s/api/ingest-thread", agentAddr),
			"query", fmt.Sprintf("POST http://localhost%s/api/query", agentAddr),
		)
		logAuthMode(cfg)
		if err := agentHTTPServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errors <- fmt.Errorf("agent server error: %w", err)
		}
	}()

	// Start ADK Launcher in parallel (if enabled)
	if cfg.Launcher.Enabled {
		wg.Add(1)
		go func() {
			defer wg.Done()
			launcherCfg := launcher.NewConfigFromAppConfig(&cfg.Launcher, cfg.APIKeys)
			log.Infow("ADK Launcher starting",
				"port", launcherCfg.Port,
				"webui", launcherCfg.EnableWebUI,
				"a2a_endpoint", fmt.Sprintf("http://localhost:%d/a2a/invoke", launcherCfg.Port),
				"agent_card", fmt.Sprintf("http://localhost:%d/.well-known/agent-card.json", launcherCfg.Port),
			)
			if launcherCfg.EnableWebUI {
				log.Infow("WebUI available", "url", fmt.Sprintf("http://localhost:%d/ui/", launcherCfg.Port))
			}

			if err := launcher.Run(shutdownCtx, launcherCfg, agentInstance.GetLLMAgent(), agentInstance.GetSessionService()); err != nil {
				if shutdownCtx.Err() == nil {
					// Only report error if not cancelled
					errors <- fmt.Errorf("launcher error: %w", err)
				}
			}
		}()
	}

	// Give agent a moment to start
	time.Sleep(500 * time.Millisecond)

	// Initialize Slack handler
	slackHandler := slack.NewHandler(cfg, agentURL)

	// Start Slack Bridge based on mode
	if cfg.Slack.Mode == "socket" {
		// Socket Mode
		log.Info("Starting Slack Bridge in Socket Mode (no public endpoint required)")

		socketHandler := slack.NewSocketHandler(slackHandler, cfg.Slack.AppToken)

		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := socketHandler.Start(shutdownCtx); err != nil && shutdownCtx.Err() == nil {
				// Only report error if not cancelled by shutdown
				errors <- fmt.Errorf("socket mode error: %w", err)
			}
		}()

	} else {
		// Webhook Mode
		log.Info("Starting Slack Bridge in Webhook Mode (requires public endpoint)")

		slackServer := server.NewSlackServer(slackHandler)
		slackAddr := fmt.Sprintf(":%d", cfg.Server.SlackBotPort)
		slackHTTPServer := &http.Server{
			Addr:         slackAddr,
			Handler:      slackServer.Handler(),
			ReadTimeout:  time.Duration(cfg.Server.ReadTimeout) * time.Second,
			WriteTimeout: time.Duration(cfg.Server.WriteTimeout) * time.Second,
			IdleTimeout:  60 * time.Second,
		}

		wg.Add(1)
		go func() {
			defer wg.Done()
			log.Infow("Slack Webhook Bridge starting",
				"addr", slackAddr,
				"port", cfg.Server.SlackBotPort,
				"events_endpoint", fmt.Sprintf("http://localhost%s/slack/events", slackAddr),
				"agent_url", agentURL,
			)
			if err := slackHTTPServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				errors <- fmt.Errorf("slack server error: %w", err)
			}
		}()

		// Wait for shutdown signal
		select {
		case <-done:
			log.Info("Shutting down all services...")
		case err := <-errors:
			log.Errorw("Service error", "error", err)
		}

		// Shutdown both servers
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		log.Info("Shutting down Slack bridge...")
		if err := slackHTTPServer.Shutdown(shutdownCtx); err != nil {
			log.Warnw("Slack server shutdown error", "error", err)
		}

		log.Info("Shutting down Agent service...")
		if err := agentHTTPServer.Shutdown(shutdownCtx); err != nil {
			log.Warnw("Agent server shutdown error", "error", err)
		}

		wg.Wait()

		// Close Slack handler resources (including cache cleanup)
		log.Info("Closing Slack handler resources...")
		if err := slackHandler.Close(); err != nil {
			log.Warnw("Error closing Slack handler", "error", err)
		}

		// Close agent server resources (rate limiter cleanup)
		log.Info("Closing agent server resources...")
		if err := agentServer.Close(); err != nil {
			log.Warnw("Error closing agent server", "error", err)
		}

		// Close agent resources (with timeout for safety)
		closeAgentWithTimeout(agentInstance, 5*time.Second)

		log.Info("All services stopped")
		return
	}

	// For socket mode, just wait for signal
	select {
	case <-done:
		log.Info("Shutting down all services...")
	case err := <-errors:
		log.Errorw("Service error", "error", err)
	}

	// Cancel context to stop socket handler
	log.Info("Cancelling socket handler...")
	cancelShutdown()

	// Shutdown agent HTTP server
	httpShutdownCtx, httpCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer httpCancel()

	log.Info("Shutting down Agent service...")
	if err := agentHTTPServer.Shutdown(httpShutdownCtx); err != nil {
		log.Warnw("Agent server shutdown error", "error", err)
	}

	// Wait for goroutines with timeout
	log.Info("Waiting for goroutines to finish...")
	waitDone := make(chan struct{})
	go func() {
		wg.Wait()
		close(waitDone)
	}()

	select {
	case <-waitDone:
		log.Info("All goroutines finished")
	case <-time.After(3 * time.Second):
		log.Warn("Goroutine wait timeout - forcing shutdown")
	}

	// Close Slack handler resources (including cache cleanup)
	log.Info("Closing Slack handler resources...")
	if err := slackHandler.Close(); err != nil {
		log.Warnw("Error closing Slack handler", "error", err)
	}

	// Close agent server resources (rate limiter cleanup)
	log.Info("Closing agent server resources...")
	if err := agentServer.Close(); err != nil {
		log.Warnw("Error closing agent server", "error", err)
	}

	// Close agent resources (with timeout for safety)
	closeAgentWithTimeout(agentInstance, 5*time.Second)

	log.Info("All services stopped")
}

// closeAgentWithTimeout closes an agent with a timeout to prevent hanging
func closeAgentWithTimeout(agentInstance *agent.Agent, timeout time.Duration) {
	log := logger.Get()
	log.Info("Closing agent resources...")

	closeDone := make(chan error, 1)
	go func() {
		closeDone <- agentInstance.Close()
	}()

	select {
	case err := <-closeDone:
		if err != nil {
			log.Warnw("Error closing agent resources", "error", err)
		} else {
			log.Info("Agent resources closed successfully")
		}
	case <-time.After(timeout):
		log.Warn("Agent close timeout - forcing shutdown")
	}
}

// logAuthMode logs the authentication mode configuration
func logAuthMode(cfg *config.Config) {
	log := logger.Get()
	hasInternalToken := cfg.Auth.InternalToken != ""
	hasAPIKeys := len(cfg.APIKeys) > 0

	if !hasInternalToken && !hasAPIKeys {
		log.Info("Authentication: Open mode (no authentication required)")
	} else {
		authMethods := []string{}
		if hasInternalToken {
			authMethods = append(authMethods, "internal_token")
		}
		if hasAPIKeys {
			authMethods = append(authMethods, fmt.Sprintf("a2a_api_keys(%d)", len(cfg.APIKeys)))
		}

		log.Infow("Authentication: Secured mode",
			"methods", strings.Join(authMethods, ", "),
			"slack_auth", "signing_secret",
		)
	}
}
