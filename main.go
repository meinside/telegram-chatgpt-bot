package main

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
	chatCompletionModel = "gpt-3.5-turbo"
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

	// other configurations
	AllowedTelegramUsers []string `json:"allowed_telegram_users"`
	Verbose              bool     `json:"verbose,omitempty"`
}

func main() {
	if len(os.Args) <= 1 {
		printUsage()
	} else {
		confFilepath := os.Args[1]

		if conf, err := loadConfig(confFilepath); err == nil {
			runBot(conf)
		} else {
			log.Printf("failed to load config: %s", err)
		}
	}
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

	if b := bot.GetMe(); b.Ok {
		log.Printf("launching bot: %s", userName(b.Result))

		bot.StartMonitoringUpdates(0, intervalSeconds, func(b *tg.Bot, update tg.Update, err error) {
			if isAllowed(update, allowedUsers) {
				if update.HasMessage() && update.Message.HasText() {
					chatID := update.Message.Chat.ID
					userID := update.Message.From.ID
					message := *update.Message.Text

					if !strings.HasPrefix(message, "/") {
						// classify message
						if reason, flagged := isFlagged(client, message); flagged {
							send(bot, conf, fmt.Sprintf("Could not handle message: %s.", reason), chatID)
						} else {
							answer(bot, client, conf, message, chatID, userID)
						}
					} else {
						switch message {
						case cmdStart:
							send(bot, conf, msgStart, chatID)
						// TODO: process more bot commands here
						default:
							send(bot, conf, fmt.Sprintf(msgCmdNotSupported, message), chatID)
						}
					}
				}
			} else {
				log.Printf("not allowed: %s", userName(update.Message.From))
			}
		})
	} else {
		log.Printf("failed to get bot info: %s", *b.Description)
	}
}

// checks if given update is allowed or not
func isAllowed(update tg.Update, allowedUsers map[string]bool) bool {
	if update.Message.From.Username != nil {
		if _, exists := allowedUsers[*update.Message.From.Username]; exists {
			return true
		}
	}

	return false
}

// send given message to the chat
func send(bot *tg.Bot, conf config, message string, chatID int64) {
	bot.SendChatAction(chatID, tg.ChatActionTyping, nil)

	if conf.Verbose {
		log.Printf("[verbose] sending message to chat(%d): '%s'", chatID, message)
	}

	if res := bot.SendMessage(chatID, message, tg.OptionsSendMessage{}); !res.Ok {
		log.Printf("failed to send message: %s", *res.Description)
	}
}

// generate an answer to given message and send it to the chat
func answer(bot *tg.Bot, client *openai.Client, conf config, message string, chatID, userID int64) {
	bot.SendChatAction(chatID, tg.ChatActionTyping, nil)

	if response, err := client.CreateChatCompletion(chatCompletionModel,
		[]openai.ChatMessage{
			openai.NewChatUserMessage(message),
		},
		openai.ChatCompletionOptions{}.
			SetUser(userAgent(userID))); err == nil {
		if conf.Verbose {
			log.Printf("[verbose] %s ===> %+v", message, response.Choices)
		}

		var answer string
		if len(response.Choices) > 0 {
			answer = response.Choices[0].Message.Content
		} else {
			answer = "No response from API."
		}

		if conf.Verbose {
			log.Printf("[verbose] sending answer to chat(%d): '%s'", chatID, answer)
		}

		if res := bot.SendMessage(chatID, answer, tg.OptionsSendMessage{}); !res.Ok {
			log.Printf("failed to answer message '%s' with '%s': %s", message, answer, err)
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

// print usage string
func printUsage() {
	fmt.Printf(`
Usage: %s [config_filepath]
`, os.Args[0])
}
