#!/usr/bin/env python3
"""
Ejemplo de integración con Slack para enviar threads al Knowledge Agent

Este script muestra cómo:
1. Recibir un evento de Slack (mention, reaction, etc.)
2. Obtener el thread completo
3. Enviarlo al Knowledge Agent via webhook
"""

import os
import requests
from slack_sdk import WebClient
from slack_sdk.errors import SlackApiError


class SlackToKnowledgeAgent:
    def __init__(self, slack_token: str, knowledge_agent_url: str):
        """
        Inicializar el integrador

        Args:
            slack_token: Token del bot de Slack (xoxb-...)
            knowledge_agent_url: URL del Knowledge Agent (ej: http://localhost:8081)
        """
        self.slack_client = WebClient(token=slack_token)
        self.knowledge_agent_url = knowledge_agent_url.rstrip('/')

    def fetch_thread(self, channel_id: str, thread_ts: str) -> dict:
        """
        Obtener todos los mensajes de un thread

        Args:
            channel_id: ID del canal de Slack
            thread_ts: Timestamp del mensaje principal del thread

        Returns:
            Dict con la información del thread
        """
        try:
            # Obtener todos los replies del thread
            result = self.slack_client.conversations_replies(
                channel=channel_id,
                ts=thread_ts,
                inclusive=True  # Incluir el mensaje principal
            )

            messages = []
            for msg in result["messages"]:
                messages.append({
                    "user": msg.get("user", "unknown"),
                    "text": msg.get("text", ""),
                    "ts": msg.get("ts", ""),
                    "type": msg.get("type", "message")
                })

            return {
                "thread_ts": thread_ts,
                "channel_id": channel_id,
                "messages": messages
            }

        except SlackApiError as e:
            print(f"Error fetching thread: {e.response['error']}")
            raise

    def send_to_knowledge_agent(self, thread_data: dict) -> dict:
        """
        Enviar thread al Knowledge Agent usando intent: ingest

        Args:
            thread_data: Dict con thread_ts, channel_id, y messages

        Returns:
            Respuesta del Knowledge Agent
        """
        endpoint = f"{self.knowledge_agent_url}/api/query"

        # Use the unified /api/query endpoint with intent: "ingest"
        payload = {
            "question": "Ingest this thread",
            "intent": "ingest",
            **thread_data
        }

        try:
            response = requests.post(endpoint, json=payload, timeout=30)
            response.raise_for_status()
            return response.json()

        except requests.exceptions.RequestException as e:
            print(f"Error sending to Knowledge Agent: {e}")
            raise

    def ingest_thread(self, channel_id: str, thread_ts: str) -> dict:
        """
        Pipeline completo: fetch thread de Slack y enviarlo al Knowledge Agent

        Args:
            channel_id: ID del canal de Slack
            thread_ts: Timestamp del thread

        Returns:
            Respuesta del Knowledge Agent
        """
        print(f"Fetching thread {thread_ts} from channel {channel_id}...")
        thread_data = self.fetch_thread(channel_id, thread_ts)

        print(f"Sending {len(thread_data['messages'])} messages to Knowledge Agent...")
        result = self.send_to_knowledge_agent(thread_data)

        print(f"✅ Success! {result.get('memories_added', 0)} memories created")
        return result

    def post_confirmation(self, channel_id: str, thread_ts: str, memories_count: int):
        """
        Postear confirmación en el thread de Slack

        Args:
            channel_id: ID del canal
            thread_ts: Timestamp del thread
            memories_count: Número de memories creadas
        """
        try:
            self.slack_client.chat_postMessage(
                channel=channel_id,
                thread_ts=thread_ts,
                text=f"✅ Thread ingested to knowledge base! Created {memories_count} memories."
            )
        except SlackApiError as e:
            print(f"Error posting confirmation: {e.response['error']}")


# =============================================================================
# Ejemplo de uso con Flask (para recibir webhooks de Slack)
# =============================================================================

from flask import Flask, request, jsonify

app = Flask(__name__)

# Configuración
SLACK_BOT_TOKEN = os.getenv("SLACK_BOT_TOKEN")
KNOWLEDGE_AGENT_URL = os.getenv("KNOWLEDGE_AGENT_URL", "http://localhost:8081")
SLACK_SIGNING_SECRET = os.getenv("SLACK_SIGNING_SECRET")

integrator = SlackToKnowledgeAgent(SLACK_BOT_TOKEN, KNOWLEDGE_AGENT_URL)


@app.route("/slack/events", methods=["POST"])
def slack_events():
    """
    Endpoint para recibir eventos de Slack

    Configurar en Slack:
    Event Subscriptions → Request URL: https://tu-dominio.com/slack/events
    Subscribe to bot events: app_mention
    """
    data = request.json

    # Verificación de URL de Slack
    if "challenge" in data:
        return jsonify({"challenge": data["challenge"]})

    # Procesar eventos
    if "event" in data:
        event = data["event"]

        # Cuando mencionan el bot
        if event.get("type") == "app_mention":
            channel_id = event.get("channel")
            thread_ts = event.get("thread_ts") or event.get("ts")
            text = event.get("text", "").lower()

            # Si el mensaje contiene "ingest" o "save"
            if "ingest" in text or "save" in text:
                try:
                    # Ingestar el thread
                    result = integrator.ingest_thread(channel_id, thread_ts)

                    # Confirmar en Slack
                    integrator.post_confirmation(
                        channel_id,
                        thread_ts,
                        result.get("memories_added", 0)
                    )

                except Exception as e:
                    print(f"Error processing thread: {e}")
                    # Opcionalmente postear error en Slack

    return jsonify({"status": "ok"})


@app.route("/slack/shortcuts", methods=["POST"])
def slack_shortcuts():
    """
    Endpoint para Slack shortcuts/interactive components

    Útil para crear un shortcut "Save to Knowledge Base"
    """
    payload = request.form.get("payload")
    if not payload:
        return jsonify({"error": "No payload"}), 400

    import json
    data = json.loads(payload)

    if data.get("type") == "shortcut":
        # Obtener información del mensaje
        message = data.get("message", {})
        channel_id = data["channel"]["id"]
        thread_ts = message.get("thread_ts") or message.get("ts")

        try:
            # Ingestar
            result = integrator.ingest_thread(channel_id, thread_ts)

            # Responder al shortcut
            return jsonify({
                "text": f"✅ Thread saved! Created {result.get('memories_added', 0)} memories."
            })

        except Exception as e:
            return jsonify({
                "text": f"❌ Error: {str(e)}"
            }), 500

    return jsonify({"status": "ok"})


# =============================================================================
# Ejemplo de uso directo (sin webhooks)
# =============================================================================

if __name__ == "__main__":
    # Uso del integrator directamente
    if len(os.sys.argv) > 2:
        channel = os.sys.argv[1]
        thread = os.sys.argv[2]

        integrator = SlackToKnowledgeAgent(
            slack_token=os.getenv("SLACK_BOT_TOKEN"),
            knowledge_agent_url=os.getenv("KNOWLEDGE_AGENT_URL", "http://localhost:8081")
        )

        try:
            result = integrator.ingest_thread(channel, thread)
            print(f"\n✅ Success!")
            print(f"Memories created: {result.get('memories_added', 0)}")
            print(f"Message: {result.get('message', 'N/A')}")

        except Exception as e:
            print(f"\n❌ Error: {e}")
            exit(1)

    else:
        # Modo servidor Flask
        print("Starting Flask server for Slack webhooks...")
        print("Configure Slack Event URL: http://your-domain.com/slack/events")
        app.run(host="0.0.0.0", port=3000, debug=True)
