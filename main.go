package main

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"mtuci-task-manager/api"
	"mtuci-task-manager/bot"

	"github.com/joho/godotenv"
)

func main() {
	// ============================================================
	// Загрузка конфигурации
	// ============================================================
	if err := godotenv.Load(); err != nil {
		log.Println("⚠️  Файл .env не найден, используем системные переменные окружения")
	}

	// Токен бота (обязательно)
	token := os.Getenv("TELEGRAM_BOT_TOKEN")
	if token == "" {
		log.Fatal("❌ TELEGRAM_BOT_TOKEN не задан! Укажи его в файле .env")
	}

	// URL Mini App (необязательно для работы бота, но нужен для кнопки)
	webAppURL := os.Getenv("WEBAPP_URL")
	if webAppURL == "" {
		log.Println("⚠️  WEBAPP_URL не задан — кнопка «Открыть приложение» не появится в боте")
	} else if parsed, err := url.ParseRequestURI(webAppURL); err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		// Bot API принимает для WebAppInfo только абсолютный HTTPS URL.
		log.Println("⚠️  WEBAPP_URL должен быть абсолютным HTTPS URL — кнопка Mini App отключена")
		webAppURL = ""
	} else {
		log.Printf("🌐 Mini App URL: %s", webAppURL)
	}

	// Порт HTTP-сервера (по умолчанию 8080)
	port := os.Getenv("SERVER_PORT")
	if port == "" {
		port = "8080"
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		log.Fatalf("❌ SERVER_PORT должен быть числом от 1 до 65535, получено %q", port)
	}

	// ============================================================
	// Создание общего хранилища задач
	// Используется и ботом, и HTTP API
	// ============================================================
	storage := bot.NewStorage()

	// ============================================================
	// Создание бота
	// ============================================================
	b, err := bot.New(token, storage, webAppURL)
	if err != nil {
		log.Fatalf("❌ Ошибка создания бота: %v", err)
	}

	// ============================================================
	// Запуск HTTP-сервера (в отдельной горутине)
	// Обслуживает:
	//   - /api/*     — REST API для Mini App
	//   - /*         — статические файлы фронтенда (папка web/)
	// ============================================================
	apiServer := api.NewServer(storage, token)
	apiServer.SetNotifier(b.SendNotification)
	httpServer := &http.Server{
		Addr:              net.JoinHostPort("", port),
		Handler:           apiServer.Router(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    64 << 10,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	serverErrors := make(chan error, 1)
	go func() {
		log.Printf("🌐 HTTP-сервер запущен на http://localhost:%s", port)
		serverErrors <- httpServer.ListenAndServe()
	}()

	go b.Start(ctx)

	// Ожидание сигнала остановки или ошибки HTTP-сервера
	log.Println("✅ Бот и HTTP-сервер запущены! Нажми Ctrl+C для остановки.")
	select {
	case <-ctx.Done():
		log.Println("⏳ Останавливаем приложение...")
	case err := <-serverErrors:
		if !errors.Is(err, http.ErrServerClosed) {
			log.Printf("❌ Ошибка HTTP-сервера: %v", err)
		}
		stop()
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("❌ Ошибка корректной остановки HTTP-сервера: %v", err)
	}
}
