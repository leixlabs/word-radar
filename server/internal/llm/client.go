package llm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"word-radar/server/internal/logger"
)

// Client OpenAI-compatible LLM client
type Client struct {
	apiURL      string
	apiKey      string
	model       string
	temperature float64
	client      *http.Client
}

// NewClient 创建 LLM 客户端
func NewClient(apiURL, apiKey, model string, temperature float64) *Client {
	return &Client{
		apiURL:      apiURL,
		apiKey:      apiKey,
		model:       model,
		temperature: temperature,
		client:      &http.Client{Timeout: 6 * time.Minute},
	}
}

// ChatMessage 对话消息
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// JSONSchemaProperty OpenAI JSON Schema property
type JSONSchemaProperty struct {
	Type        string                      `json:"type,omitempty"`
	Description string                      `json:"description,omitempty"`
	Enum        []string                    `json:"enum,omitempty"`
	Items       *JSONSchemaProperty         `json:"items,omitempty"`
	Properties  map[string]JSONSchemaProperty `json:"properties,omitempty"`
	Required    []string                    `json:"required,omitempty"`
}

// JSONSchema OpenAI response_format json_schema
type JSONSchema struct {
	Name   string `json:"name"`
	Strict bool   `json:"strict"`
	Schema map[string]interface{} `json:"schema"`
}

// ResponseFormat OpenAI response_format
type ResponseFormat struct {
	Type       string     `json:"type"`
	JSONSchema JSONSchema `json:"json_schema,omitempty"`
}

// ChatRequest 请求体
type ChatRequest struct {
	Model          string         `json:"model"`
	Messages       []ChatMessage  `json:"messages"`
	Temperature    float64        `json:"temperature"`
	ResponseFormat *ResponseFormat `json:"response_format,omitempty"`
}

// ChatResponse 响应体
type ChatResponse struct {
	Choices []struct {
		Message ChatMessage `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// ChatCompletion 调用 LLM（普通模式）
func (c *Client) ChatCompletion(systemPrompt, userPrompt string) (string, int, error) {
	return c.chat(systemPrompt, userPrompt, nil)
}

// ChatCompletionWithSchema 调用 LLM，强制返回指定 JSON Schema
func (c *Client) ChatCompletionWithSchema(systemPrompt, userPrompt string, schema JSONSchema) (string, int, error) {
	rf := &ResponseFormat{
		Type:       "json_schema",
		JSONSchema: schema,
	}
	return c.chat(systemPrompt, userPrompt, rf)
}

func (c *Client) chat(systemPrompt, userPrompt string, responseFormat *ResponseFormat) (string, int, error) {
	log := logger.L()
	reqBody := ChatRequest{
		Model: c.model,
		Messages: []ChatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		},
		Temperature:    c.temperature,
		ResponseFormat: responseFormat,
	}

	data, err := json.Marshal(reqBody)
	if err != nil {
		return "", 0, fmt.Errorf("marshal request: %w", err)
	}

	url := c.apiURL + "/v1/chat/completions"
	req, err := http.NewRequest("POST", url, bytes.NewReader(data))
	if err != nil {
		return "", 0, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	start := time.Now()
	resp, err := c.client.Do(req)
	elapsed := time.Since(start)
	if err != nil {
		return "", 0, fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", 0, fmt.Errorf("read body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		log.Warn("llm http error",
			slog.Int("status", resp.StatusCode),
			slog.String("body", string(body)),
			slog.Duration("elapsed", elapsed),
		)
		return "", 0, fmt.Errorf("llm returned %d: %s", resp.StatusCode, string(body))
	}

	log.Debug("llm http ok", slog.Duration("elapsed", elapsed))

	var result ChatResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return "", 0, fmt.Errorf("unmarshal response: %w", err)
	}

	if result.Error != nil && result.Error.Message != "" {
		return "", 0, fmt.Errorf("llm error: %s", result.Error.Message)
	}

	if len(result.Choices) == 0 {
		return "", 0, fmt.Errorf("no choices in llm response")
	}

	return result.Choices[0].Message.Content, result.Usage.TotalTokens, nil
}
