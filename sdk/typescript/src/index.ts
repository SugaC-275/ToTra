/**
 * ToTra TypeScript SDK
 *
 * Zero-dependency TypeScript/Node.js client for the ToTra AI Gateway.
 * Provides an OpenAI-compatible interface using native fetch.
 *
 * @example
 * ```ts
 * import ToTra from "totra-sdk";
 *
 * const client = new ToTra("sk-my-key", "https://gateway.example.com");
 *
 * // Direct method
 * const resp = await client.complete("gpt-4o", [{ role: "user", content: "Hi" }]);
 *
 * // OpenAI drop-in
 * const resp2 = await client.chat.completions.create("gpt-4o", messages);
 *
 * // Streaming
 * for await (const chunk of client.stream("gpt-4o", messages)) {
 *   process.stdout.write(chunk);
 * }
 * ```
 */

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

export interface ChatMessage {
  role: string;
  content: string;
}

export interface ChatCompletionOptions {
  maxTokens?: number;
  max_tokens?: number;
  temperature?: number;
  system?: string;
  stream?: boolean;
  [key: string]: unknown;
}

export interface ChatCompletionChoice {
  index: number;
  message: ChatMessage;
  finish_reason: string;
}

export interface ChatCompletionUsage {
  prompt_tokens: number;
  completion_tokens: number;
  total_tokens: number;
}

export interface ChatCompletionResponse {
  id: string;
  object: string;
  model: string;
  choices: ChatCompletionChoice[];
  usage: ChatCompletionUsage;
}

export interface EmbeddingData {
  index: number;
  embedding: number[];
}

export interface EmbeddingResponse {
  object: string;
  model: string;
  data: EmbeddingData[];
}

export interface PromptTemplate {
  name: string;
  content: string;
  version?: number;
}

// ---------------------------------------------------------------------------
// Error class
// ---------------------------------------------------------------------------

export class ToTraError extends Error {
  constructor(
    public readonly statusCode: number,
    public readonly body: string,
  ) {
    super(`ToTra API error ${statusCode}: ${body}`);
    this.name = "ToTraError";
  }
}

export class ToTraConnectionError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "ToTraConnectionError";
  }
}

// ---------------------------------------------------------------------------
// Main client
// ---------------------------------------------------------------------------

export class ToTra {
  private readonly baseUrl: string;
  private readonly headers: Record<string, string>;

  /** OpenAI-compatible namespace: client.chat.completions.create(...) */
  readonly chat: {
    completions: {
      create: (
        model: string,
        messages: ChatMessage[],
        opts?: ChatCompletionOptions,
      ) => Promise<ChatCompletionResponse>;
    };
  };

  /** Prompt template management */
  readonly prompts: {
    list: () => Promise<PromptTemplate[]>;
    get: (name: string) => Promise<PromptTemplate>;
    save: (name: string, content: string) => Promise<PromptTemplate>;
    render: (name: string, vars: Record<string, string>) => Promise<string>;
  };

  constructor(apiKey: string, baseUrl: string) {
    this.baseUrl = baseUrl.replace(/\/$/, "");
    this.headers = {
      Authorization: `Bearer ${apiKey}`,
      "Content-Type": "application/json",
    };

    // Bind the OpenAI-compat namespace
    this.chat = {
      completions: {
        create: (
          model: string,
          messages: ChatMessage[],
          opts?: ChatCompletionOptions,
        ) => this._chatCreate(model, messages, opts),
      },
    };

    // Bind the prompts namespace
    this.prompts = {
      list: () => this._get<PromptTemplate[]>("/v1/prompts"),
      get: (name: string) => this._get<PromptTemplate>(`/v1/prompts/${name}`),
      save: (name: string, content: string) =>
        this._post<PromptTemplate>("/v1/prompts", { name, content }),
      render: async (name: string, vars: Record<string, string>) => {
        const result = await this._post<{ rendered: string }>(
          `/v1/prompts/${name}/render`,
          { variables: vars },
        );
        return result.rendered ?? "";
      },
    };
  }

  // ------------------------------------------------------------------
  // Public methods
  // ------------------------------------------------------------------

  /**
   * Send a chat-completions request and return the full response.
   */
  async complete(
    model: string,
    messages: ChatMessage[],
    opts?: ChatCompletionOptions,
  ): Promise<ChatCompletionResponse> {
    return this._chatCreate(model, messages, opts);
  }

