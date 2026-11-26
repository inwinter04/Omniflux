# Agent Runtime

For applications requiring stateful conversations, fixed instructions, and dedicated model configurations, the **Omniflux Agent Runtime** provides an enterprise-ready foundation.

## Architecture

An Omniflux Agent encapsulates three components into a single deployable asset:
1. **Locked Prompt Version** (specific PromptTemplate version);
2. **Bound Model Service** (logical service identifier);
3. **Fixed Hyperparameters** (Temperature, Top P, Max Tokens, etc.).

## Session Management

The Agent runtime manages multi-turn conversational history on the server:

1. **First Turn (Session Creation)**:
   - Call `/v1/agents/{name}/chat/completions` with initial `message` and required template `variables`;
   - The response headers provide:
     - `X-Agent-Session-Id`: Unique session identifier;
     - `X-Agent-Run-Id`: Execution run identifier.
2. **Subsequent Turns (Session Continuation)**:
   - Pass `session_id` in subsequent request bodies;
   - Server loads historical context; template variables cannot be modified.

## Version Pinning

- **Active Sessions Lock Their Initial Version**: In-flight sessions remain pinned to the Agent version active when the session was created, preventing disruptions during mid-conversation deployments.
- **New Sessions Adopt the Latest Release**: Subsequent sessions automatically use the latest published Agent version.

## API Examples

### 1. Initiate Session (First Turn)
```bash
curl -i -sS http://127.0.0.1:8080/v1/agents/order-assistant/chat/completions \
  -H "Authorization: Bearer $OMNIFLUX_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "message": "Hi, I need help with an order.",
    "variables": {
      "user_id": "U10086",
      "region": "US-West"
    }
  }'
```

Header response:
```http
HTTP/1.1 200 OK
Content-Type: application/json
X-Agent-Session-Id: ses_01JXXXXXXXXXXXX
X-Agent-Run-Id: run_01JYYYYYYYYYYYY
```

### 2. Continue Session
```bash
curl -sS http://127.0.0.1:8080/v1/agents/order-assistant/chat/completions \
  -H "Authorization: Bearer $OMNIFLUX_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "session_id": "ses_01JXXXXXXXXXXXX",
    "message": "Order number is 20261126-001. Has it shipped?"
  }'
```

Set `"stream": true` for real-time SSE streaming.
