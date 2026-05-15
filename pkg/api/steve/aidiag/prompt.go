package aidiag

import (
	"fmt"
	"strings"
)

const systemPrompt = `You are a Kubernetes diagnostic expert integrated into the Rancher management platform.
Your role is to analyze Kubernetes resource status, events, logs, and conditions to identify problems
and provide actionable remediation steps.

Rules:
- Respond in the same language as the user's message.
- Be concise but thorough. Prioritize the most likely root cause.
- When diagnosing, structure your response with: Problem Summary, Root Cause Analysis, Recommended Actions.
- If the resource appears healthy, confirm that and mention any minor warnings.
- Never fabricate information. If the provided context is insufficient, say so.
- Format your response using Markdown for readability.`

// BuildDiagnosticPrompt assembles the resource context into a user message
// for the AI to analyze.
func BuildDiagnosticPrompt(info *ResourceInfo, userMessage string) []ChatMessage {
	messages := []ChatMessage{
		{Role: "system", Content: systemPrompt},
	}

	var contextParts []string

	contextParts = append(contextParts, fmt.Sprintf("## Resource: %s/%s", info.Kind, info.Name))
	if info.Namespace != "" {
		contextParts = append(contextParts, fmt.Sprintf("Namespace: %s", info.Namespace))
	}
	if info.Status != "" {
		contextParts = append(contextParts, fmt.Sprintf("Status: %s", info.Status))
	}

	if len(info.Conditions) > 0 {
		contextParts = append(contextParts, "\n### Conditions")
		for _, c := range info.Conditions {
			contextParts = append(contextParts, "- "+c)
		}
	}

	if len(info.Extra) > 0 {
		contextParts = append(contextParts, "\n### Details")
		for k, v := range info.Extra {
			contextParts = append(contextParts, fmt.Sprintf("- %s: %s", k, v))
		}
	}

	if len(info.Events) > 0 {
		contextParts = append(contextParts, "\n### Events")
		for _, e := range info.Events {
			contextParts = append(contextParts, "- "+e)
		}
	}

	if len(info.Logs) > 0 {
		contextParts = append(contextParts, "\n### Logs")
		for _, l := range info.Logs {
			contextParts = append(contextParts, l)
		}
	}

	resourceContext := strings.Join(contextParts, "\n")

	userContent := fmt.Sprintf("Here is the Kubernetes resource diagnostic context:\n\n%s\n\nUser request: %s",
		resourceContext, userMessage)

	messages = append(messages, ChatMessage{
		Role:    "user",
		Content: userContent,
	})

	return messages
}

// BuildConversationPrompt builds messages for follow-up conversation, keeping
// the original resource context as the first message and appending history.
func BuildConversationPrompt(info *ResourceInfo, history []ChatMessage, userMessage string) []ChatMessage {
	if len(history) == 0 {
		return BuildDiagnosticPrompt(info, userMessage)
	}

	messages := []ChatMessage{
		{Role: "system", Content: systemPrompt},
	}

	messages = append(messages, history...)

	messages = append(messages, ChatMessage{
		Role:    "user",
		Content: userMessage,
	})

	return messages
}
