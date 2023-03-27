package main

// bot.go

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/meinside/openai-go"
	tg "github.com/meinside/telegram-bot-go"
)

const (
	chatCompletionModelDefault = "gpt-3.5-turbo"
)

const (
	intervalSeconds = 1

	cmdStart           = "/start"
	msgStart           = "This bot will answer your messages with ChatGPT API :-)"
	msgCmdNotSupported = "Not a supported bot command: %s"
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

	if b := bot.GetMe(); b.Ok {
		log.Printf("launching bot: %s", userName(b.Result))

		bot.StartMonitoringUpdates(0, intervalSeconds, func(b *tg.Bot, update tg.Update, err error) {
			if isAllowed(update, allowedUsers) {
				var message, replyTo *tg.Message

				if update.HasMessage() && update.Message.HasText() {
					message = update.Message
				} else if update.HasEditedMessage() && update.EditedMessage.HasText() {
					message = update.EditedMessage
				}
				replyTo = repliedToMessage(message)

				chatID := message.Chat.ID
				userID := message.From.ID
				txt := *message.Text

				if !strings.HasPrefix(txt, "/") {
					// classify message
					if reason, flagged := isFlagged(client, txt); flagged {
						send(bot, conf, fmt.Sprintf("Could not handle message: %s.", reason), chatID)
					} else {
						messageID := message.MessageID

						// chat messages for generation
						messages := []openai.ChatMessage{}
						if replyTo != nil {
							messages = append(messages, convertMessage(replyTo))
						}
						messages = append(messages, convertMessage(message))

						answer(bot, client, conf, messages, chatID, userID, messageID)
					}
				} else {
					switch txt {
					case cmdStart:
						send(bot, conf, msgStart, chatID)
					// TODO: process more bot commands here
					default:
						send(bot, conf, fmt.Sprintf(msgCmdNotSupported, txt), chatID)
					}
				}
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

// send given message to the chat
func send(bot *tg.Bot, conf config, message string, chatID int64) {
	_ = bot.SendChatAction(chatID, tg.ChatActionTyping, nil)

	if conf.Verbose {
		log.Printf("[verbose] sending message to chat(%d): '%s'", chatID, message)
	}

	if res := bot.SendMessage(chatID, message, tg.OptionsSendMessage{}); !res.Ok {
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
			answer = "No response from API."
		}

		if conf.Verbose {
			log.Printf("[verbose] sending answer to chat(%d): '%s'", chatID, answer)
		}

		if res := bot.SendMessage(
			chatID,
			answer,
			tg.OptionsSendMessage{}.
				SetReplyToMessageID(messageID)); !res.Ok {
			log.Printf("failed to answer messages '%+v' with '%s': %s", messages, answer, err)
		}
	} else {
		log.Printf("failed to create chat completion: %s", err)
	}
}

// check if given message is flagged or not
func isFlagged(client *openai.Client, message string) (output string, flagged bool) {
	if response, err := client.CreateModeration(message, openai.ModerationOptions{}); err == nil {
		for _, classification := range response.Results {
			if classification.Flagged {
				categories := []string{}

				for k, v := range classification.Categories {
					if v {
						categories = append(categories, k)
					}
				}

				return fmt.Sprintf("'%s' was flagged due to following reason(s): %s", message, strings.Join(categories, ", ")), true
			}
		}

		return "", false
	} else {
		return fmt.Sprintf("failed to classify message: '%s' with error: %s", message, err), true
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

// convert telegram bot message to openai chat message
// (if it was sent from bot, make it an assistant's message)
func convertMessage(message *tg.Message) openai.ChatMessage {
	if message.ViaBot != nil &&
		message.ViaBot.IsBot {
		return openai.NewChatAssistantMessage(*message.Text)
	}
	return openai.NewChatUserMessage(*message.Text)
}
