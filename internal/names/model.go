// Package names provides utilities for generating human-readable model names
// from model IDs. It uses a combination of static mappings and Levenshtein
// distance-based fuzzy matching to provide consistent, user-friendly names.
package names

import (
	"regexp"
	"strings"
)

// modelNames maps model IDs to their human-readable display names.
var modelNames = map[string]string{
	// Anthropic
	"claude-sonnet-4-5-20250929": "Claude Sonnet 4.5",
	"claude-opus-4-5-20251101":   "Claude Opus 4.5",
	"claude-3-5-haiku-20241022":  "Claude 3.5 Haiku",
	"claude-3-5-sonnet-20241022": "Claude 3.5 Sonnet",
	"claude-3-opus-20240229":     "Claude 3 Opus",
	"claude-3-haiku-20240307":    "Claude 3 Haiku",
	"claude-sonnet-4":            "Claude Sonnet 4",
	"claude-sonnet-4-5":          "Claude Sonnet 4.5",
	"claude-opus-4":              "Claude Opus 4",
	"claude-opus-4-1":            "Claude Opus 4.1",
	"claude-opus-4-5":            "Claude Opus 4.5",
	"claude-opus-4-5-think":      "Claude Opus 4.5 Think",
	"claude-sonnet-4-5-20250214": "Claude Sonnet 4.5",
	"claude-haiku-4-5":           "Claude Haiku 4.5",
	"claude-3-5-haiku":           "Claude 3.5 Haiku",
	"claude-3-5-sonnet":          "Claude 3.5 Sonnet",
	"claude-sonnet-4-0":          "Claude Sonnet 4",
	"claude-opus-4-0":            "Claude Opus 4",
	"claude-sonnet-4-5-think":    "Claude Sonnet 4.5 Think",
	"claude-3-7-sonnet":          "Claude 3.7 Sonnet",

	// OpenAI
	"gpt-5.2":              "GPT-5.2",
	"gpt-5.2-codex":        "GPT-5.2 Codex",
	"gpt-5.1-codex":        "GPT-5.1 Codex",
	"gpt-5.1":              "GPT-5.1",
	"gpt-4.1":              "GPT-4.1",
	"gpt-4.1-mini":         "GPT-4.1 Mini",
	"gpt-4.1-nano":         "GPT-4.1 Nano",
	"gpt-4-turbo":          "GPT-4 Turbo",
	"gpt-4-turbo-preview":  "GPT-4 Turbo Preview",
	"gpt-4-vision-preview": "GPT-4 Vision",
	"gpt-3.5-turbo":        "GPT-3.5 Turbo",
	"gpt-3.5-turbo-16k":    "GPT-3.5 Turbo 16K",
	"o1-preview":           "O1 Preview",
	"o1-mini":              "O1 Mini",
	"o1":                   "O1",
	"o3":                   "O3",
	"o3-mini":              "O3 Mini",
	"o3-pro":               "O3 Pro",
	"o4-mini":              "O4 Mini",
	"gpt-5":                "GPT-5",
	"gpt-5-pro":            "GPT-5 Pro",
	"gpt-5-mini":           "GPT-5 Mini",
	"gpt-5-nano":           "GPT-5 Nano",
	"gpt-5-codex":          "GPT-5 Codex",

	// DeepSeek
	"deepseek-r1":            "DeepSeek R1",
	"deepseek-v3":            "DeepSeek V3",
	"deepseek-v3-fast":       "DeepSeek V3 Fast",
	"deepseek-v3.1-fast":     "DeepSeek V3.1 Fast",
	"deepseek-v3.1-terminus": "DeepSeek V3.1 Terminus",
	"deepseek-v3.1-think":    "DeepSeek V3.1 Think",
	"deepseek-v3.2":          "DeepSeek V3.2",
	"deepseek-v3.2-exp":      "DeepSeek V3.2 Exp",
	"deepseek-v3.2-fast":     "DeepSeek V3.2 Fast",
	"deepseek-v3.2-think":    "DeepSeek V3.2 Think",
	"deepseek-v3.2-speciale": "DeepSeek V3.2 Speciale",
	"deepseek-math-v2":       "DeepSeek Math V2",
	"deepseek-ocr":           "DeepSeek OCR",

	// Microsoft Phi
	"phi-4-mini-reasoning": "Phi 4 Mini",
	"phi-4-reasoning":      "Phi 4",
	"phi-4-mini":           "Phi 4 Mini",
	"phi-4":                "Phi 4",
	"phi-3.5-mini":         "Phi 3.5 Mini",
	"phi-3.5":              "Phi 3.5",
	"phi-3-mini":           "Phi 3 Mini",
	"phi-3":                "Phi 3",

	// ByteDance
	"bytedance-seed/seed-oss-36b-instruct": "ByteDance Seed OSS 36B",

	// Google/Gemini
	"gemini-3-flash-preview":                 "Gemini 3.0 Flash Preview",
	"gemini-3-flash-preview-free":            "Gemini 3.0 Flash Preview (Free)",
	"gemini-2.5-pro":                         "Gemini 2.5 Pro",
	"gemini-2.5-flash":                       "Gemini 2.5 Flash",
	"gemini-2.5-flash-lite":                  "Gemini 2.5 Flash Lite",
	"gemini-2.5-flash-lite-preview-09-2025":  "Gemini 2.5 Flash Lite Preview",
	"gemini-2.5-flash-preview-09-2025":       "Gemini 2.5 Flash Preview",
	"gemini-2.5-flash-preview-05-20-nothink": "Gemini 2.5 Flash Preview (No Think)",
	"gemini-2.5-flash-preview-05-20-search":  "Gemini 2.5 Flash Search",
	"gemini-2.5-flash-nothink":               "Gemini 2.5 Flash (No Think)",
	"gemini-2.5-flash-search":                "Gemini 2.5 Flash Search",
	"gemini-2.5-pro-preview-05-06":           "Gemini 2.5 Pro Preview",
	"gemini-2.5-pro-preview-06-05":           "Gemini 2.5 Pro Preview",
	"gemini-2.5-pro-search":                  "Gemini 2.5 Pro Search",
	"gemini-2.0-pro-exp-02-05":               "Gemini 2.0 Pro",
	"gemini-2.0-flash-exp":                   "Gemini 2.0 Flash",
	"gemini-2.0-flash-free":                  "Gemini 2.0 Flash (Free)",
	"gemini-1.5-pro":                         "Gemini 1.5 Pro",
	"gemini-1.5-flash":                       "Gemini 1.5 Flash",
	"gemini-1.5-flash-8b":                    "Gemini 1.5 Flash 8B",
	"gemini-1.0-pro":                         "Gemini 1.0 Pro",

	// Zhipu AI (GLM)
	"glm-4.7":       "GLM-4.7",
	"glm-4.7-flash": "GLM-4.7 Flash",
	"glm-4.6":       "GLM-4.6",
	"glm-4.6v":      "GLM-4.6 Vision",
	"glm-4.5v":      "GLM-4.5 Vision",
	"glm-4-flash":   "GLM-4 Flash",
	"glm-4-plus":    "GLM-4 Plus",
	"glm-4-air":     "GLM-4 Air",

	// Meta (Llama)
	"llama-4-maverick":             "Llama 4 Maverick",
	"llama-4-scout":                "Llama 4 Scout",
	"llama-3.3-70b-instruct":       "Llama 3.3 70B",
	"llama-3.2-90b-vision-preview": "Llama 3.2 90B Vision",
	"llama-3.2-11b-vision-preview": "Llama 3.2 11B Vision",
	"llama-3.2-3b-instruct":        "Llama 3.2 3B",
	"llama-3.2-1b-instruct":        "Llama 3.2 1B",
	"llama-3.1-405b-instruct":      "Llama 3.1 405B",
	"llama-3.1-70b-instruct":       "Llama 3.1 70B",
	"llama-3.1-8b-instruct":        "Llama 3.1 8B",
	"llama-3-70b-instruct":         "Llama 3 70B",
	"llama-3-8b-instruct":          "Llama 3 8B",
	"llama-2-70b-chat":             "Llama 2 70B",
	"llama-2-13b-chat":             "Llama 2 13B",
	"llama-2-7b-chat":              "Llama 2 7B",

	// Mistral
	"mistral-large-2411":          "Mistral Large",
	"mistral-large-3":             "Mistral Large 3",
	"mistral-large-2402":          "Mistral Large (2024.02)",
	"mistral-medium-2312":         "Mistral Medium",
	"mistral-small-2402":          "Mistral Small",
	"mistral-7b-instruct-v0.3":    "Mistral 7B v0.3",
	"mixtral-8x7b-instruct-v0.1":  "Mixtral 8x7B",
	"mixtral-8x22b-instruct-v0.1": "Mixtral 8x22B",
	"mistral-nemo":                "Mistral Nemo",
	"codestral-latest":            "Codestral",
	"codestral-2405":              "Codestral",

	// Cohere
	"command-r-plus":      "Command R+",
	"command-r-08-2024":   "Command R",
	"command-r7b-12-2024": "Command R7B",
	"command-light":       "Command Light",
	"command":             "Command",

	// AI21
	"jamba-large-1.7": "Jamba Large 1.7",
	"jamba-mini-1.7":  "Jamba Mini 1.7",

	// X.AI (Grok)
	"grok-4-1-fast-non-reasoning": "Grok 4.1 Fast",
	"grok-4-1-fast-reasoning":     "Grok 4.1 Fast (Reasoning)",
	"grok-code-fast-1":            "Grok Code Fast",
	"grok-2":                      "Grok 2",
	"grok-2-1212":                 "Grok 2",
	"grok-1-5":                    "Grok 1.5",
	"grok-beta":                   "Grok Beta",

	// Alibaba (Qwen)
	"qwen-2.5-coder-32b-instruct": "Qwen 2.5 Coder 32B",
	"qwen-2.5-72b-instruct":       "Qwen 2.5 72B",
	"qwen-2.5-14b-instruct":       "Qwen 2.5 14B",
	"qwen-2.5-7b-instruct":        "Qwen 2.5 7B",
	"qwen-2.5-3b-instruct":        "Qwen 2.5 3B",
	"qwen-2-72b-instruct":         "Qwen 2 72B",
	"qwen-2-7b-instruct":          "Qwen 2 7B",
	"qwq-32b-preview":             "Qwen QwQ 32B",
	"qwq-32b":                     "Qwen QwQ 32B",

	// Baidu (ERNIE)
	"ernie-5.0-thinking-exp":     "ERNIE 5.0 Thinking",
	"ernie-5.0-thinking-preview": "ERNIE 5.0 Thinking Preview",
	"ernie-4.5":                  "ERNIE 4.5",
	"ernie-4.5-turbo-latest":     "ERNIE 4.5 Turbo",
	"ernie-4.5-turbo-vl":         "ERNIE 4.5 Turbo Vision",
	"ernie-x1.1-preview":         "ERNIE X1.1 Preview",
	"ernie-x1-turbo":             "ERNIE X1 Turbo",

	// Kimi
	"kimi-for-coding-free":  "Kimi for Coding (Free)",
	"kimi-k2-thinking":      "Kimi K2 Thinking",
	"kimi-k2-0905":          "Kimi K2",
	"kimi-k2-0711":          "Kimi K2 (0711)",
	"kimi-k2-turbo-preview": "Kimi K2 Turbo Preview",

	// Qwen (Alibaba)
	"qwen3-vl-235b-a22b-instruct":    "Qwen3 VL 235B",
	"qwen3-vl-235b-a22b-thinking":    "Qwen3 VL 235B Thinking",
	"qwen3-vl-30b-a3b-instruct":      "Qwen3 VL 30B",
	"qwen3-vl-30b-a3b-thinking":      "Qwen3 VL 30B Thinking",
	"qwen3-vl-plus":                  "Qwen3 VL Plus",
	"qwen3-max":                      "Qwen3 Max",
	"qwen3-next-80b-a3b-instruct":    "Qwen3 Next 80B",
	"qwen3-next-80b-a3b-thinking":    "Qwen3 Next 80B Thinking",
	"qwen3-235b-a22b-instruct-2507":  "Qwen3 235B",
	"qwen3-235b-a22b-thinking-2507":  "Qwen3 235B Thinking",
	"qwen3-235b-a22b":                "Qwen3 235B",
	"qwen3-coder-30b-a3b-instruct":   "Qwen3 Coder 30B",
	"qwen3-coder-480b-a35b-instruct": "Qwen3 Coder 480B",
	"qwen3-coder-flash":              "Qwen3 Coder Flash",
	"qwen3-coder-plus":               "Qwen3 Coder Plus",
	"qwen3-coder-plus-2025-07-22":    "Qwen3 Coder Plus",

	// Other
	"kat-dev":                    "Kat Dev",
	"jina-deepsearch-v1":         "Jina DeepSearch V1",
	"mimo-v2-flash-free":         "Mimo V2 Flash (Free)",
	"gpt-oss-120b":               "GPT OSS 120B",
	"gpt-oss-20b":                "GPT OSS 20B",
	"gpt-4o-audio-preview":       "GPT-4o Audio Preview",
	"gpt-4o-search-preview":      "GPT-4o Search",
	"gpt-4o-mini-search-preview": "GPT-4o Mini Search",
	"gpt-4o-2024-11-20":          "GPT-4o",
	"gpt-4o":                     "GPT-4o",
	"gpt-4o-mini":                "GPT-4o Mini",
	"coding-glm-4.6-free":        "Coding GLM 4.6 (Free)",
	"coding-minimax-m2.1":        "Coding MiniMax M2.1",
	"coding-minimax-m2":          "Coding MiniMax M2",
	"coding-minimax-m2-free":     "Coding MiniMax M2 (Free)",

	// OpenRouter-specific mappings (provider/model format)
	"anthropic/claude-sonnet-4":          "Claude Sonnet 4",
	"anthropic/claude-sonnet-4.5":        "Claude Sonnet 4.5",
	"anthropic/claude-3-opus":            "Claude 3 Opus",
	"anthropic/claude-3.5-haiku":         "Claude 3.5 Haiku",
	"anthropic/claude-3-haiku":           "Claude 3 Haiku",
	"openai/gpt-5.2":                     "GPT-5.2",
	"openai/gpt-5.2-codex":               "GPT-5.2 Codex",
	"openai/gpt-5":                       "GPT-5",
	"openai/gpt-4-turbo":                 "GPT-4 Turbo",
	"openai/gpt-4-turbo-preview":         "GPT-4 Turbo Preview",
	"openai/gpt-3.5-turbo":               "GPT-3.5 Turbo",
	"google/gemini-pro-1.5":              "Gemini 1.5 Pro",
	"google/gemini-flash-1.5":            "Gemini 1.5 Flash",
	"meta-llama/llama-3.3-70b-instruct":  "Llama 3.3 70B",
	"meta-llama/llama-3.2-3b-instruct":   "Llama 3.2 3B",
	"meta-llama/llama-3.1-405b-instruct": "Llama 3.1 405B",
	"mistralai/mistral-large":            "Mistral Large",
	"mistralai/mistral-medium":           "Mistral Medium",
	"mistralai/mistral-small":            "Mistral Small",
	"qwen/qwen-2.5-72b-instruct":         "Qwen 2.5 72B",
}

