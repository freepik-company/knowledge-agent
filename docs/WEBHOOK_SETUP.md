# Webhook Mode Setup Guide

## Socket Mode vs Webhook Mode

### Socket Mode (Current - Development)
- ✅ **No public endpoint needed** - WebSocket connection to Slack
- ✅ **Perfect for local dev** - Works behind firewalls/NAT
- ✅ **Simple setup** - Just need App Token
- ❌ **Not ideal for production** - Persistent connection, single instance
- ❌ **No load balancing** - One connection per app

### Webhook Mode (Production Recommended)
- ✅ **Stateless** - Each event is independent HTTP request
- ✅ **Scalable** - Can run multiple instances with load balancer
- ✅ **Production-ready** - Industry standard for Slack apps
- ✅ **Auto-retry** - Slack retries failed deliveries
- ❌ **Requires public HTTPS endpoint** - Must be accessible from internet
- ❌ **More setup** - Need ngrok/reverse proxy for local testing

## Security: Slack Request Signing

Both modes are secure, but webhook uses **cryptographic signature verification**:

### How It Works

```
┌─────────┐                                    ┌──────────────┐
│  Slack  │                                    │ Your Server  │
└────┬────┘                                    └──────┬───────┘
     │                                                 │
     │ 1. Event occurs (user mentions bot)            │
     │                                                 │
     │ 2. Slack computes HMAC-SHA256 signature        │
     │    using its Signing Secret                    │
     │                                                 │
     │ 3. Slack sends HTTP POST with headers:         │
     │    - X-Slack-Signature: v0=abc123...           │
     │    - X-Slack-Request-Timestamp: 1234567890     │
     │    - Body: {...event data...}                  │
     ├────────────────────────────────────────────────>│
     │                                                 │
     │                                                 │ 4. Your server computes
     │                                                 │    its own signature using
     │                                                 │    the SAME Signing Secret
     │                                                 │
     │                                                 │ 5. Compare signatures
     │                                                 │    ✅ Match = legitimate
     │                                                 │    ❌ No match = reject
     │                                                 │
     │ 6. ✅ 200 OK (if signature valid)              │
     │<────────────────────────────────────────────────┤
     │    ❌ 401 Unauthorized (if invalid)             │
     │                                                 │
```

### Security Protections

1. **Replay Attack Prevention**
   - Timestamp must be within 5 minutes
   - Old requests are rejected

2. **Request Tampering Prevention**
   - Signature includes entire body
   - Any modification invalidates signature

3. **Impersonation Prevention**
   - Only Slack knows the Signing Secret
   - Impossible to forge valid signatures

4. **Timing Attack Prevention**
   - Uses constant-time comparison
   - Prevents signature guessing

## Setup Instructions

### Step 1: Get Your Signing Secret

1. Go to https://api.slack.com/apps
2. Select your app
3. Navigate to **Settings → Basic Information**
4. Scroll to **App Credentials** section
5. Copy the **Signing Secret** (looks like: `abc123def456...`)

