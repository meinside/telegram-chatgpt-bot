package main

// bot.go

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/meinside/geektoken"
	"github.com/meinside/openai-go"
	tg "github.com/meinside/telegram-bot-go"
	"github.com/meinside/version-go"
)

const (
	chatCompletionModelDefault = "gpt-3.5-turbo"
)

const (
	intervalSeconds = 1

	cmdStart = "/start"
	cmdCount = "/count"
	cmdStats = "/stats"
	cmdHelp  = "/help"

	msgStart                 = "This bot will answer your messages with ChatGPT API :-)"
	msgCmdNotSupported       = "Not a supported bot command: %s"
	msgTypeNotSupported      = "Not a supported message type."
	msgDatabaseNotConfigured = "Database not configured. Set `db_filepath` in your config file."
	msgDatabaseEmpty         = "Database is empty."
	msgTokenCount            = "<b>%d</b> tokens in <b>%d</b> chars <i>(cl100k_base)</i>"
	msgHelp                  = `Help message here:

/count [some_text] : count the number of tokens in a given text.
/stats : show stats of this bot.
/help : show this help message.

<i>version: %s</i>
`
)

// config struct for loading a configuration file
type config struct {
	// telegram bot api
	TelegramBotToken string `json:"telegram_bot_token"`

	// openai api
	OpenAIAPIKey         string `json:"openai_api_key"`
	OpenAIOrganizationID string `json:"openai_org_id"`
	OpenAIModel          string `json:"openai_model,omitempty"`

	// database logging
	RequestLogsDBFilepath string `json:"db_filepath,omitempty"`

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

		var db *Database = nil
		if conf.RequestLogsDBFilepath != "" {
			var err error
			if db, err = OpenDatabase(conf.RequestLogsDBFilepath); err != nil {
				log.Printf("failed to open request logs db: %s", err)
			}
		}

		// set message handler
		bot.SetMessageHandler(func(b *tg.Bot, update tg.Update, message tg.Message, edited bool) {
			if !isAllowed(update, allowedUsers) {
				log.Printf("message not allowed: %s", userNameFromUpdate(update))
				return
			}

			handleMessage(b, client, conf, db, update, message)
		})

		// set command handlers
		bot.AddCommandHandler(cmdStart, startCommandHandler(conf, allowedUsers))
		bot.AddCommandHandler(cmdStats, statsCommandHandler(conf, db, allowedUsers))
		bot.AddCommandHandler(cmdHelp, helpCommandHandler(conf, allowedUsers))
		bot.AddCommandHandler(cmdCount, countCommandHandler(conf, allowedUsers))
		bot.SetNoMatchingCommandHandler(noSuchCommandHandler(conf, allowedUsers))

		// poll updates
		bot.StartPollingUpdates(0, intervalSeconds, func(b *tg.Bot, update tg.Update, err error) {
			if err == nil {
				if !isAllowed(update, allowedUsers) {
					log.Printf("not allowed: %s", userNameFromUpdate(update))
					return
				}

				// type not supported
				message := usableMessageFromUpdate(update)
				if message != nil {
					send(b, conf, msgTypeNotSupported, message.Chat.ID, &message.MessageID)
				}
			} else {
				log.Printf("failed to poll updates: %s", err)
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

// handle allowed message update from telegram bot api
func handleMessage(bot *tg.Bot, client *openai.Client, conf config, db *Database, update tg.Update, message tg.Message) {
	chatID := message.Chat.ID
	userID := message.From.ID
	messageID := message.MessageID

	messages := chatMessagesFromTGMessage(bot, message)
	if len(messages) > 0 {
		answer(bot, client, conf, db, messages, chatID, userID, userNameFromUpdate(update), messageID)
	} else {
		log.Printf("no converted chat messages from update: %+v", update)

		msg := "Failed to get usable chat messages from your input. See the server logs for more information."
		send(bot, conf, msg, chatID, &messageID)
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

// convert telegram bot message into openai chat messages
func chatMessagesFromTGMessage(bot *tg.Bot, message tg.Message) (chatMessages []openai.ChatMessage) {
	chatMessages = []openai.ChatMessage{}

	replyTo := repliedToMessage(message)

	// chat message 1
	if replyTo != nil {
		if chatMessage := convertMessage(bot, *replyTo); chatMessage != nil {
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

	options := tg.OptionsSendMessage{}.
		SetParseMode(tg.ParseModeHTML)
	if messageID != nil {
		options.SetReplyToMessageID(*messageID)
	}
	if res := bot.SendMessage(chatID, message, options); !res.Ok {
		log.Printf("failed to send message: %s", *res.Description)
	}
}

// generate an answer to given message and send it to the chat
func answer(bot *tg.Bot, client *openai.Client, conf config, db *Database, messages []openai.ChatMessage, chatID, userID int64, username string, messageID int64) {
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
			if response.Choices[0].Message.Content != nil {
				answer = *response.Choices[0].Message.Content
			}
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
					SetCaption(strings.ToValidUTF8(answer[:128], "")+"...")); res.Ok {
				// save to database (successful)
				savePromptAndResult(db, chatID, userID, username, messagesToPrompt(messages), uint(response.Usage.PromptTokens), answer, uint(response.Usage.CompletionTokens), true)
			} else {
				log.Printf("failed to answer messages '%+v' with '%s' as file: %s", messages, answer, err)

				msg := "Failed to send you the answer as a text file. See the server logs for more information."
				send(bot, conf, msg, chatID, &messageID)

				// save to database (error)
				savePromptAndResult(db, chatID, userID, username, messagesToPrompt(messages), uint(response.Usage.PromptTokens), err.Error(), 0, false)
			}
		} else {
			if res := bot.SendMessage(
				chatID,
				answer,
				tg.OptionsSendMessage{}.
					SetReplyToMessageID(messageID)); res.Ok {
				// save to database (successful)
				savePromptAndResult(db, chatID, userID, username, messagesToPrompt(messages), uint(response.Usage.PromptTokens), answer, uint(response.Usage.CompletionTokens), true)
			} else {
				log.Printf("failed to answer messages '%+v' with '%s': %s", messages, answer, err)

				msg := "Failed to send you the answer as a text. See the server logs for more information."
				send(bot, conf, msg, chatID, &messageID)

				// save to database (error)
				savePromptAndResult(db, chatID, userID, username, messagesToPrompt(messages), uint(response.Usage.PromptTokens), err.Error(), 0, false)
			}
		}
	} else {
		log.Printf("failed to create chat completion: %s", err)

		msg := "Failed to generate an answer from OpenAI. See the server logs for more information."
		send(bot, conf, msg, chatID, &messageID)

		// save to database (error)
		savePromptAndResult(db, chatID, userID, username, messagesToPrompt(messages), 0, err.Error(), 0, false)
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
func userNameFromUpdate(update tg.Update) string {
	if from := update.GetFrom(); from != nil {
		return userName(from)
	} else {
		return "unknown"
	}
}

// get original message which was replied by given `message`
func repliedToMessage(message tg.Message) *tg.Message {
	if message.ReplyToMessage != nil {
		return message.ReplyToMessage
	}

	return nil
}

// convert given telegram bot message to an openai chat message,
// nil if there was any error.
//
// (if it was sent from bot, make it an assistant's message)
func convertMessage(bot *tg.Bot, message tg.Message) *openai.ChatMessage {
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

	var tokens []int
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

	content, err = io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return content, nil
}

// convert chat messages to a prompt for logging
func messagesToPrompt(messages []openai.ChatMessage) string {
	lines := []string{}

	for _, message := range messages {
		if message.Content != nil {
			lines = append(lines, fmt.Sprintf("[%s] %s", message.Role, *message.Content))
		}
	}

	return strings.Join(lines, "\n--------\n")
}

// retrieve stats from database
func retrieveStats(db *Database) string {
	if db == nil {
		return msgDatabaseNotConfigured
	} else {
		lines := []string{}

		var prompt Prompt
		if tx := db.db.First(&prompt); tx.Error == nil {
			lines = append(lines, fmt.Sprintf("Since <i>%s</i>", prompt.CreatedAt.Format("2006-01-02 15:04:05")))
			lines = append(lines, "")
		}

		var count int64
		if tx := db.db.Table("prompts").Select("count(distinct chat_id) as count").Scan(&count); tx.Error == nil {
			lines = append(lines, fmt.Sprintf("* Chats: <b>%d</b>", count))
		}

		var sumAndCount struct {
			Sum   int64
			Count int64
		}
		if tx := db.db.Table("prompts").Select("sum(tokens) as sum, count(id) as count").Where("tokens > 0").Scan(&sumAndCount); tx.Error == nil {
			lines = append(lines, fmt.Sprintf("* Prompts: <b>%d</b> (Total tokens: <b>%d</b>)", sumAndCount.Count, sumAndCount.Sum))
		}
		if tx := db.db.Table("generateds").Select("sum(tokens) as sum, count(id) as count").Where("successful = 1").Scan(&sumAndCount); tx.Error == nil {
			lines = append(lines, fmt.Sprintf("* Completions: <b>%d</b> (Total tokens: <b>%d</b>)", sumAndCount.Count, sumAndCount.Sum))
		}
		if tx := db.db.Table("generateds").Select("count(id) as count").Where("successful = 0").Scan(&count); tx.Error == nil {
			lines = append(lines, fmt.Sprintf("* Errors: <b>%d</b>", count))
		}

		if len(lines) > 0 {
			return strings.Join(lines, "\n")
		}

		return msgDatabaseEmpty
	}
}

// save prompt and its result to logs database
func savePromptAndResult(db *Database, chatID, userID int64, username string, prompt string, promptTokens uint, result string, resultTokens uint, resultSuccessful bool) {
	if db != nil {
		if err := db.SavePrompt(Prompt{
			ChatID:   chatID,
			UserID:   userID,
			Username: username,
			Text:     prompt,
			Tokens:   promptTokens,
			Result: Generated{
				Successful: resultSuccessful,
				Text:       result,
				Tokens:     resultTokens,
			},
		}); err != nil {
			log.Printf("failed to save prompt & result to database: %s", err)
		}
	}
}

// generate a help message with version info
func helpMessage() string {
	return fmt.Sprintf(msgHelp, version.Build(version.OS|version.Architecture|version.Revision))
}

// return a /start command handler
func startCommandHandler(conf config, allowedUsers map[string]bool) func(b *tg.Bot, update tg.Update, args string) {
	return func(b *tg.Bot, update tg.Update, _ string) {
		if !isAllowed(update, allowedUsers) {
			log.Printf("start command not allowed: %s", userNameFromUpdate(update))
			return
		}

		message := usableMessageFromUpdate(update)
		if message == nil {
			log.Printf("no usable message from update.")
			return
		}

		chatID := message.Chat.ID

		send(b, conf, msgStart, chatID, nil)
	}
}

// return a /stats command handler
func statsCommandHandler(conf config, db *Database, allowedUsers map[string]bool) func(b *tg.Bot, update tg.Update, args string) {
	return func(b *tg.Bot, update tg.Update, args string) {
		if !isAllowed(update, allowedUsers) {
			log.Printf("stats command not allowed: %s", userNameFromUpdate(update))
			return
		}

		message := usableMessageFromUpdate(update)
		if message == nil {
			log.Printf("no usable message from update.")
			return
		}

		chatID := message.Chat.ID
		messageID := message.MessageID

		send(b, conf, retrieveStats(db), chatID, &messageID)
	}
}

// return a /help command handler
func helpCommandHandler(conf config, allowedUsers map[string]bool) func(b *tg.Bot, update tg.Update, args string) {
	return func(b *tg.Bot, update tg.Update, _ string) {
		if !isAllowed(update, allowedUsers) {
			log.Printf("help command not allowed: %s", userNameFromUpdate(update))
			return
		}

		message := usableMessageFromUpdate(update)
		if message == nil {
			log.Printf("no usable message from update.")
			return
		}

		chatID := message.Chat.ID
		messageID := message.MessageID

		send(b, conf, helpMessage(), chatID, &messageID)
	}
}

// return a /count command handler
func countCommandHandler(conf config, allowedUsers map[string]bool) func(b *tg.Bot, update tg.Update, args string) {
	return func(b *tg.Bot, update tg.Update, args string) {
		if !isAllowed(update, allowedUsers) {
			log.Printf("count command not allowed: %s", userNameFromUpdate(update))
			return
		}

		message := usableMessageFromUpdate(update)
		if message == nil {
			log.Printf("no usable message from update.")
			return
		}

		chatID := message.Chat.ID
		messageID := message.MessageID

		var msg string
		if count, err := countTokens(args); err == nil {
			msg = fmt.Sprintf(msgTokenCount, count, len(args))
		} else {
			msg = err.Error()
		}

		send(b, conf, msg, chatID, &messageID)
	}
}

// return a 'no such command' handler
func noSuchCommandHandler(conf config, allowedUsers map[string]bool) func(b *tg.Bot, update tg.Update, cmd, args string) {
	return func(b *tg.Bot, update tg.Update, cmd, args string) {
		if !isAllowed(update, allowedUsers) {
			log.Printf("command not allowed: %s", userNameFromUpdate(update))
			return
		}

		message := usableMessageFromUpdate(update)
		if message == nil {
			log.Printf("no usable message from update.")
			return
		}

		chatID := message.Chat.ID
		messageID := message.MessageID

		msg := fmt.Sprintf(msgCmdNotSupported, cmd)
		send(b, conf, msg, chatID, &messageID)
	}
}