// GetDisplayName returns a human-readable display name for the given model ID.
func GetDisplayName(modelID string) string {
	if name := lookupInMappings(modelID); name != "" {
		return name
	}

	if bestMatch := findBestMatch(strings.ToLower(modelID)); bestMatch != "" {
		return bestMatch
	}

	return formatModelName(modelID)
}

// lookupInMappings attempts to find the model ID in the static mappings,
// checking both the original string and without provider prefix.
func lookupInMappings(modelID string) string {
	if name, ok := modelNames[modelID]; ok {
		return name
	}

	lowered := strings.ToLower(modelID)
	if name, ok := modelNames[lowered]; ok {
		return name
	}

	if idx := strings.LastIndex(modelID, "/"); idx != -1 {
		baseModel := modelID[idx+1:]
		if name, ok := modelNames[baseModel]; ok {
			return name
		}
		if name, ok := modelNames[strings.ToLower(baseModel)]; ok {
			return name
		}
	}

	return ""
}

// formatModelName converts a technical model ID to a more readable format.
func formatModelName(modelID string) string {
	result := modelID

	// Remove provider prefix if present
	if idx := strings.LastIndex(result, "/"); idx != -1 {
		result = result[idx+1:]
	}

	// Replace underscores and slashes with spaces
	result = strings.ReplaceAll(strings.ReplaceAll(result, "_", " "), "/", " ")

	// Convert version patterns like "3-5" to "3.5"
	versionDashRegex := regexp.MustCompile(`(\d)-(\d)`)
	result = versionDashRegex.ReplaceAllString(result, "$1.$2")

	// Replace remaining dashes with spaces and clean up
	result = strings.ReplaceAll(result, "-", " ")
	result = strings.Join(strings.Fields(result), " ")

	// Capitalize each word while preserving version indicators
	words := strings.Fields(result)
	var builder strings.Builder
	for i, word := range words {
		if i > 0 {
			builder.WriteString(" ")
		}
		builder.WriteString(capitalizeWord(word))
	}

	return builder.String()
}

