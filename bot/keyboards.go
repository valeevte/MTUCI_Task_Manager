package bot

import (
	"fmt"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// ============================================================
// WebApp-совместимые типы
//
// Библиотека go-telegram-bot-api v5.5.1 не поддерживает WebApp,
// поэтому определяем свои типы для JSON-сериализации.
// ReplyMarkup принимает interface{} — любой JSON-объект подходит.
// ============================================================
type webAppInfo struct {
	URL string `json:"url"`
}

type webAppButton struct {
	Text   string      `json:"text"`
	WebApp *webAppInfo `json:"web_app,omitempty"`
}

type webAppReplyKeyboard struct {
	Keyboard       [][]webAppButton `json:"keyboard"`
	ResizeKeyboard bool             `json:"resize_keyboard"`
}

// ============================================================
// ГЛАВНОЕ МЕНЮ — Reply-клавиатура
// Эта клавиатура всегда видна внизу чата
//
// webAppURL — URL Mini App; если пустой, кнопка не показывается
// ============================================================
func mainMenuKeyboard(webAppURL string) interface{} {
	// Собираем ряды кнопок
	rows := [][]webAppButton{
		// Первый ряд: основные действия
		{
			{Text: "📋 Мои задачи"},
			{Text: "➕ Новая задача"},
		},
	}

	// Если WEBAPP_URL задан — добавляем кнопку открытия Mini App
	if webAppURL != "" {
		rows = append(rows, []webAppButton{
			{
				Text:   "📱 Открыть приложение",
				WebApp: &webAppInfo{URL: webAppURL},
			},
		})
	}

	// Последний ряд: информация
	rows = append(rows, []webAppButton{
		{Text: "ℹ️ О боте"},
	})

	return webAppReplyKeyboard{
		Keyboard:       rows,
		ResizeKeyboard: true,
	}
}

// ============================================================
// СПИСОК ЗАДАЧ — Inline-клавиатура
// Каждая задача отображается как кнопка с её статусом и названием
//
// При нажатии отправляется callback с данными "task_<ID>"
// ============================================================
func taskListKeyboard(tasks []Task) tgbotapi.InlineKeyboardMarkup {
	// Создаём срез рядов кнопок
	var rows [][]tgbotapi.InlineKeyboardButton

	for _, task := range tasks {
		// Текст кнопки: "статус | название"
		buttonText := fmt.Sprintf("%s | %s", task.Status, task.Title)

		// callback data — строка, которая придёт боту при нажатии
		callbackData := fmt.Sprintf("task_%d", task.ID)

		// Создаём ряд с одной кнопкой
		row := tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(buttonText, callbackData),
		)
		rows = append(rows, row)
	}

	return tgbotapi.NewInlineKeyboardMarkup(rows...)
}

// ============================================================
// ДЕЙСТВИЯ С ЗАДАЧЕЙ — Inline-клавиатура
// Показывается при просмотре конкретной задачи
//
// Можешь добавить свои кнопки, например:
// "📎 Прикрепить файл", "👤 Назначить исполнителя" и т.д.
// ============================================================
func taskActionsKeyboard(taskID int) tgbotapi.InlineKeyboardMarkup {
	return tgbotapi.NewInlineKeyboardMarkup(
		// Ряд 1: смена статуса
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(
				"🔄 Сменить статус",
				fmt.Sprintf("status_%d", taskID),
			),
		),
		// Ряд 2: удаление
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(
				"🗑 Удалить",
				fmt.Sprintf("delete_%d", taskID),
			),
		),
		// Ряд 3: назад к списку
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("⬅️ К списку задач", "back_to_list"),
		),
	)
}

// ============================================================
// ВЫБОР СТАТУСА — Inline-клавиатура
// Показывает все доступные статусы для задачи
//
// Чтобы добавить новый статус:
// 1. Добавь константу в storage.go (например, StatusOnHold = "⏸ На паузе")
// 2. Добавь новый ряд кнопок ниже
// 3. Добавь обработку в handlers.go (функция handleSetStatus)
// ============================================================
func statusKeyboard(taskID int) tgbotapi.InlineKeyboardMarkup {
	return tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(
				StatusNew,
				fmt.Sprintf("setstatus_%d_new", taskID),
			),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(
				StatusInProgress,
				fmt.Sprintf("setstatus_%d_progress", taskID),
			),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(
				StatusDone,
				fmt.Sprintf("setstatus_%d_done", taskID),
			),
		),
		// Кнопка "Назад" к деталям задачи
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(
				"⬅️ Назад",
				fmt.Sprintf("task_%d", taskID),
			),
		),
	)
}

// ============================================================
// ПРОПУСТИТЬ — Inline-клавиатура
// Используется при необязательных шагах (например, описание задачи)
// ============================================================
func skipKeyboard() tgbotapi.InlineKeyboardMarkup {
	return tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("⏭ Пропустить", "skip"),
		),
	)
}

// ============================================================
// ПОДТВЕРЖДЕНИЕ УДАЛЕНИЯ — Inline-клавиатура
// Защита от случайного удаления задачи
// ============================================================
func confirmDeleteKeyboard(taskID int) tgbotapi.InlineKeyboardMarkup {
	return tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(
				"✅ Да, удалить",
				fmt.Sprintf("confirm_delete_%d", taskID),
			),
			tgbotapi.NewInlineKeyboardButtonData(
				"❌ Отмена",
				fmt.Sprintf("task_%d", taskID),
			),
		),
	)
}