![Slack Signing Secret Location](https://api.slack.com/img/api/articles/verifying-requests-from-slack/signing-secret.png)

**⚠️ IMPORTANT**: Keep this secret safe! Anyone with this secret can impersonate Slack.

### Step 2: Configure Your Application

#### Option A: Environment Variable

```bash
export SLACK_SIGNING_SECRET=your_signing_secret_here
```

#### Option B: Config File (Recommended)

```yaml
# config.yaml
slack:
  bot_token: xoxb-...
  signing_secret: ${SLACK_SIGNING_SECRET}  # Reference env var
  mode: webhook  # Change from "socket" to "webhook"
```

### Step 3: Setup Public HTTPS Endpoint

#### For Local Development: Use ngrok

```bash
# Install ngrok
brew install ngrok  # macOS
# or download from https://ngrok.com/download

# Start your agent
make dev CONFIG=config.yaml

# In another terminal, create tunnel
ngrok http 8080

# Output will show:
# Forwarding: https://abc123.ngrok.io -> http://localhost:8080
```

#### For Production: Use Reverse Proxy

**Nginx Example:**
```nginx
server {
    listen 443 ssl http2;
    server_name bot.example.com;

    ssl_certificate /etc/letsencrypt/live/bot.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/bot.example.com/privkey.pem;

    location /slack/events {
        proxy_pass http://localhost:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
```

**Caddy Example (Easier):**
```caddyfile
bot.example.com {
    reverse_proxy /slack/events localhost:8080
}
```

### Step 4: Configure Slack Event Subscriptions

1. Go to https://api.slack.com/apps
2. Select your app
3. Navigate to **Features → Event Subscriptions**
4. Enable Events
5. Set **Request URL** to your public endpoint:
   - Local (ngrok): `https://abc123.ngrok.io/slack/events`
   - Production: `https://bot.example.com/slack/events`
6. Slack will send a verification request (automatically handled)
7. Subscribe to bot events:
   - `app_mention` (required)
   - `message.channels` (optional)
8. Save Changes
9. Reinstall app to workspace (if prompted)

### Step 5: Test the Webhook

```bash
# Start your agent in webhook mode
make dev CONFIG=config.yaml

# You should see:
# INFO  Slack Webhook Bridge starting  addr=:8080
# INFO  Webhook Mode - public endpoint required

# Mention your bot in Slack
# You should see in logs:
# DEBUG Webhook: AppMentionEvent detected
# INFO  Slack event received
```

## Verification Flow

When Slack sends a request, your server:

1. **Extracts Headers:**
   ```
   X-Slack-Signature: v0=a2114d57b48eac39b9ad189dd8316235a7b4a8d21a10bd27519666489c69b503
   X-Slack-Request-Timestamp: 1531420618
   ```

2. **Checks Timestamp:**
   ```go
   now := time.Now().Unix()
   if abs(now - timestamp) > 300 {  // 5 minutes
       return "timestamp too old"
   }
   ```

3. **Computes Expected Signature:**
   ```go
   baseString := "v0:1531420618:{request_body}"
   expectedSig := HMAC-SHA256(baseString, signingSecret)
   // v0=a2114d57b48eac39b9ad189dd8316235a7b4a8d21a10bd27519666489c69b503
   ```

4. **Compares Signatures (Constant Time):**
   ```go
   if hmac.Equal(receivedSig, expectedSig) {
       return "✅ Valid request from Slack"
   } else {
       return "❌ Invalid signature - reject"
   }
   ```

## Troubleshooting

### "Signature verification failed"

**Causes:**
- Wrong signing secret in config
- Request modified by proxy (check body encoding)
- Clock skew (server time incorrect)

**Solution:**
```bash
# Verify signing secret
echo $SLACK_SIGNING_SECRET

# Check server time
date

# Enable debug logs
log:
  level: debug

# Look for:
# WARN Signature verification failed
```

### "Timestamp too old"

**Causes:**
- Server clock out of sync
- Request delayed (network issues)
- Replay attack (legitimate protection)

**Solution:**
```bash
# Sync system clock
sudo ntpdate -s time.apple.com  # macOS
sudo timedatectl set-ntp true   # Linux

# Check if Slack is retrying old requests
# (happens if endpoint was down)
```

### "Challenge validation failed"

**Causes:**
- Endpoint not publicly accessible
- Firewall blocking Slack IPs
- Wrong URL in Event Subscriptions

**Solution:**
```bash
# Test endpoint is accessible
curl https://your-endpoint.com/slack/events

# Should return 405 Method Not Allowed (means it's reachable)

# Verify ngrok tunnel (for local)
ngrok http 8080
# Use the HTTPS URL, not HTTP
```

### "No signature header"

**Causes:**
- Using Socket Mode config in Webhook Mode
- Proxy stripping headers
- Testing with curl (won't have signature)

**Solution:**
```yaml
# Ensure webhook mode is set
slack:
  mode: webhook
  signing_secret: ${SLACK_SIGNING_SECRET}
  # app_token: ""  # Not needed for webhook
```

## Comparison Table

| Feature | Socket Mode | Webhook Mode |
|---------|-------------|--------------|
| **Setup Complexity** | ⭐ Simple | ⭐⭐⭐ Moderate |
| **Security** | ⭐⭐⭐⭐ App Token | ⭐⭐⭐⭐⭐ Cryptographic Signature |
| **Public Endpoint** | ❌ Not needed | ✅ Required (HTTPS) |
| **Local Development** | ✅ Perfect | ⚠️ Needs ngrok |
| **Production** | ⚠️ Single instance | ✅ Scalable |
| **Load Balancing** | ❌ Not supported | ✅ Supported |
| **Auto-Retry** | ✅ Built-in | ✅ Built-in |
| **Latency** | ⭐⭐⭐⭐⭐ Real-time | ⭐⭐⭐⭐ HTTP request |
| **Debugging** | ⭐⭐⭐⭐ Direct logs | ⭐⭐⭐ Check Slack logs |

## Recommendations

### For Development
**Use Socket Mode**:
```yaml
slack:
  mode: socket
  app_token: ${SLACK_APP_TOKEN}
  bot_token: ${SLACK_BOT_TOKEN}
```

### For Production
**Use Webhook Mode**:
```yaml
slack:
  mode: webhook
  signing_secret: ${SLACK_SIGNING_SECRET}
  bot_token: ${SLACK_BOT_TOKEN}
```

### For Staging
**Use Webhook Mode with ngrok**:
```bash
# Terminal 1: Agent
make dev CONFIG=config.staging.yaml

# Terminal 2: ngrok
ngrok http 8080 --region=eu --log=stdout

# Update Slack Event URL to ngrok HTTPS URL
```

## Security Best Practices

1. **Never Commit Secrets**
   ```bash
   # ✅ Good - use environment variables
   slack:
     signing_secret: ${SLACK_SIGNING_SECRET}

   # ❌ Bad - hardcoded secret
   slack:
     signing_secret: abc123def456  # DON'T DO THIS
   ```

2. **Use HTTPS in Production**
   - Slack REQUIRES HTTPS for webhooks
   - Use Let's Encrypt for free certificates
   - ngrok provides HTTPS automatically

3. **Monitor Failed Verifications**
   ```bash
   grep "Signature verification failed" logs/slack-bridge.log
   # If you see many, investigate:
   # - Wrong secret?
   # - Proxy modifying requests?
   # - Attack attempt?
   ```

4. **Rotate Secrets Periodically**
   - Go to Slack App Settings
   - Basic Information → App Credentials
   - Click "Reset" on Signing Secret
   - Update your config immediately

5. **Restrict Firewall**
   ```bash
   # Only allow Slack IPs (optional but recommended)
   # https://api.slack.com/apis/connections/events-api#ip_allowlisting
   ```

## Additional Resources

- **Slack Documentation**: https://api.slack.com/authentication/verifying-requests-from-slack
- **Event Subscriptions Guide**: https://api.slack.com/events-api
- **Security Best Practices**: https://api.slack.com/security/best-practices
- **ngrok Documentation**: https://ngrok.com/docs
- **Knowledge Agent Docs**: `docs/CONFIGURATION.md`

## FAQs

**Q: Can I use both Socket and Webhook modes simultaneously?**
A: No, choose one. Socket for dev, Webhook for prod is recommended.

**Q: Is webhook mode slower than socket mode?**
A: Slightly (10-50ms difference), but negligible for user experience.

**Q: Do I need to handle signature verification myself?**
A: No, it's automatic. Just configure `SLACK_SIGNING_SECRET`.

**Q: What happens if my webhook endpoint is down?**
A: Slack retries 3 times with exponential backoff. Events may be lost if all retries fail.

**Q: Can I test webhooks locally without ngrok?**
A: No, Slack needs a public HTTPS endpoint. Use Socket Mode for pure local testing.

**Q: Is the signature verification secure enough?**
A: Yes, it uses HMAC-SHA256, the same security used by AWS, Stripe, GitHub, etc.
