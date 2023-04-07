package main

// bot.go

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/meinside/geektoken"
	"github.com/meinside/openai-go"
	tg "github.com/meinside/telegram-bot-go"
)

const (
	chatCompletionModelDefault = "gpt-3.5-turbo"
)

const (
	intervalSeconds = 1

	cmdStart = "/start"
	cmdCount = "/count"

	msgStart           = "This bot will answer your messages with ChatGPT API :-)"
	msgCmdNotSupported = "Not a supported bot command: %s"
	msgTokenCount      = "%d tokens in %d chars (cl100k_base)"
)

// config struct for loading a configuration file
type config struct {
	// telegram bot api
	TelegramBotToken string `json:"telegram_bot_token"`

	// openai api
	OpenAIAPIKey         string `json:"openai_api_key"`
	OpenAIOrganizationID string `json:"openai_org_id"`
	OpenAIModel          string `json:"openai_model,omitempty"`

	// other configurations
	AllowedTelegramUsers []string `json:"allowed_telegram_users"`
	Verbose              bool     `json:"verbose,omitempty"`
}

// load config at given path
func loadConfig(fpath string) (conf config, err error) {
	var bytes []byte
	if bytes, err = os.ReadFile(fpath); err == nil {
		if err = json.Unmarshal(bytes, &conf); err == nil {
			return conf, nil
		}
	}

	return config{}, err
}

// launch bot with given parameters
func runBot(conf config) {
	token := conf.TelegramBotToken
	apiKey := conf.OpenAIAPIKey
	orgID := conf.OpenAIOrganizationID

	allowedUsers := map[string]bool{}
	for _, user := range conf.AllowedTelegramUsers {
		allowedUsers[user] = true
	}

	bot := tg.NewClient(token)
	client := openai.NewClient(apiKey, orgID)

	// set verbosity
	client.Verbose = conf.Verbose

	_ = bot.DeleteWebhook(false) // delete webhook before polling updates
	if b := bot.GetMe(); b.Ok {
		log.Printf("launching bot: %s", userName(b.Result))

		// poll updates
		bot.StartMonitoringUpdates(0, intervalSeconds, func(b *tg.Bot, update tg.Update, err error) {
			if isAllowed(update, allowedUsers) {
				handleUpdate(bot, client, conf, update)
			} else {
				log.Printf("not allowed: %s", userNameFromUpdate(&update))
			}
		})
	} else {
		log.Printf("failed to get bot info: %s", *b.Description)
	}
}

// checks if given update is allowed or not
func isAllowed(update tg.Update, allowedUsers map[string]bool) bool {
	var username string
	if update.HasMessage() && update.Message.From.Username != nil {
		username = *update.Message.From.Username
	} else if update.HasEditedMessage() && update.EditedMessage.From.Username != nil {
		username = *update.EditedMessage.From.Username
	}

	if _, exists := allowedUsers[username]; exists {
		return true
	}

	return false
}

// handle allowed update from telegram bot api
func handleUpdate(bot *tg.Bot, client *openai.Client, conf config, update tg.Update) {
	message := usableMessageFromUpdate(update)
	chatID := message.Chat.ID
	userID := message.From.ID
	messageID := message.MessageID

	if message.HasText() && strings.HasPrefix(*message.Text, "/") {
		cmd := *message.Text
		switch cmd {
		case cmdStart:
			send(bot, conf, msgStart, chatID, nil)
		// TODO: process more bot commands here
		default:
			var msg string
			if strings.HasPrefix(cmd, cmdCount) {
				txtToCount := strings.TrimSpace(strings.Replace(cmd, cmdCount, "", 1))
				if count, err := countTokens(txtToCount); err == nil {
					msg = fmt.Sprintf(msgTokenCount, count, len(txtToCount))
				} else {
					msg = err.Error()
				}
			} else {
				msg = fmt.Sprintf(msgCmdNotSupported, cmd)
			}
			send(bot, conf, msg, chatID, &messageID)
			return
		}
	} else {
		messages := chatMessagesFromUpdate(bot, update)
		if len(messages) > 0 {
			answer(bot, client, conf, messages, chatID, userID, messageID)
		} else {
			log.Printf("No converted chat messages from update: %+v", update)

			msg := fmt.Sprintf("Failed to get usable chat messages from your input. See the server logs for more information.")
			send(bot, conf, msg, chatID, &messageID)
		}
	}
}

