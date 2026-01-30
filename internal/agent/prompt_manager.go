package agent

import (
	"fmt"
	"os"
	"sync"

	"knowledge-agent/internal/config"
	"knowledge-agent/internal/logger"

	"github.com/fsnotify/fsnotify"
)

// PromptManager manages system prompts with support for hot reload
type PromptManager struct {
	config        *config.PromptConfig
	currentPrompt string
	mu            sync.RWMutex
	watcher       *fsnotify.Watcher
	stopChan      chan struct{}
}

// NewPromptManager creates a new prompt manager
func NewPromptManager(cfg *config.PromptConfig) (*PromptManager, error) {
	log := logger.Get()

	m := &PromptManager{
		config:   cfg,
		stopChan: make(chan struct{}),
	}

	// Load initial prompt
	if err := m.loadPrompt(); err != nil {
		return nil, fmt.Errorf("failed to load prompt: %w", err)
	}

	// Set up file watcher if hot reload is enabled and using a template path
	if cfg.EnableHotReload && cfg.TemplatePath != "" {
		watcher, err := fsnotify.NewWatcher()
		if err != nil {
			log.Warnw("Failed to create file watcher, hot reload disabled", "error", err)
		} else {
			if err := watcher.Add(cfg.TemplatePath); err != nil {
				log.Warnw("Failed to watch template file, hot reload disabled",
					"file", cfg.TemplatePath,
					"error", err,
				)
				watcher.Close()
			} else {
				m.watcher = watcher
				go m.watchFile()
				log.Infow("Hot reload enabled for prompt template",
					"file", cfg.TemplatePath,
				)
			}
		}
	}

	return m, nil
}

// loadPrompt loads the prompt from file or config
func (m *PromptManager) loadPrompt() error {
	log := logger.Get()

	var prompt string

	// Priority 1: Load from external template file if specified
	if m.config.TemplatePath != "" {
		data, err := os.ReadFile(m.config.TemplatePath)
		if err != nil {
			return fmt.Errorf("failed to read template file: %w", err)
		}
		prompt = string(data)
		log.Infow("Loaded prompt from external file",
			"file", m.config.TemplatePath,
			"length", len(prompt),
		)
	} else if m.config.BasePrompt != "" {
		// Priority 2: Use base_prompt from config
		prompt = m.config.BasePrompt
		log.Infow("Loaded prompt from config",
			"length", len(prompt),
		)
	} else {
		// Priority 3: Use default from agent package (set via SetDefaultPrompt)
		prompt = defaultPrompt
		if prompt == "" {
			return fmt.Errorf("no prompt configured: specify base_prompt in config or template_path")
		}
		log.Infow("Using default prompt",
			"length", len(prompt),
		)
	}

	// Store the prompt
	m.mu.Lock()
	m.currentPrompt = prompt
	m.mu.Unlock()

	return nil
}

// watchFile watches for changes to the template file
func (m *PromptManager) watchFile() {
	log := logger.Get()

	for {
		select {
		case event, ok := <-m.watcher.Events:
			if !ok {
				return
			}
			if event.Op&(fsnotify.Write|fsnotify.Create) != 0 {
				log.Infow("Prompt template file changed, reloading",
					"file", event.Name,
					"op", event.Op.String(),
				)
				if err := m.loadPrompt(); err != nil {
					log.Errorw("Failed to reload prompt",
						"error", err,
					)
				} else {
					log.Info("Prompt reloaded successfully")
				}
			}

		case err, ok := <-m.watcher.Errors:
			if !ok {
				return
			}
			log.Errorw("File watcher error", "error", err)

		case <-m.stopChan:
			return
		}
	}
}

// GetPrompt returns the current prompt (thread-safe)
func (m *PromptManager) GetPrompt() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.currentPrompt
}

// Reload manually reloads the prompt
func (m *PromptManager) Reload() error {
	return m.loadPrompt()
}

// Close stops the file watcher and cleanup
func (m *PromptManager) Close() error {
	if m.watcher != nil {
		close(m.stopChan)
		return m.watcher.Close()
	}
	return nil
}

// defaultPrompt is set at package initialization
var defaultPrompt string

// SetDefaultPrompt sets the default prompt (called from agent package init)
func SetDefaultPrompt(prompt string) {
	defaultPrompt = prompt
}
