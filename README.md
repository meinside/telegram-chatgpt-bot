# Telegram ChatGPT Bot

A telegram bot which answers to messages with [ChatGPT API](https://platform.openai.com/docs/api-reference/chat).

<img width="630" alt="1" src="https://user-images.githubusercontent.com/185988/227860711-fcb9b464-4c11-4de3-94d6-9e3cd68ce0c8.png">

You can also reply to messages for keeping the context of your conversation:

<img width="629" alt="2" src="https://user-images.githubusercontent.com/185988/227860693-a934b46f-6e28-45ff-a566-34ebd94045cf.png">

You can count the number of tokens of text with `/count` command:

<img width="630" alt="count_command" src="https://user-images.githubusercontent.com/185988/230024392-fba2c0b1-ba5e-42db-8a84-9f9653051d00.png">

## Prerequisites

* A [paid OpenAI account](https://openai.com/pricing), and
* a machine which can build and run golang applications.

## Configurations

Create a configuration file:

```bash
$ cp config.json.sample config.json
$ vi config.json
```

and set your values:

```json
{
  "allowed_telegram_users": ["user1", "user2"],
  "openai_model": "gpt-3.5-turbo",
  "db_filepath": null,
  "verbose": false,

  "telegram_bot_token": "123456:abcdefghijklmnop-QRSTUVWXYZ7890",
  "openai_api_key": "key-ABCDEFGHIJK1234567890",
  "openai_org_id": "org-1234567890abcdefghijk"
}
```

If `db_filepath` is given, all prompts and their responses will be logged in the SQLite3 file.

### Using Infisical

You can use [Infisical](https://infisical.com/) for retrieving your bot token and api key:

```json
{
  "allowed_telegram_users": ["user1", "user2"],
  "openai_model": "gpt-3.5-turbo",
  "db_filepath": null,
  "verbose": false,

  "infisical": {
    "workspace_id": "012345abcdefg",
    "token": "st.xyzwabcd.0987654321.abcdefghijklmnop",
    "environment": "dev",
    "secret_type": "shared",

    "telegram_bot_token_key_path": "/path/to/your/KEY_TO_CHATGPT_BOT_TOKEN",
    "openai_api_key_key_path": "/path/to/your/KEY_TO_OPENAI_API_KEY",
    "openai_org_id_key_path": "/path/to/your/KEY_TO_OPENAI_ORG_ID"
  }
}
```

If your Infisical workspace's E2EE setting is enabled, you also need to provide your API key:

```json
{
  "allowed_telegram_users": ["user1", "user2"],
  "openai_model": "gpt-3.5-turbo",
  "db_filepath": null,
  "verbose": false,

  "infisical": {
    "e2ee": true,
    "api_key": "ak.1234567890.abcdefghijk",

    "workspace_id": "012345abcdefg",
    "token": "st.xyzwabcd.0987654321.abcdefghijklmnop",
    "environment": "dev",
    "secret_type": "shared",

    "telegram_bot_token_key_path": "/path/to/your/KEY_TO_CHATGPT_BOT_TOKEN",
    "openai_api_key_key_path": "/path/to/your/KEY_TO_OPENAI_API_KEY",
    "openai_org_id_key_path": "/path/to/your/KEY_TO_OPENAI_ORG_ID"
  }
}
```


## Build

```bash
$ go build
```

## Run

Run the built binary with the config file's path:

```bash
$ ./telegram-chatgpt-bot path-to/config.json
```

## Run as a systemd service

Createa a systemd service file:

```
[Unit]
Description=Telegram ChatGPT Bot
After=syslog.target
After=network.target

[Service]
Type=simple
User=ubuntu
Group=ubuntu
WorkingDirectory=/dir/to/telegram-chatgpt-bot
ExecStart=/dir/to/telegram-chatgpt-bot/telegram-chatgpt-bot /path/to/config.json
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
```

and `systemctl` enable|start|restart|stop the service.

## Commands

- `/help` for help message.

## Todos / Known Issues

- [X] Handle returning messages' size limit (Telegram Bot API's limit: [4096 chars](https://core.telegram.org/bots/api#sendmessage))
  - Will send a text document instead of an ordinary text message.

## License

The MIT License (MIT)

Copyright © 2024 Sungjin Han

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.

