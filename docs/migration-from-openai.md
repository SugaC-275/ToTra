# Migrate from OpenAI to ToTra in 5 Minutes

ToTra is an OpenAI-compatible gateway. You change **one import** and one URL — everything else stays the same.

---

## Python

**Before:**

```python
from openai import OpenAI

client = OpenAI(api_key="sk-...")
```

**After:**

```python
from totra import OpenAI

client = OpenAI(
    api_key="your-totra-key",
    base_url="https://your-gateway.example.com",
)
```

Install:

```bash
pip install totra-sdk
```

---

## TypeScript / Node.js

**Before:**

```typescript
import OpenAI from "openai";

const client = new OpenAI({ apiKey: "sk-..." });
```

**After:**

```typescript
import { OpenAI } from "totra-sdk";

const client = new OpenAI({
  apiKey: "your-totra-key",
  baseURL: "https://your-gateway.example.com",
});
```

Install:

```bash
npm install totra-sdk
```

---

## What you get (without changing any other code)

| Feature | Benefit |
|---|---|
| Multi-provider failover | OpenAI → Azure → Anthropic automatically on failure |
| Cost tracking | Per-user, per-team spend dashboards |
| Budget enforcement | Hard-stop or rate-limit when budgets are hit |
| PII detection | Requests scanned before leaving your network |
| Compliance audit trail | Every request logged with model, cost, latency |
| Semantic caching | 30–50% cost reduction typical on repeated queries |
| Rate limiting | Per-user and per-team quotas |

---

## Advanced features

These are ToTra-specific extensions available on the client:

```python
from totra import OpenAI

client = OpenAI(api_key="your-totra-key", base_url="https://your-gateway.example.com")

# Prompt library — fetch a managed prompt by ID
prompt = client.prompts.get("summarize-v2")
response = client.chat.completions.create(
    model="gpt-4o",
    messages=prompt.messages + [{"role": "user", "content": user_input}],
)

# Budget check — remaining spend for the current key
budget = client.budget.get()
print(f"Used: ${budget.used:.4f} / ${budget.limit:.2f}")

# Evals — run a named eval suite against a prompt
result = client.evals.run("hallucination-check", prompt_id="summarize-v2")
print(result.pass_rate)
```

```typescript
import { OpenAI } from "totra-sdk";

const client = new OpenAI({ apiKey: "your-totra-key", baseURL: "https://your-gateway.example.com" });

// Prompt library
const prompt = await client.prompts.get("summarize-v2");

// Budget
const budget = await client.budget.get();
console.log(`Used: $${budget.used.toFixed(4)} / $${budget.limit}`);

// Evals
const result = await client.evals.run("hallucination-check", { promptId: "summarize-v2" });
console.log(result.passRate);
```

---

## Environment variables

ToTra respects the same env vars as OpenAI SDKs — just point them at your gateway:

| Variable | Purpose |
|---|---|
| `TOTRA_API_KEY` | Your gateway API key (or use `OPENAI_API_KEY`) |
| `TOTRA_BASE_URL` | Gateway URL (or use `OPENAI_BASE_URL`) |

```bash
export OPENAI_API_KEY="your-totra-key"
export OPENAI_BASE_URL="https://your-gateway.example.com"
# No code changes needed — existing apps pick these up automatically
```

---

## Rollback

If you need to revert, swap the import back. There are no lock-in side effects.
