package slack

import (
	"context"

	"knowledge-agent/internal/logger"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"
)

// SocketHandler handles Slack events via Socket Mode
type SocketHandler struct {
	handler *Handler
	client  *socketmode.Client
}

// NewSocketHandler creates a new socket mode handler
func NewSocketHandler(handler *Handler, appToken string) *SocketHandler {
	api := slack.New(
		handler.config.Slack.BotToken,
		slack.OptionAppLevelToken(appToken),
	)

	// Create socket mode client (debug disabled to reduce log noise)
	client := socketmode.New(
		api,
		socketmode.OptionDebug(false),
	)

	return &SocketHandler{
		handler: handler,
		client:  client,
	}
}

// Start starts listening for events via Socket Mode
func (sh *SocketHandler) Start(ctx context.Context) error {
	log := logger.Get()
	log.Infow("Slack Socket Mode starting",
		"agent_url", sh.handler.agentURL,
		"mode", "socket",
	)
	log.Info("Socket Mode enabled - no public endpoint required")
	log.Info("Listening for events from Slack")

	go sh.handleEvents(ctx)

	return sh.client.RunContext(ctx)
}

func (sh *SocketHandler) handleEvents(ctx context.Context) {
	log := logger.Get()
	for envelope := range sh.client.Events {
		log.Debugw("Socket event received",
			"type", envelope.Type,
		)

		switch envelope.Type {
		case socketmode.EventTypeConnecting:
			log.Info("Connecting to Slack")

		case socketmode.EventTypeConnectionError:
			log.Errorw("Connection error", "error", envelope.Data)

		case socketmode.EventTypeConnected:
			log.Info("Connected to Slack via Socket Mode")

		case socketmode.EventTypeEventsAPI:
			eventsAPIEvent, ok := envelope.Data.(slackevents.EventsAPIEvent)
			if !ok {
				log.Warnw("Unexpected event type", "data", envelope.Data)
				continue
			}

			// Acknowledge the event
			sh.client.Ack(*envelope.Request)

			// Handle the event
			sh.handleEventsAPIEvent(eventsAPIEvent)

		case socketmode.EventTypeSlashCommand:
			// Future: handle slash commands if needed
			sh.client.Ack(*envelope.Request)

		case socketmode.EventTypeInteractive:
			// Future: handle interactive components if needed
			sh.client.Ack(*envelope.Request)

		default:
			log.Debugw("Unhandled event type", "type", envelope.Type)
		}
	}
}

func (sh *SocketHandler) handleEventsAPIEvent(event slackevents.EventsAPIEvent) {
	log := logger.Get()
	log.Debugw("Processing socket event",
		"event_type", event.Type,
	)

	switch event.Type {
	case slackevents.CallbackEvent:
		innerEvent := event.InnerEvent
		log.Debugw("Callback event received",
			"inner_type", innerEvent.Type,
		)

		switch ev := innerEvent.Data.(type) {
		case *slackevents.AppMentionEvent:
			log.Infow("App mention received",
				"user", ev.User,
				"channel", ev.Channel,
			)
			// Handle app mention using existing handler logic
			sh.handler.handleAppMention(ev)
		case *slackevents.MessageEvent:
			// Handle direct messages (DMs) - no @mention needed
			if sh.handler.shouldHandleDirectMessage(ev) {
				log.Infow("Direct message received",
					"user", ev.User,
					"channel", ev.Channel,
				)
				sh.handler.handleDirectMessage(ev)
			}
		default:
			log.Debugw("Unhandled inner event type", "type", innerEvent.Type)
		}
	default:
		log.Debugw("Unhandled event type", "type", event.Type)
	}
}
