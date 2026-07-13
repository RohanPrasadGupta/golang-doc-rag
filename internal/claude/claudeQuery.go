package claude

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

var (
	clientOnce sync.Once
	client     anthropic.Client
	clientErr  error
)

func getClient() (anthropic.Client, error) {
	clientOnce.Do(func() {
		anthropicKey := os.Getenv("ANTHROPIC_API_KEY")
		if anthropicKey == "" {
			clientErr = errors.New("ANTHROPIC_API_KEY is not set")
			return
		}
		client = anthropic.NewClient(
			option.WithAPIKey(anthropicKey),
		)
	})
	return client, clientErr
}

func Query(ctx context.Context, query string, matchesText string) (string, error) {
	c, err := getClient()
	if err != nil {
		return "", err
	}

	message, err := c.Messages.New(ctx, anthropic.MessageNewParams{
		MaxTokens: 1024,
		System: []anthropic.TextBlockParam{
			{Text: `You are a document question-answering assistant. You must answer ONLY using the provided context below, which was retrieved from the user's uploaded documents.

			Rules:
			- If the context does not contain the information needed to answer the question, respond exactly: "I couldn't find anything about that in the uploaded documents."
			- Do NOT use any outside knowledge. Do NOT answer general questions, write code, or provide information that is not present in the context, even if you know the answer.
			- Only answer based on what is explicitly in the context.`},
		},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(query + "\n\n" + "Here are the matched chunks of text: " + matchesText)),
		},
		Model: "claude-haiku-4-5",
	})
	if err != nil {
		return "", fmt.Errorf("failed to query: %w", err)
	}

	return textFromMessage(message), nil
}

func QueryResumeExtraction(ctx context.Context, content string) (string, error) {
	c, err := getClient()
	if err != nil {
		return "", err
	}

	message, err := c.Messages.New(ctx, anthropic.MessageNewParams{
		MaxTokens: 4096,
		System: []anthropic.TextBlockParam{
			{Text: ExtractionSystem},
		},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(content)),
		},
		Model: "claude-haiku-4-5",
	})
	if err != nil {
		return "", fmt.Errorf("failed to query: %w", err)
	}

	answer := textFromMessage(message)
	if strings.TrimSpace(answer) == "" {
		return "", fmt.Errorf("Claude returned no text content")
	}

	return cleanJSONResponse(answer), nil
}

func cleanJSONResponse(input string) string {
	input = strings.TrimSpace(input)

	input = strings.TrimPrefix(input, "```json")
	input = strings.TrimPrefix(input, "```JSON")
	input = strings.TrimPrefix(input, "```")

	input = strings.TrimSuffix(input, "```")

	return strings.TrimSpace(input)
}

func QueryJDExtraction(ctx context.Context, content string) (string, error) {
	c, err := getClient()
	if err != nil {
		return "", err
	}

	message, err := c.Messages.New(ctx, anthropic.MessageNewParams{
		MaxTokens: 4096,
		System: []anthropic.TextBlockParam{
			{Text: JDExtractionSystem},
		},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(content)),
		},
		Model: "claude-haiku-4-5",
	})
	if err != nil {
		return "", fmt.Errorf("failed to query: %w", err)
	}

	return textFromMessage(message), nil
}

func QueryJDScoring(ctx context.Context, userInformationJSON string, jd string) (string, error) {
	c, err := getClient()
	if err != nil {
		return "", err
	}

	message, err := c.Messages.New(ctx, anthropic.MessageNewParams{
		MaxTokens: 4096,
		System: []anthropic.TextBlockParam{
			{Text: JDScoring},
		},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(userInformationJSON + "\n\n" + jd)),
		},
		Model: "claude-haiku-4-5",
	})
	if err != nil {
		return "", fmt.Errorf("failed to query: %w", err)
	}

	return textFromMessage(message), nil
}

func QueryJOBCoverLetter(ctx context.Context, userInformationJSON string, jd string) (string, error) {
	c, err := getClient()
	if err != nil {
		return "", err
	}

	message, err := c.Messages.New(ctx, anthropic.MessageNewParams{
		MaxTokens: 4096,
		System: []anthropic.TextBlockParam{
			{Text: JOBCoverLetterSystem},
		},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(userInformationJSON + "\n\n" + jd)),
		},
		Model: "claude-haiku-4-5",
	})
	if err != nil {
		return "", fmt.Errorf("failed to query: %w", err)
	}

	return textFromMessage(message), nil
}

func QueryNewResume(
	ctx context.Context,
	userInformationJSON string,
	userUpdatesJSON string,
	jd string,
) (string, error) {
	c, err := getClient()
	if err != nil {
		return "", err
	}

	userPrompt := fmt.Sprintf(`
USER_INFORMATION:
%s

USER_UPDATES:
%s

JOB_DESCRIPTION:
%s

Generate the updated ATS-friendly resume in LaTeX.
`, userInformationJSON, userUpdatesJSON, jd)

	message, err := c.Messages.New(ctx, anthropic.MessageNewParams{
		MaxTokens: 4096,
		System: []anthropic.TextBlockParam{
			{Text: NEWResumeLatexBuilder},
		},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(
				anthropic.NewTextBlock(userPrompt),
			),
		},
		Model: "claude-haiku-4-5",
	})
	if err != nil {
		return "", fmt.Errorf("failed to query: %w", err)
	}

	return textFromMessage(message), nil
}

func textFromMessage(message *anthropic.Message) string {
	var answer string
	for _, block := range message.Content {
		if textBlock, ok := block.AsAny().(anthropic.TextBlock); ok {
			answer += textBlock.Text
		}
	}
	return answer
}