  /**
   * Request text embeddings.
   *
   * @returns A 2-D array — one embedding vector per input string.
   */
  async embed(
    model: string,
    input: string | string[],
  ): Promise<number[][]> {
    const body = { model, input };
    const resp = await this._post<EmbeddingResponse>("/v1/embeddings", body);
    const sorted = [...resp.data].sort((a, b) => a.index - b.index);
    return sorted.map((d) => d.embedding);
  }

  /**
   * Stream a chat-completions request.
   *
   * @yields Individual text delta strings as they arrive.
   *
   * @example
   * ```ts
   * for await (const chunk of client.stream("gpt-4o", messages)) {
   *   process.stdout.write(chunk);
   * }
   * ```
   */
  async *stream(
    model: string,
    messages: ChatMessage[],
    opts?: ChatCompletionOptions,
  ): AsyncGenerator<string> {
    const { system, maxTokens, max_tokens, temperature, ...rest } = opts ?? {};
    const allMessages: ChatMessage[] = system
      ? [{ role: "system", content: system }, ...messages]
      : messages;

    const payload: Record<string, unknown> = {
      model,
      messages: allMessages,
      stream: true,
      ...rest,
    };
    if (maxTokens !== undefined) payload["max_tokens"] = maxTokens;
    if (max_tokens !== undefined) payload["max_tokens"] = max_tokens;
    if (temperature !== undefined) payload["temperature"] = temperature;

    let response: Response;
    try {
      response = await fetch(`${this.baseUrl}/v1/chat/completions`, {
        method: "POST",
        headers: this.headers,
        body: JSON.stringify(payload),
      });
    } catch (err) {
      throw new ToTraConnectionError(String(err));
    }

    if (!response.ok) {
      const text = await response.text();
      throw new ToTraError(response.status, text);
    }

    if (!response.body) {
      throw new ToTraError(response.status, "No response body for streaming");
    }

    const reader = response.body.getReader();
    const decoder = new TextDecoder();
    let buffer = "";

    try {
      while (true) {
        const { done, value } = await reader.read();
        if (done) break;

        buffer += decoder.decode(value, { stream: true });
        const lines = buffer.split("\n");
        buffer = lines.pop() ?? "";

        for (const line of lines) {
          const trimmed = line.trim();
          if (!trimmed || !trimmed.startsWith("data:")) continue;
          const data = trimmed.slice("data:".length).trim();
          if (data === "[DONE]") return;
          try {
            const chunk = JSON.parse(data) as {
              choices?: Array<{ delta?: { content?: string } }>;
            };
            const content = chunk.choices?.[0]?.delta?.content;
            if (content) yield content;
          } catch {
            // Ignore malformed SSE chunks
          }
        }
      }
    } finally {
      reader.releaseLock();
    }
  }

  // ------------------------------------------------------------------
  // Private helpers
  // ------------------------------------------------------------------

  private async _chatCreate(
    model: string,
    messages: ChatMessage[],
    opts?: ChatCompletionOptions,
  ): Promise<ChatCompletionResponse> {
    const { system, maxTokens, max_tokens, temperature, stream: _stream, ...rest } = opts ?? {};
    const allMessages: ChatMessage[] = system
      ? [{ role: "system", content: system }, ...messages]
      : messages;

    const payload: Record<string, unknown> = {
      model,
      messages: allMessages,
      stream: false,
      ...rest,
    };
    if (maxTokens !== undefined) payload["max_tokens"] = maxTokens;
    if (max_tokens !== undefined) payload["max_tokens"] = max_tokens;
    if (temperature !== undefined) payload["temperature"] = temperature;

    return this._post<ChatCompletionResponse>("/v1/chat/completions", payload);
  }

  private async _get<T>(path: string): Promise<T> {
    let response: Response;
    try {
      response = await fetch(`${this.baseUrl}${path}`, {
        method: "GET",
        headers: this.headers,
      });
    } catch (err) {
      throw new ToTraConnectionError(String(err));
    }
    if (!response.ok) {
      const text = await response.text();
      throw new ToTraError(response.status, text);
    }
    return response.json() as Promise<T>;
  }

  private async _post<T>(path: string, body: unknown): Promise<T> {
    let response: Response;
    try {
      response = await fetch(`${this.baseUrl}${path}`, {
        method: "POST",
        headers: this.headers,
        body: JSON.stringify(body),
      });
    } catch (err) {
      throw new ToTraConnectionError(String(err));
    }
    if (!response.ok) {
      const text = await response.text();
      throw new ToTraError(response.status, text);
    }
    return response.json() as Promise<T>;
  }
}

export default ToTra;

// OpenAI drop-in alias
export { ToTra as OpenAI };
