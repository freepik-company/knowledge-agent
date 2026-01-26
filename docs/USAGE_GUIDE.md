# Knowledge Agent - Usage Guide

## Quick Start

Simply mention the bot with your message:
```
@bot <your message>
```

The AI automatically decides what to do based on context.

## What It Does

### 1. Answer Questions
Search the knowledge base and respond:
```
@bot how do we deploy to production?
@bot what's our database backup strategy?
```

### 2. Save Information
Store valuable insights from conversations:
```
@bot remember that deployments are on Tuesdays
@bot save this discussion
```

### 3. Analyze Images
Technical diagrams, errors, architectures:
```
[Upload architecture diagram]
@bot this is our microservices architecture
```

### 4. Fetch URLs
Analyze documentation and web content:
```
@bot check this documentation https://example.com/docs
```

## Language Support

The agent responds in your language automatically (Spanish, English, etc.).

## What Gets Saved

**Saved ✅:**
- Technical decisions and solutions
- Architecture diagrams and system designs
- Error messages and troubleshooting steps
- Configurations and procedures
- Important discussions with business value

**Not Saved ❌:**
- Casual conversations
- Duplicate information
- Temporary details

## Examples

**Technical Image:**
```
User: "Esta es nuestra arquitectura de microservicios" + [diagram]
Bot: Analyzes, saves architecture details, confirms in Spanish
```

**Error Screenshot:**
```
User: "Este error bloquea producción" + [screenshot]
Bot: Extracts error, saves details, provides analysis
```

**Question:**
```
User: "¿Qué base de datos usamos?"
Bot: Searches memory, provides answer
```

That's it. Just talk naturally with the bot.
