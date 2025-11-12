package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"backend/internal"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	cfg := internal.LoadConfig()

	db := internal.NewDB(ctx, cfg.DatabaseURL)
	defer db.Close()

	log.Println("Инициализация схемы базы данных...")
	if err := db.InitSchema(ctx); err != nil {
		log.Fatalf("Ошибка при инициализации базы данных: %v", err)
	}

	svc := internal.NewServices(db)

	api, err := tgbotapi.NewBotAPI(cfg.TelegramToken)
	if err != nil {
		log.Fatalf("Ошибка при инициализации Telegram API: %v", err)
	}
	api.Debug = false

	bot := internal.NewBot(api, db, cfg, svc)
	web := internal.NewWeb(cfg, db, svc, bot)

	if cfg.UseWebhook {
		webhookURL := cfg.PublicBaseURL + cfg.WebhookPath
		wh, err := tgbotapi.NewWebhook(webhookURL)
		if err != nil {
			log.Fatalf("Ошибка создания webhook: %v", err)
		}

		if _, err := api.Request(wh); err != nil {
			log.Fatalf("Ошибка при установке webhook: %v", err)
		}

		go func() {
			if err := web.StartHTTP(ctx); err != nil {
				log.Printf("Ошибка HTTP-сервера: %v", err)
			}
		}()

		log.Printf("Бот запущен (@%s) в режиме webhook: %s", api.Self.UserName, webhookURL)

	} else {
		_, _ = api.Request(tgbotapi.DeleteWebhookConfig{})

		go func() {
			if err := web.StartHTTP(ctx); err != nil {
				log.Printf("Ошибка HTTP-сервера: %v", err)
			}
		}()

		go func() {
			if err := bot.StartLongPolling(ctx); err != nil {
				log.Printf("Ошибка long polling: %v", err)
			}
		}()

		log.Printf("🤖 Бот запущен (@%s) в режиме long polling", api.Self.UserName)
	}

	log.Println("✅ Приложение успешно запущено.")
	<-ctx.Done()
	log.Println("Завершение работы приложения...")
}
