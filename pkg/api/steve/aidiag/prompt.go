package aidiag

import (
	"fmt"
	"strings"
)

const systemPrompt = `你是一名集成在 Rancher 管理平台中的 Kubernetes 诊断专家。
你拥有集群的访问权限，已经自动采集了目标资源的状态、事件、日志和条件等诊断数据。

规则：
- 始终使用中文回复。
- 简洁但全面，优先分析最可能的根因。
- 诊断时使用以下结构：问题概述、根因分析、建议操作。
- 你已经拥有集群访问权限并已自动采集了诊断数据，不要让用户去手动执行 kubectl 命令来收集信息。
- 直接基于已提供的数据给出分析结论，不要列出"排查步骤"让用户自己去执行。
- 建议操作应当是具体的修复方案（如修改配置、调整资源、重启服务等），而不是信息收集命令。
- 如果需要其他关联资源的诊断数据（如依赖的 Service、Elasticsearch 等），告诉用户可以在 Rancher 界面中对那些资源使用"AI 诊断"功能来进一步分析。
- 如果资源运行正常，请确认并指出可能存在的次要告警。
- 不要捏造信息。如果提供的上下文不足以确定根因，明确说明缺少哪些信息，并引导用户对相关资源使用 AI 诊断。
- 使用 Markdown 格式以提高可读性。`

// BuildDiagnosticPrompt assembles the resource context into a user message
// for the AI to analyze.
func BuildDiagnosticPrompt(info *ResourceInfo, userMessage string) []ChatMessage {
	messages := []ChatMessage{
		{Role: "system", Content: systemPrompt},
	}

	var contextParts []string

	contextParts = append(contextParts, fmt.Sprintf("## 资源: %s/%s", info.Kind, info.Name))
	if info.Namespace != "" {
		contextParts = append(contextParts, fmt.Sprintf("命名空间: %s", info.Namespace))
	}
	if info.Status != "" {
		contextParts = append(contextParts, fmt.Sprintf("状态: %s", info.Status))
	}

	if len(info.Conditions) > 0 {
		contextParts = append(contextParts, "\n### 条件")
		for _, c := range info.Conditions {
			contextParts = append(contextParts, "- "+c)
		}
	}

	if len(info.Extra) > 0 {
		contextParts = append(contextParts, "\n### 详情")
		for k, v := range info.Extra {
			contextParts = append(contextParts, fmt.Sprintf("- %s: %s", k, v))
		}
	}

	if len(info.Events) > 0 {
		contextParts = append(contextParts, "\n### 事件")
		for _, e := range info.Events {
			contextParts = append(contextParts, "- "+e)
		}
	}

	if len(info.Logs) > 0 {
		contextParts = append(contextParts, "\n### 日志")
		for _, l := range info.Logs {
			contextParts = append(contextParts, l)
		}
	}

	resourceContext := strings.Join(contextParts, "\n")

	userContent := fmt.Sprintf("以下是 Kubernetes 资源的诊断上下文信息:\n\n%s\n\n用户请求: %s",
		resourceContext, userMessage)

	messages = append(messages, ChatMessage{
		Role:    "user",
		Content: userContent,
	})

	return messages
}

// BuildFreeChat builds messages for general conversation without resource context.
func BuildFreeChat(history []ChatMessage, userMessage string) []ChatMessage {
	messages := []ChatMessage{
		{Role: "system", Content: systemPrompt},
	}
	messages = append(messages, history...)
	messages = append(messages, ChatMessage{Role: "user", Content: userMessage})

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
