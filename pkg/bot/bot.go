package bot

import (
	"context"
	"fmt"
	"os"
	"time"

	openai "github.com/sashabaranov/go-openai"
	log "github.com/sirupsen/logrus"
	tele "gopkg.in/telebot.v3"
)

const defaultTimeout = 10 * time.Second

type ChatGPT struct {
	client            *openai.Client
	openAIMessagesMap OpenAIMessagesMap
	validChatID       []int64
}

func NewChatGPT(key string, validChatID []int64) *ChatGPT {
	messageMap := make(OpenAIMessagesMap)
	return &ChatGPT{
		client:            openai.NewClient(key),
		openAIMessagesMap: messageMap,
		validChatID:       validChatID,
	}
}

func (g *ChatGPT) isValidChatID(chatID int64) bool {
	for _, id := range g.validChatID {
		if id == chatID {
			return true
		}
	}
	return false
}

func (g *ChatGPT) complete(ctx context.Context, messages OpenAIMessages) (OpenAIMessages, error) {
	request := openai.ChatCompletionRequest{
		Model:    openai.GPT3Dot5Turbo,
		Messages: messages,
	}

	resp, err := g.client.CreateChatCompletion(ctx, request)
	if err != nil {
		return nil, err
	}

	messages = append(messages, resp.Choices[0].Message)
	return messages, nil
}

func (g *ChatGPT) chat(c tele.Context) error {
	message := c.Message()

	isReply := message.IsReply()
	log.Infof("isReply: %t", isReply)

	// If chatIDs is not empty, then we only accept messages from those chatIDs
	chatID := message.Chat.ID
	if len(g.validChatID) != 0 && !g.isValidChatID(chatID) {
		return c.Reply(fmt.Sprintf("Sorry, I'm not allowed to talk to you :(. Add your chat ID: %d to the VALID_CHAT_ID env var.", chatID))
	}

	// If message is not a reply and the payload is empty, then we ignore the message
	if !isReply && message.Payload == "" {
		return nil
	}

	// If message is a reply, then we need to append the reply to the previous messages
	openAIMessages := OpenAIMessages{}
	if isReply {
		previousOpenAIMessages, ok := g.openAIMessagesMap[message.ReplyTo.ID]
		if !ok {
			previousMessage := openai.ChatCompletionMessage{
				Role:    openai.ChatMessageRoleAssistant,
				Content: message.ReplyTo.Text,
			}
			openAIMessages = append(openAIMessages, previousMessage)
		}
		openAIMessages = append(openAIMessages, previousOpenAIMessages...)
	}

	content := message.Payload
	if isReply {
		content = message.Text
	}
	log.Infof("user content: %s", content)

	if content == "" {
		log.Infof("empty content, ignoring")
		return nil
	}

	openAIMessages = append(openAIMessages, openai.ChatCompletionMessage{
		Role:    openai.ChatMessageRoleUser,
		Content: content,
	})

	openAIMessages, err := g.complete(context.Background(), openAIMessages)
	if err != nil {
		return err
	}

	replyMessage, err := c.Bot().Reply(message, openAIMessages.LastContent(), &tele.SendOptions{
		ParseMode: "Markdown",
	})
	if err != nil {
		return err
	}
	g.openAIMessagesMap[replyMessage.ID] = openAIMessages
	return nil
}

func Execute() {
	botToken := os.Getenv("TELEGRAM_BOT_TOKEN")
	openaiAPIKey := os.Getenv("OPENAI_API_KEY")
	validChatID, err := parseInt64(os.Getenv("VALID_CHAT_ID"))
	if err != nil {
		log.Fatalf("failed to parse VALID_CHAT_ID: %+v", err)
		return
	}

	pref := tele.Settings{
		Token:  botToken,
		Poller: &tele.LongPoller{Timeout: defaultTimeout},
	}

	bot, err := tele.NewBot(pref)
	if err != nil {
		log.Fatal(err)
		return
	}

	chatGPT := NewChatGPT(openaiAPIKey, validChatID)

	bot.Handle("/gpt", chatGPT.chat)
	bot.Handle(tele.OnText, chatGPT.chat)

	log.Infof("Starting bot")
	bot.Start()
}