// get usable message from given update
func usableMessageFromUpdate(update tg.Update) (message *tg.Message) {
	if update.HasMessage() && update.Message.HasText() {
		message = update.Message
	} else if update.HasMessage() && update.Message.HasDocument() {
		message = update.Message
	} else if update.HasEditedMessage() && update.EditedMessage.HasText() {
		message = update.EditedMessage
	}

	return message
}

// convert telegram bot update into openai chat messages
func chatMessagesFromUpdate(bot *tg.Bot, update tg.Update) (chatMessages []openai.ChatMessage) {
	chatMessages = []openai.ChatMessage{}

	var message *tg.Message
	if update.HasMessage() {
		message = update.Message
	} else if update.HasEditedMessage() {
		message = update.EditedMessage
	}
	replyTo := repliedToMessage(message)

	// chat message 1
	if replyTo != nil {
		if chatMessage := convertMessage(bot, replyTo); chatMessage != nil {
			chatMessages = append(chatMessages, *chatMessage)
		}
	}

	// chat message 2
	if chatMessage := convertMessage(bot, message); chatMessage != nil {
		chatMessages = append(chatMessages, *chatMessage)
	}

	return chatMessages
}

// send given message to the chat
func send(bot *tg.Bot, conf config, message string, chatID int64, messageID *int64) {
	_ = bot.SendChatAction(chatID, tg.ChatActionTyping, nil)

	if conf.Verbose {
		log.Printf("[verbose] sending message to chat(%d): '%s'", chatID, message)
	}

	options := tg.OptionsSendMessage{}
	if messageID != nil {
		options.SetReplyToMessageID(*messageID)
	}
	if res := bot.SendMessage(chatID, message, options); !res.Ok {
		log.Printf("failed to send message: %s", *res.Description)
	}
}

// generate an answer to given message and send it to the chat
func answer(bot *tg.Bot, client *openai.Client, conf config, messages []openai.ChatMessage, chatID, userID, messageID int64) {
	_ = bot.SendChatAction(chatID, tg.ChatActionTyping, nil)

	model := conf.OpenAIModel
	if model == "" {
		model = chatCompletionModelDefault
	}

	if response, err := client.CreateChatCompletion(model,
		messages,
		openai.ChatCompletionOptions{}.
			SetUser(userAgent(userID))); err == nil {
		if conf.Verbose {
			log.Printf("[verbose] %+v ===> %+v", messages, response.Choices)
		}

		_ = bot.SendChatAction(chatID, tg.ChatActionTyping, nil)

		var answer string
		if len(response.Choices) > 0 {
			answer = response.Choices[0].Message.Content
		} else {
			answer = "There was no response from OpenAI API."
		}

		if conf.Verbose {
			log.Printf("[verbose] sending answer to chat(%d): '%s'", chatID, answer)
		}

		// if answer is too long for telegram api, send it as a text document
		if len(answer) > 4096 {
			file := tg.InputFileFromBytes([]byte(answer))
			if res := bot.SendDocument(
				chatID,
				file,
				tg.OptionsSendDocument{}.
					SetReplyToMessageID(messageID).
					SetCaption(strings.ToValidUTF8(answer[:128], "")+"...")); !res.Ok {
				log.Printf("failed to answer messages '%+v' with '%s' as file: %s", messages, answer, err)

				msg := fmt.Sprintf("Failed to send you the answer as a text file. See the server logs for more information.")
				send(bot, conf, msg, chatID, &messageID)
			}
		} else {
			if res := bot.SendMessage(
				chatID,
				answer,
				tg.OptionsSendMessage{}.
					SetReplyToMessageID(messageID)); !res.Ok {
				log.Printf("failed to answer messages '%+v' with '%s': %s", messages, answer, err)

				msg := fmt.Sprintf("Failed to send you the answer as a text. See the server logs for more information.")
				send(bot, conf, msg, chatID, &messageID)
			}
		}
	} else {
		log.Printf("failed to create chat completion: %s", err)

		msg := fmt.Sprintf("Failed to generate an answer from OpenAI. See the server logs for more information.")
		send(bot, conf, msg, chatID, &messageID)
	}
}

