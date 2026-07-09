package claude

import (
	"context"
	"errors"
	"os"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

func Query(ctx context.Context, query string, matchesText string) (string, error) {

	anthropicKey := os.Getenv("ANTHROPIC_API_KEY")
	if anthropicKey == "" {
		return "", errors.New("ANTHROPIC_API_KEY is not set")
	}

	client := anthropic.NewClient(
		option.WithAPIKey(anthropicKey),
	)

	message, err := client.Messages.New(context.TODO(), anthropic.MessageNewParams{
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
		panic(err.Error())
	}
	var answer string
	for _, block := range message.Content {
		if textBlock, ok := block.AsAny().(anthropic.TextBlock); ok {
			answer += textBlock.Text
		}
	}
	return answer, nil
}
