package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	openai "github.com/sashabaranov/go-openai"
)

var modelFilter map[string]struct{}

func loadModelFilter(path string) (map[string]struct{}, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	filter := make(map[string]struct{})

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			filter[line] = struct{}{}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return filter, nil
}

func main() {
	r := gin.Default()
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		if len(os.Args) > 1 {
			apiKey = os.Args[len(os.Args)-1]
		} else {
			slog.Error("OPENAI_API_KEY environment variable or command-line argument not set.")
			return
		}
	}

	baseUrl := os.Getenv("OPENAI_BASE_URL")
	if baseUrl == "" {
		if len(os.Args) > 2 {
			baseUrl = os.Args[1]
		} else {
			baseUrl = "https://openrouter.ai/api/v1/"
		}
	}

	provider := NewOpenrouterProvider(baseUrl, apiKey)

	filter, err := loadModelFilter("models-filter")
	if err != nil {
		if os.IsNotExist(err) {
			slog.Info("models-filter file not found. Skipping model filtering.")
			modelFilter = make(map[string]struct{})
		} else {
			slog.Error("Error loading models filter", "Error", err)
			return
		}
	} else {
		modelFilter = filter
		slog.Info("Loaded models from filter:")
		for model := range modelFilter {
			slog.Info(" - " + model)
		}
	}

	r.GET("/", func(c *gin.Context) {
		c.String(http.StatusOK, "Ollama is running")
	})
	r.HEAD("/", func(c *gin.Context) {
		c.String(http.StatusOK, "")
	})

	r.GET("/api/tags", func(c *gin.Context) {
		models, err := provider.GetModels()
		if err != nil {
			slog.Error("Error getting models", "Error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		filter := modelFilter
		newModels := make([]map[string]interface{}, 0, len(models))
		for _, m := range models {
			if len(filter) > 0 {
				if _, ok := filter[m.Model]; !ok {
					continue
				}
			}
			newModels = append(newModels, map[string]interface{}{
				"name":        m.Name,
				"model":       m.Model,
				"modified_at": m.ModifiedAt,
				"size":        270898672,
				"digest":      "9077fe9d2ae1a4a41a868836b56b8163731a8fe16621397028c2c76f838c6907",
				"details":     m.Details,
			})
		}

		c.JSON(http.StatusOK, gin.H{"models": newModels})
	})

	r.POST("/api/show", func(c *gin.Context) {
		var request map[string]string
		if err := c.BindJSON(&request); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON payload"})
			return
		}

		modelName := request["name"]
		if modelName == "" {
			modelName = request["model"]
		}
		if modelName == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Model name is required"})
			return
		}

		details, err := provider.GetModelDetails(modelName)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, details)
	})

	r.POST("/api/chat", func(c *gin.Context) {
		var request struct {
			Model    string                         `json:"model"`
			Messages []openai.ChatCompletionMessage `json:"messages"`
			Stream   *bool                          `json:"stream"`
		}

		if err := c.ShouldBindJSON(&request); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON payload"})
			return
		}

		streamRequested := true
		if request.Stream != nil {
			streamRequested = *request.Stream
		}

		if !streamRequested {
			fullModelName, err := provider.GetFullModelName(request.Model)
			if err != nil {
				slog.Error("Error getting full model name", "Error", err)
				c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
				return
			}

			response, err := provider.Chat(request.Messages, fullModelName)
			if err != nil {
				slog.Error("Failed to get chat response", "Error", err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}

			if len(response.Choices) == 0 {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "No response from model"})
				return
			}

			content := ""
			if len(response.Choices) > 0 && response.Choices[0].Message.Content != "" {
				content = response.Choices[0].Message.Content
			}

			finishReason := "stop"
			if response.Choices[0].FinishReason != "" {
				finishReason = string(response.Choices[0].FinishReason)
			}

			ollamaResponse := map[string]interface{}{
				"model":      fullModelName,
				"created_at": time.Now().Format(time.RFC3339),
				"message": map[string]string{
					"role":    "assistant",
					"content": content,
				},
				"done":              true,
				"finish_reason":     finishReason,
				"total_duration":    response.Usage.TotalTokens * 10,
				"load_duration":     0,
				"prompt_eval_count": response.Usage.PromptTokens,
				"eval_count":        response.Usage.CompletionTokens,
				"eval_duration":     response.Usage.CompletionTokens * 10,
			}

			c.JSON(http.StatusOK, ollamaResponse)
			return
		}

		slog.Info("Requested model", "model", request.Model)
		fullModelName, err := provider.GetFullModelName(request.Model)
		if err != nil {
			slog.Error("Error getting full model name", "Error", err, "model", request.Model)
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		slog.Info("Using model", "fullModelName", fullModelName)

		stream, err := provider.ChatStream(request.Messages, fullModelName)
		if err != nil {
			slog.Error("Failed to create stream", "Error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		defer stream.Close()

		c.Writer.Header().Set("Content-Type", "application/x-ndjson")
		c.Writer.Header().Set("Cache-Control", "no-cache")
		c.Writer.Header().Set("Connection", "keep-alive")

		w := c.Writer
		flusher, ok := w.(http.Flusher)
		if !ok {
			slog.Error("Expected http.ResponseWriter to be an http.Flusher")
			return
		}

		var lastFinishReason string

		for {
			response, err := stream.Recv()
			if errors.Is(err, io.EOF) {

				break
			}
			if err != nil {
				slog.Error("Backend stream error", "Error", err)
				errorMsg := map[string]string{"error": "Stream error: " + err.Error()}
				errorJson, _ := json.Marshal(errorMsg)
				fmt.Fprintf(w, "%s\n", string(errorJson))
				flusher.Flush()
				return
			}

			if len(response.Choices) > 0 && response.Choices[0].FinishReason != "" {
				lastFinishReason = string(response.Choices[0].FinishReason)
			}

			responseJSON := map[string]interface{}{
				"model":      fullModelName,
				"created_at": time.Now().Format(time.RFC3339),
				"message": map[string]string{
					"role":    "assistant",
					"content": response.Choices[0].Delta.Content,
				},
				"done": false,
			}

			jsonData, err := json.Marshal(responseJSON)
			if err != nil {
				slog.Error("Error marshaling intermediate response JSON", "Error", err)
				return
			}

			fmt.Fprintf(w, "%s\n", string(jsonData))
			flusher.Flush()
		}

		if lastFinishReason == "" {
			lastFinishReason = "stop"
		}

		finalResponse := map[string]interface{}{
			"model":      fullModelName,
			"created_at": time.Now().Format(time.RFC3339),
			"message": map[string]string{
				"role":    "assistant",
				"content": "",
			},
			"done":              true,
			"finish_reason":     lastFinishReason,
			"total_duration":    0,
			"load_duration":     0,
			"prompt_eval_count": 0,
			"eval_count":        0,
			"eval_duration":     0,
		}

		finalJsonData, err := json.Marshal(finalResponse)
		if err != nil {
			slog.Error("Error marshaling final response JSON", "Error", err)
			return
		}

		fmt.Fprintf(w, "%s\n", string(finalJsonData))
		flusher.Flush()
	})

	r.Run(":11434")
}