// generate a user-agent value
func userAgent(userID int64) string {
	return fmt.Sprintf("telegram-chatgpt-bot:%d", userID)
}

// generate user's name
func userName(user *tg.User) string {
	if user.Username != nil {
		return fmt.Sprintf("@%s (%s)", *user.Username, user.FirstName)
	} else {
		return user.FirstName
	}
}

// generate user's name from update
func userNameFromUpdate(update *tg.Update) string {
	var user *tg.User
	if update.HasMessage() {
		user = update.Message.From
	} else if update.HasEditedMessage() {
		user = update.EditedMessage.From
	}

	return userName(user)
}

// get original message which was replied by given `message`
func repliedToMessage(message *tg.Message) *tg.Message {
	if message.ReplyToMessage != nil {
		return message.ReplyToMessage
	}

	return nil
}

// convert given telegram bot message to an openai chat message,
// nil if there was any error.
//
// (if it was sent from bot, make it an assistant's message)
func convertMessage(bot *tg.Bot, message *tg.Message) *openai.ChatMessage {
	if message.ViaBot != nil &&
		message.ViaBot.IsBot {
		if message.HasText() {
			chatMessage := openai.NewChatAssistantMessage(*message.Text)
			return &chatMessage
		} else if message.HasDocument() {
			if bytes, err := documentText(bot, message.Document); err == nil {
				str := strings.TrimSpace(strings.ToValidUTF8(string(bytes), "?"))
				chatMessage := openai.NewChatAssistantMessage(str)
				return &chatMessage
			} else {
				log.Printf("failed to read document content for assistant message: %s", err)
			}
		}
	}

	if message.HasText() {
		chatMessage := openai.NewChatUserMessage(*message.Text)
		return &chatMessage
	} else if message.HasDocument() {
		if bytes, err := documentText(bot, message.Document); err == nil {
			str := strings.TrimSpace(strings.ToValidUTF8(string(bytes), "?"))
			chatMessage := openai.NewChatUserMessage(str)
			return &chatMessage
		} else {
			log.Printf("failed to read document content for user message: %s", err)
		}
	}

	return nil
}

// read bytes from given document
func documentText(bot *tg.Bot, document *tg.Document) (result []byte, err error) {
	if res := bot.GetFile(document.FileID); !res.Ok {
		err = fmt.Errorf("Failed to get document: %s", *res.Description)
	} else {
		fileURL := bot.GetFileURL(*res.Result)
		result, err = readFileContentAtURL(fileURL)
	}

	return result, err
}

var _tokenizer *geektoken.Tokenizer = nil

// count BPE tokens for given `text`
func countTokens(text string) (result int, err error) {
	result = 0

	// lazy-load the tokenizer
	if _tokenizer == nil {
		var tokenizer geektoken.Tokenizer
		tokenizer, err = geektoken.GetTokenizerWithEncoding(geektoken.EncodingCl100kBase)

		if err == nil {
			_tokenizer = &tokenizer
		}
	}

	if _tokenizer == nil {
		return 0, fmt.Errorf("tokenizer is not initialized.")
	}

	tokens := []int{}
	tokens, err = _tokenizer.Encode(text, nil, nil)

	if err == nil {
		return len(tokens), nil
	}

	return result, err
}

// read file content at given url, will timeout in 60 seconds
func readFileContentAtURL(url string) (content []byte, err error) {
	httpClient := http.Client{
		Timeout: time.Second * 60,
	}

	var resp *http.Response
	resp, err = httpClient.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	content, err = ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return content, nil
}
