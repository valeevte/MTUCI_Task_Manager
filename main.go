package main

import (
	"log"
	"net/http"
	"os"

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
	} else {
		log.Printf("🌐 Mini App URL: %s", webAppURL)
	}

	// Порт HTTP-сервера (по умолчанию 8080)
	port := os.Getenv("SERVER_PORT")
	if port == "" {
		port = "8080"
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
	go func() {
		router := apiServer.Router()
		log.Printf("🌐 HTTP-сервер запущен на http://localhost:%s", port)
		if err := http.ListenAndServe(":"+port, router); err != nil {
			log.Fatalf("❌ Ошибка HTTP-сервера: %v", err)
		}
	}()

	// ============================================================
	// Запуск бота (в основном потоке)
	// Start() запускает бесконечный цикл обработки сообщений
	// ============================================================
	log.Println("✅ Бот и HTTP-сервер запущены! Нажми Ctrl+C для остановки.")
	b.Start()
}
