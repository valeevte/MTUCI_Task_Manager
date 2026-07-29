package bot

import (
	"fmt"
	"log"
	"strconv"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// ============================================================
// Шаги диалога — определяют, чего бот ждёт от пользователя
// ============================================================
const (
	StepNone      = ""                    // Обычное состояние (ничего не ждём)
	StepWaitTitle = "waiting_title"       // Ждём ввод названия задачи
	StepWaitDesc  = "waiting_description" // Ждём ввод описания задачи
)

// ============================================================
// handleMessage — обрабатывает все текстовые сообщения
//
// Логика:
// 1. Проверяем, не находится ли пользователь в процессе создания задачи
// 2. Если да — обрабатываем ввод (название или описание)
// 3. Если нет — обрабатываем как команду или кнопку меню
// ============================================================
func (b *Bot) handleMessage(msg *tgbotapi.Message) {
	if msg == nil || msg.From == nil || msg.Chat == nil {
		return
	}
	b.registerUser(msg.From)

	userID := msg.From.ID
	chatID := msg.Chat.ID

	// Команды обрабатываются раньше состояния диалога, чтобы /cancel и /start
	// не превращались в название или описание создаваемой задачи.
	if msg.IsCommand() {
		switch msg.Command() {
		case "start":
			b.resetUserState(userID)
			b.handleStart(chatID)
		case "cancel":
			b.resetUserState(userID)
			b.sendText(chatID, "Создание задачи отменено.")
		default:
			b.sendText(chatID, "🤔 Неизвестная команда. Используй кнопки меню 👇")
		}
		return
	}

	// Кнопки главного меню тоже прерывают незавершённый диалог: иначе, например,
	// «📋 Мои задачи» ошибочно сохранялось бы как название новой задачи.
	switch msg.Text {
	case "📋 Мои задачи":
		b.resetUserState(userID)
		b.handleTaskList(chatID, userID)
		return
	case "➕ Новая задача":
		b.handleNewTask(chatID, userID)
		return
	case "ℹ️ О боте":
		b.resetUserState(userID)
		b.handleAbout(chatID)
		return
	}

	// Получаем текущее состояние диалога пользователя
	state := b.getUserState(userID)

	// Если пользователь в процессе создания задачи
	switch state.Step {
	case StepWaitTitle:
		b.handleTitleInput(chatID, userID, msg.Text)
		return
	case StepWaitDesc:
		b.handleDescriptionInput(chatID, userID, msg.Text)
		return
	}

	// Неизвестная команда — подсказываем использовать меню.
	b.sendText(chatID, "🤔 Не понимаю. Используй кнопки меню 👇")
}

// ============================================================
// handleStart — приветственное сообщение
//
// Вызывается при первом запуске бота или команде /start
// Можешь изменить текст приветствия по своему вкусу
// ============================================================
func (b *Bot) handleStart(chatID int64) {
	// Текст приветствия (MarkdownV2 — для жирного текста и форматирования)
	text := "👋 *Привет\\!*\n\n" +
		"Я — твой персональный менеджер задач\\.\n" +
		"Помогу организовать рабочий процесс\\.\n\n" +
		"Выбери действие в меню 👇"

	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "MarkdownV2"
	msg.ReplyMarkup = mainMenuKeyboard(b.webAppURL) // Показываем главное меню

	b.send(msg)
}

// ============================================================
// handleTaskList — показывает список задач пользователя
// ============================================================
func (b *Bot) handleTaskList(chatID, userID int64) {
	tasks := b.storage.GetTasks(userID)

	// Если задач нет — показываем подсказку
	if len(tasks) == 0 {
		b.sendText(chatID, "📭 У тебя пока нет задач.\nНажми «➕ Новая задача» чтобы создать первую!")
		return
	}

	// Формируем сообщение со списком
	text := fmt.Sprintf(
		"📋 *Твои задачи* \\(%d\\):\n\nНажми на задачу для подробностей 👇",
		len(tasks),
	)

	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "MarkdownV2"
	keyboard := taskListKeyboard(tasks)
	msg.ReplyMarkup = keyboard

	b.send(msg)
}

// ============================================================
// СОЗДАНИЕ ЗАДАЧИ — пошаговый диалог
// ============================================================

// handleNewTask — начинает процесс создания новой задачи
func (b *Bot) handleNewTask(chatID, userID int64) {
	b.setUserState(userID, UserState{Step: StepWaitTitle})

	b.sendText(chatID, "✏️ Введи название задачи (для отмены: /cancel):")
}

// handleTitleInput — пользователь ввёл название задачи
func (b *Bot) handleTitleInput(chatID, userID int64, title string) {
	normalizedTitle, _, err := ValidateTaskText(title, "")
	if err != nil {
		if err == ErrTaskTitleTooLong {
			b.sendText(chatID, fmt.Sprintf("⚠️ Название слишком длинное (максимум %d символов).", MaxTaskTitleLength))
		} else {
			b.sendText(chatID, "⚠️ Название не может быть пустым.")
		}
		return
	}

	b.setUserState(userID, UserState{
		Step:      StepWaitDesc,
		TempTitle: normalizedTitle,
	})

	// Предлагаем ввести описание или пропустить
	keyboard := skipKeyboard()
	b.sendWithInlineKeyboard(chatID, "📝 Теперь введи описание задачи (или нажми «Пропустить»):", keyboard)
}

// handleDescriptionInput — пользователь ввёл описание задачи
func (b *Bot) handleDescriptionInput(chatID, userID int64, description string) {
	b.finishTaskCreation(chatID, userID, description)
}

// finishTaskCreation — завершает создание задачи и сохраняет её
func (b *Bot) finishTaskCreation(chatID, userID int64, description string) {
	state := b.getUserState(userID)
	title := state.TempTitle

	// Проверяем, что название есть (на случай ошибки)
	if title == "" {
		b.sendText(chatID, "⚠️ Что-то пошло не так. Попробуй создать задачу заново.")
		b.resetUserState(userID)
		return
	}

	title, description, err := ValidateTaskText(title, description)
	if err != nil {
		if err == ErrTaskDescriptionTooLong {
			b.sendText(chatID, fmt.Sprintf("⚠️ Описание слишком длинное (максимум %d символов).", MaxTaskDescriptionLength))
			return
		}
		b.sendText(chatID, "⚠️ Что-то пошло не так. Попробуй создать задачу заново.")
		b.resetUserState(userID)
		return
	}

	// Сохраняем задачу в хранилище
	task := b.storage.AddTask(userID, title, description)

	// Сбрасываем состояние диалога
	b.resetUserState(userID)

	// Формируем сообщение-подтверждение
	text := fmt.Sprintf("✅ Задача создана!\n\n📌 %s\n📊 %s", task.Title, task.Status)
	if task.Description != "" {
		text = fmt.Sprintf("✅ Задача создана!\n\n📌 %s\n📝 %s\n📊 %s",
			task.Title, task.Description, task.Status)
	}

	b.sendText(chatID, text)
}

// ============================================================
// handleAbout — информация о боте
//
// Измени текст, чтобы описать свой проект
// ============================================================
func (b *Bot) handleAbout(chatID int64) {
	text := "ℹ️ *MTUCI Task Manager*\n\n" +
		"Версия: 1\\.0\\.0\n" +
		"📌 Возможности:\n" +
		"• Создание задач\n" +
		"• Просмотр списка задач\n" +
		"• Смена статуса\n" +
		"• Удаление задач\n" +
		"• Совместные группы в Mini App\n\n" +
		"🚧 В разработке:\n" +
		"• Постоянное хранилище\n" +
		"• Дедлайны и напоминания\n" +
		"• Приоритеты задач"

	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "MarkdownV2"
	b.send(msg)
}

// ============================================================
// handleCallback — обрабатывает нажатия inline-кнопок
//
// Каждая inline-кнопка отправляет callback с определённой строкой (data)
// По этой строке мы определяем, какое действие выполнить
// ============================================================
func (b *Bot) handleCallback(cb *tgbotapi.CallbackQuery) {
	if cb == nil || cb.From == nil {
		return
	}
	b.registerUser(cb.From)

	userID := cb.From.ID
	data := cb.Data

	// Отвечаем на callback — убирает "часики" загрузки на кнопке
	answer := tgbotapi.NewCallback(cb.ID, "")
	if _, err := b.api.Request(answer); err != nil {
		log.Printf("❌ Ошибка ответа на callback: %v", err)
	}

	// У inline-mode callback нет сообщения и, следовательно, chat_id.
	if cb.Message == nil || cb.Message.Chat == nil {
		return
	}
	chatID := cb.Message.Chat.ID

	// Определяем действие по callback data
	switch {

	// "Пропустить" — при создании задачи пропускаем описание
	case data == "skip":
		b.finishTaskCreation(chatID, userID, "")

	// "task_<ID>" — показать подробности задачи
	case strings.HasPrefix(data, "task_"):
		taskID := b.parseID(data, "task_")
		b.showTaskDetail(chatID, userID, taskID)

	// "status_<ID>" — показать меню выбора статуса
	case strings.HasPrefix(data, "status_"):
		taskID := b.parseID(data, "status_")
		b.showStatusSelection(chatID, taskID)

	// "setstatus_<ID>_<status>" — установить новый статус
	case strings.HasPrefix(data, "setstatus_"):
		b.handleSetStatus(chatID, userID, data)

	// "delete_<ID>" — запросить подтверждение удаления
	case strings.HasPrefix(data, "delete_"):
		taskID := b.parseID(data, "delete_")
		b.showDeleteConfirmation(chatID, taskID)

	// "confirm_delete_<ID>" — подтвердить удаление
	case strings.HasPrefix(data, "confirm_delete_"):
		taskID := b.parseID(data, "confirm_delete_")
		b.handleDelete(chatID, userID, taskID)

	// "back_to_list" — вернуться к списку задач
	case data == "back_to_list":
		b.handleTaskList(chatID, userID)
	}
}

// ============================================================
// ПРОСМОТР ЗАДАЧИ
// ============================================================

// showTaskDetail — показывает подробную информацию о задаче
func (b *Bot) showTaskDetail(chatID, userID int64, taskID int) {
	task, found := b.storage.GetTask(userID, taskID)
	if !found {
		b.sendText(chatID, "⚠️ Задача не найдена.")
		return
	}

	// Детали отправляются как обычный текст: пользовательское содержимое не
	// раздувается Markdown-экранированием и не может сломать разметку.
	text := fmt.Sprintf("📌 %s\n\n", task.Title)

	if task.Description != "" {
		text += fmt.Sprintf("📝 %s\n\n", task.Description)
	}

	text += fmt.Sprintf("📊 Статус: %s\n", task.Status)
	text += fmt.Sprintf("📅 Создана: %s", task.CreatedAt.Format("02.01.2006 15:04"))

	msg := tgbotapi.NewMessage(chatID, limitTelegramText(text))
	keyboard := taskActionsKeyboard(taskID)
	msg.ReplyMarkup = keyboard

	b.send(msg)
}

// ============================================================
// СМЕНА СТАТУСА
// ============================================================

// showStatusSelection — показывает кнопки выбора нового статуса
func (b *Bot) showStatusSelection(chatID int64, taskID int) {
	keyboard := statusKeyboard(taskID)
	b.sendWithInlineKeyboard(chatID, "Выбери новый статус:", keyboard)
}

// handleSetStatus — устанавливает выбранный статус
func (b *Bot) handleSetStatus(chatID, userID int64, data string) {
	// Callback data имеет формат: "setstatus_<taskID>_<statusKey>"
	// Разбиваем строку на 3 части по символу "_"
	parts := strings.SplitN(data, "_", 3)
	if len(parts) < 3 {
		return
	}

	// Парсим ID задачи из строки в число
	taskID, err := strconv.Atoi(parts[1])
	if err != nil {
		return
	}

	// Определяем полный статус по короткому ключу
	var status string
	switch parts[2] {
	case "new":
		status = StatusNew
	case "progress":
		status = StatusInProgress
	case "done":
		status = StatusDone
	default:
		return
	}

	// Обновляем статус в хранилище
	if b.storage.UpdateStatus(userID, taskID, status) {
		b.sendText(chatID, fmt.Sprintf("✅ Статус изменён на: %s", status))
		// Показываем обновлённые подробности задачи
		b.showTaskDetail(chatID, userID, taskID)
	} else {
		b.sendText(chatID, "⚠️ Задача не найдена.")
	}
}

// ============================================================
// УДАЛЕНИЕ ЗАДАЧИ
// ============================================================

// showDeleteConfirmation — запрашивает подтверждение перед удалением
func (b *Bot) showDeleteConfirmation(chatID int64, taskID int) {
	keyboard := confirmDeleteKeyboard(taskID)
	b.sendWithInlineKeyboard(chatID, "⚠️ Ты уверен, что хочешь удалить эту задачу?", keyboard)
}

// handleDelete — удаляет задачу из хранилища
func (b *Bot) handleDelete(chatID, userID int64, taskID int) {
	if b.storage.DeleteTask(userID, taskID) {
		b.sendText(chatID, "🗑 Задача удалена.")
	} else {
		b.sendText(chatID, "⚠️ Задача не найдена.")
	}
}

// ============================================================
// ВСПОМОГАТЕЛЬНЫЕ ФУНКЦИИ
// ============================================================

// send — отправляет подготовленное сообщение в Telegram
func (b *Bot) send(msg tgbotapi.MessageConfig) {
	if _, err := b.api.Send(msg); err != nil {
		log.Printf("❌ Ошибка отправки: %v", err)
	}
}

// sendText — отправляет простое текстовое сообщение
func (b *Bot) sendText(chatID int64, text string) {
	msg := tgbotapi.NewMessage(chatID, limitTelegramText(text))
	b.send(msg)
}

// sendWithInlineKeyboard — отправляет текст с inline-клавиатурой
func (b *Bot) sendWithInlineKeyboard(chatID int64, text string, keyboard tgbotapi.InlineKeyboardMarkup) {
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ReplyMarkup = keyboard
	b.send(msg)
}

// parseID — извлекает числовой ID из callback data
// Например: parseID("task_42", "task_") вернёт 42
func (b *Bot) parseID(data, prefix string) int {
	idStr := strings.TrimPrefix(data, prefix)
	id, err := strconv.Atoi(idStr)
	if err != nil {
		return 0
	}
	return id
}

// Оставляем запас до лимита Bot API в 4096 символов: некоторые emoji
// считаются клиентами как несколько кодовых единиц.
func limitTelegramText(text string) string {
	const safeLimit = 3800
	runes := []rune(text)
	if len(runes) <= safeLimit {
		return text
	}
	return string(runes[:safeLimit-1]) + "…"
}
