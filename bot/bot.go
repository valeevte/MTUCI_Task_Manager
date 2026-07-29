package bot

import (
	"context"
	"log"
	"sync"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// ============================================================
// UserState хранит состояние диалога конкретного пользователя
// Нужно, чтобы бот "помнил", на каком шаге находится пользователь
// (например, ввод названия задачи)
// ============================================================
type UserState struct {
	Step      string // Текущий шаг диалога (например, "waiting_title")
	TempTitle string // Временное хранение названия при создании задачи
}

// ============================================================
// Bot — главная структура приложения
// Содержит всё необходимое для работы бота
// ============================================================
type Bot struct {
	api       *tgbotapi.BotAPI     // API-клиент для общения с Telegram
	storage   *Storage             // Хранилище задач (общее с HTTP API)
	users     map[int64]*UserState // Состояние диалога каждого пользователя
	mu        sync.RWMutex         // Мьютекс — защищает users от одновременного доступа из горутин
	webAppURL string               // URL Mini App (для кнопки в клавиатуре)
}

// ============================================================
// New создаёт нового бота
// token     — токен, полученный у @BotFather в Telegram
// storage   — общее хранилище задач (используется и ботом, и HTTP API)
// webAppURL — URL Mini App (для кнопки «Открыть приложение»)
// ============================================================
func New(token string, storage *Storage, webAppURL string) (*Bot, error) {
	// Создаём API-клиент
	api, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		return nil, err
	}

	// Debug = true покажет в консоли все запросы/ответы (полезно для отладки)
	// Измени на true, если хочешь видеть подробные логи
	api.Debug = false

	log.Printf("🤖 Авторизован как @%s", api.Self.UserName)

	return &Bot{
		api:       api,
		storage:   storage,
		users:     make(map[int64]*UserState),
		webAppURL: webAppURL,
	}, nil
}

// ============================================================
// Start запускает бесконечный цикл получения обновлений
// Использует Long Polling — бот "слушает" Telegram и получает новые сообщения
// ============================================================
func (b *Bot) Start(ctx context.Context) {
	// Настраиваем параметры получения обновлений
	config := tgbotapi.NewUpdate(0)
	config.Timeout = 30 // Ждём обновления до 30 секунд (long polling)

	// GetUpdatesChan возвращает Go-канал (channel), куда приходят обновления
	updates := b.api.GetUpdatesChan(config)

	for {
		select {
		case <-ctx.Done():
			b.api.StopReceivingUpdates()
			return
		case update, ok := <-updates:
			if !ok {
				return
			}
			// Telegram доставляет updates по порядку. Последовательная обработка
			// не даёт двум быстрым сообщениям пользователя перепутать шаги диалога.
			b.handleUpdate(update)
		}
	}
}

// ============================================================
// handleUpdate определяет тип обновления и направляет его
// к нужному обработчику (handler)
// ============================================================
func (b *Bot) handleUpdate(update tgbotapi.Update) {
	// Обновление может быть разного типа:

	// 1. Callback — пользователь нажал inline-кнопку
	if update.CallbackQuery != nil {
		b.handleCallback(update.CallbackQuery)
		return
	}

	// 2. Message — пользователь отправил текстовое сообщение
	if update.Message != nil {
		b.handleMessage(update.Message)
		return
	}

	// Другие типы обновлений (фото, стикеры и т.д.) пока игнорируем
}

// ============================================================
// Вспомогательные методы для работы с состоянием пользователя
// Мьютекс (mu) сохраняет безопасность состояния и для тестов/будущих
// обработчиков, которые могут вызывать методы бота параллельно.
// ============================================================

// getUserState возвращает копию состояния: указатель на map-значение нельзя
// безопасно читать после снятия блокировки, пока обновления идут параллельно.
func (b *Bot) getUserState(userID int64) UserState {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if state, exists := b.users[userID]; exists {
		return *state
	}
	return UserState{}
}

func (b *Bot) setUserState(userID int64, state UserState) {
	b.mu.Lock()
	defer b.mu.Unlock()

	stateCopy := state
	b.users[userID] = &stateCopy
}

// resetUserState сбрасывает состояние пользователя в начальное
func (b *Bot) resetUserState(userID int64) {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Удаление вместо хранения пустой записи не даёт map бесконечно расти.
	delete(b.users, userID)
}

// registerUser сохраняет данные Telegram-пользователя для группового режима.
func (b *Bot) registerUser(from *tgbotapi.User) {
	if from == nil {
		return
	}
	b.storage.UpsertVerifiedUser(from.ID, from.UserName, from.FirstName, from.LastName)
}

// SendNotification отправляет служебное уведомление пользователю в Telegram.
// Используется HTTP API для событий по групповым задачам.
func (b *Bot) SendNotification(userID int64, text string) error {
	msg := tgbotapi.NewMessage(userID, text)
	_, err := b.api.Send(msg)
	if err != nil {
		log.Printf("❌ Ошибка отправки уведомления user=%d: %v", userID, err)
	}
	return err
}