// capitalizeWord capitalizes the first letter of a word, preserving version patterns.
func capitalizeWord(word string) string {
	if len(word) == 0 {
		return word
	}

	// Preserve "V" prefix for version numbers (e.g., "V3" -> "v3" becomes "V3")
	upperWord := strings.ToUpper(word)
	if strings.HasPrefix(upperWord, "V") && len(word) > 1 {
		return strings.ToUpper(word[0:1]) + word[1:]
	}

	return strings.ToUpper(word[0:1]) + word[1:]
}

const fuzzyMatchThreshold = 2 // Maximum edit distance to consider for fuzzy matching

// findBestMatch uses Levenshtein distance to find the best matching model name.
func findBestMatch(modelID string) string {
	var bestMatch string
	minDistance := fuzzyMatchThreshold + 1

	for knownID, name := range modelNames {
		distance := levenshteinDistance(modelID, knownID)
		if distance < minDistance {
			minDistance = distance
			bestMatch = name
		}
	}

	return bestMatch
}

// levenshteinDistance computes the edit distance between two strings.
func levenshteinDistance(a, b string) int {
	switch {
	case len(a) == 0:
		return len(b)
	case len(b) == 0:
		return len(a)
	}

	previous := make([]int, len(b)+1)
	for j := range previous {
		previous[j] = j
	}

	for i := 1; i <= len(a); i++ {
		current := make([]int, len(b)+1)
		current[0] = i

		for j := 1; j <= len(b); j++ {
			cost := 0
			if a[i-1] != b[j-1] {
				cost = 1
			}

			deletion := previous[j] + 1
			insertion := current[j-1] + 1
			substitution := previous[j-1] + cost

			current[j] = min(deletion, min(insertion, substitution))
		}

		previous = current
	}

	return previous[len(b)]
}
