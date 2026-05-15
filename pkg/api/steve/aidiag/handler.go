package aidiag

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/rancher/rancher/pkg/settings"
	"github.com/sirupsen/logrus"
	"k8s.io/apiserver/pkg/endpoints/request"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// DiagRequest is the JSON body for the chat endpoint.
type DiagRequest struct {
	ClusterID string        `json:"clusterId"`
	Kind      string        `json:"kind"`
	Namespace string        `json:"namespace"`
	Name      string        `json:"name"`
	Message   string        `json:"message"`
	History   []ChatMessage `json:"history,omitempty"`
}

// ConfigResponse is returned by the config endpoint.
type ConfigResponse struct {
	Enabled bool   `json:"enabled"`
	Model   string `json:"model"`
}

type Handler struct {
	restConfig *rest.Config
	aiClient   *AIClient
}

func NewHandler(restConfig *rest.Config) *Handler {
	return &Handler{
		restConfig: restConfig,
		aiClient:   NewAIClient(),
	}
}

// Register mounts the AI diagnosis routes onto the given router.
func Register(router *mux.Router, restConfig *rest.Config) {
	h := NewHandler(restConfig)
	router.Handle("/v1/ai-diagnosis/chat", h.withAIEnabled(http.HandlerFunc(h.handleChat))).Methods(http.MethodPost)
	router.Handle("/v1/ai-diagnosis/config", http.HandlerFunc(h.handleConfig)).Methods(http.MethodGet)
}

func (h *Handler) withAIEnabled(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if settings.AIEnabled.Get() != "true" {
			http.Error(w, `{"error":"AI diagnosis feature is not enabled"}`, http.StatusServiceUnavailable)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (h *Handler) handleConfig(w http.ResponseWriter, r *http.Request) {
	resp := ConfigResponse{
		Enabled: settings.AIEnabled.Get() == "true",
		Model:   settings.AIModel.Get(),
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (h *Handler) handleChat(w http.ResponseWriter, r *http.Request) {
	userInfo, ok := request.UserFrom(r.Context())
	if !ok {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	logrus.Infof("AI diagnosis request from user: %s", userInfo.GetName())

	var req DiagRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"invalid request body: %s"}`, err.Error()), http.StatusBadRequest)
		return
	}

	if req.Kind == "" || req.Name == "" {
		http.Error(w, `{"error":"kind and name are required"}`, http.StatusBadRequest)
		return
	}

	if req.Message == "" {
		req.Message = fmt.Sprintf("Please diagnose this %s and identify any issues.", req.Kind)
	}

	impersonateConfig := rest.CopyConfig(h.restConfig)
	impersonateConfig.Impersonate = rest.ImpersonationConfig{
		UserName: userInfo.GetName(),
		Groups:   userInfo.GetGroups(),
	}

	k8sClient, err := kubernetes.NewForConfig(impersonateConfig)
	if err != nil {
		logrus.Errorf("Failed to create impersonated k8s client: %v", err)
		http.Error(w, `{"error":"failed to create kubernetes client"}`, http.StatusInternalServerError)
		return
	}

	collector := NewCollector(k8sClient)
	info, err := collector.Collect(r.Context(), req.Kind, req.Namespace, req.Name)
	if err != nil {
		logrus.Errorf("Failed to collect resource info for %s/%s/%s: %v", req.Kind, req.Namespace, req.Name, err)
		http.Error(w, fmt.Sprintf(`{"error":"failed to collect resource info: %s"}`, err.Error()), http.StatusInternalServerError)
		return
	}

	messages := BuildConversationPrompt(info, req.History, req.Message)

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, `{"error":"streaming not supported"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	flusher.Flush()

	err = h.aiClient.StreamChat(r.Context(), messages, func(content string) error {
		data, _ := json.Marshal(map[string]string{"content": content})
		_, writeErr := fmt.Fprintf(w, "data: %s\n\n", data)
		if writeErr != nil {
			return writeErr
		}
		flusher.Flush()
		return nil
	})

	if err != nil {
		logrus.Errorf("AI streaming error: %v", err)
		errData, _ := json.Marshal(map[string]string{"error": err.Error()})
		fmt.Fprintf(w, "data: %s\n\n", errData)
		flusher.Flush()
	}

	fmt.Fprintf(w, "data: [DONE]\n\n")
	flusher.Flush()
}
