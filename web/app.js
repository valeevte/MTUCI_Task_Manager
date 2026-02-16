// ============================================================
// MTUCI Task Manager — Mini App Frontend
//
// Этот файл содержит всю логику фронтенда:
// - Инициализация Telegram WebApp SDK
// - Работа с API (CRUD задач)
// - Управление экранами (список, создание, детали)
// ============================================================

// ============================================================
// 1. ИНИЦИАЛИЗАЦИЯ TELEGRAM WEBAPP
// ============================================================
const tg = window.Telegram.WebApp;
tg.ready();   // Сообщаем Telegram, что приложение загрузилось
tg.expand();  // Раскрываем Mini App на весь экран

// initData — строка с данными пользователя и подписью
// Передаём её на сервер для авторизации
const initData = tg.initData;

// ============================================================
// 2. API-КЛИЕНТ
// ============================================================
const API_BASE = '/api';

// Отладка: проверяем наличие initData
console.log('🔑 initData:', initData ? 'есть (' + initData.length + ' символов)' : '⚠️ ПУСТО');

/**
 * Универсальная функция для запросов к API
 * Автоматически добавляет заголовок авторизации с initData
 */
async function api(method, path, body = null) {
    const options = {
        method,
        headers: {
            // Обход interstitial-страницы ngrok (бесплатный тариф)
            'ngrok-skip-browser-warning': 'true',
        },
    };

    // Добавляем авторизацию (initData от Telegram)
    if (initData) {
        options.headers['Authorization'] = 'tma ' + initData;
    }

    // Content-Type нужен только для запросов с телом (POST, PATCH)
    if (body) {
        options.headers['Content-Type'] = 'application/json';
        options.body = JSON.stringify(body);
    }

    console.log(`📡 ${method} ${API_BASE + path}`);

    const response = await fetch(API_BASE + path, options);

    // Проверяем, что ответ — JSON, а не HTML (ngrok interstitial)
    const contentType = response.headers.get('content-type') || '';
    if (!contentType.includes('application/json')) {
        console.error('❌ Сервер вернул не JSON:', contentType, 'Статус:', response.status);
        throw new Error('Сервер недоступен или вернул неожиданный ответ. Проверьте, что ngrok и сервер запущены.');
    }

    const data = await response.json();

    if (!response.ok) {
        console.error('❌ API ошибка:', response.status, data);
        throw new Error(data.error || 'Ошибка сервера');
    }

    return data;
}

// ============================================================
// 3. СОСТОЯНИЕ ПРИЛОЖЕНИЯ
// ============================================================
let tasks = [];        // Массив задач пользователя
let currentTask = null; // Текущая выбранная задача (для экрана деталей)

// ============================================================
// 4. УПРАВЛЕНИЕ ЭКРАНАМИ (навигация)
// ============================================================

/** Показать экран списка задач */
function showTaskList() {
    document.getElementById('task-list-view').classList.remove('hidden');
    document.getElementById('create-task-view').classList.add('hidden');
    document.getElementById('task-detail-view').classList.add('hidden');
    tg.BackButton.hide(); // На главном экране кнопка «Назад» не нужна
    loadTasks();
}

/** Показать экран создания задачи */
function showCreateForm() {
    document.getElementById('task-list-view').classList.add('hidden');
    document.getElementById('create-task-view').classList.remove('hidden');
    document.getElementById('task-detail-view').classList.add('hidden');
    tg.BackButton.show(); // Показываем кнопку «Назад» в хедере Telegram

    // Очищаем форму
    document.getElementById('task-title').value = '';
    document.getElementById('task-description').value = '';
    document.getElementById('task-title').focus();
}

/** Показать экран деталей задачи */
function showTaskDetail(taskId) {
    const task = tasks.find(t => t.id === taskId);
    if (!task) return;

    currentTask = task;

    document.getElementById('task-list-view').classList.add('hidden');
    document.getElementById('create-task-view').classList.add('hidden');
    document.getElementById('task-detail-view').classList.remove('hidden');
    tg.BackButton.show();

    renderTaskDetail(task);
}

// ============================================================
// 5. РЕНДЕРИНГ (отрисовка интерфейса)
// ============================================================

/** Отрисовать список задач */
function renderTasks() {
    const container = document.getElementById('tasks-container');
    const emptyState = document.getElementById('empty-state');

    if (tasks.length === 0) {
        container.classList.add('hidden');
        emptyState.classList.remove('hidden');
        return;
    }

    emptyState.classList.add('hidden');
    container.classList.remove('hidden');

    container.innerHTML = tasks.map(task => `
        <div class="task-card" onclick="showTaskDetail(${task.id})">
            <div class="task-card-header">
                <span class="task-card-title">${escapeHtml(task.title)}</span>
                <span class="task-card-status">${escapeHtml(task.status)}</span>
            </div>
            ${task.description ? `<div class="task-card-desc">${escapeHtml(task.description)}</div>` : ''}
            <div class="task-card-date">${formatDate(task.created_at)}</div>
        </div>
    `).join('');
}

/** Отрисовать детали задачи */
function renderTaskDetail(task) {
    const statusKey = getStatusKey(task.status);

    const content = document.getElementById('task-detail-content');
    content.innerHTML = `
        <div class="task-detail-title">${escapeHtml(task.title)}</div>
        <div class="task-detail-status">${escapeHtml(task.status)}</div>
        ${task.description
            ? `<div class="task-detail-desc">${escapeHtml(task.description)}</div>`
            : ''}
        <div class="task-detail-date">Создана: ${formatDate(task.created_at)}</div>

        <div class="section-title">Изменить статус</div>
        <div class="task-actions">
            <button class="btn-status ${statusKey === 'new' ? 'active' : ''}"
                    onclick="changeStatus(${task.id}, 'new')">
                🆕 Новая
            </button>
            <button class="btn-status ${statusKey === 'progress' ? 'active' : ''}"
                    onclick="changeStatus(${task.id}, 'progress')">
                🔄 В работе
            </button>
            <button class="btn-status ${statusKey === 'done' ? 'active' : ''}"
                    onclick="changeStatus(${task.id}, 'done')">
                ✅ Выполнена
            </button>
        </div>

        <button class="btn-delete" onclick="deleteTask(${task.id})">
            🗑 Удалить задачу
        </button>
    `;
}

// ============================================================
// 6. РАБОТА С API
// ============================================================

/** Загрузить задачи с сервера */
async function loadTasks() {
    try {
        tasks = await api('GET', '/tasks');
        if (!Array.isArray(tasks)) tasks = [];
        renderTasks();
    } catch (err) {
        console.error('Ошибка загрузки задач:', err);
        document.getElementById('tasks-container').innerHTML =
            `<div class="loading">Ошибка загрузки: ${escapeHtml(err.message)}</div>`;
    }
}

/** Создать новую задачу */
async function createTask(title, description) {
    try {
        await api('POST', '/tasks', { title, description });

        // Тактильная обратная связь (вибрация)
        try { tg.HapticFeedback.notificationOccurred('success'); } catch(e) {}

        showTaskList();
    } catch (err) {
        console.error('Ошибка создания задачи:', err);
        tg.showAlert('Ошибка создания задачи: ' + err.message);
    }
}

/** Изменить статус задачи */
async function changeStatus(taskId, status) {
    try {
        await api('PATCH', `/tasks/${taskId}/status`, { status });

        // Лёгкая вибрация
        try { tg.HapticFeedback.impactOccurred('light'); } catch(e) {}

        // Перезагружаем задачи и обновляем экран деталей
        await loadTasks();
        const updated = tasks.find(t => t.id === taskId);
        if (updated) {
            currentTask = updated;
            renderTaskDetail(updated);
        }
    } catch (err) {
        console.error('Ошибка обновления статуса:', err);
        tg.showAlert('Ошибка обновления статуса: ' + err.message);
    }
}

/** Удалить задачу */
function deleteTask(taskId) {
    tg.showConfirm('Удалить эту задачу?', async function(confirmed) {
        if (!confirmed) return;

        try {
            await api('DELETE', `/tasks/${taskId}`);

            try { tg.HapticFeedback.notificationOccurred('success'); } catch(e) {}

            showTaskList();
        } catch (err) {
            console.error('Ошибка удаления задачи:', err);
            tg.showAlert('Ошибка удаления задачи: ' + err.message);
        }
    });
}

// ============================================================
// 7. ВСПОМОГАТЕЛЬНЫЕ ФУНКЦИИ
// ============================================================

/** Определить короткий ключ статуса по полному тексту */
function getStatusKey(status) {
    if (status.includes('Новая')) return 'new';
    if (status.includes('В работе')) return 'progress';
    if (status.includes('Выполнена')) return 'done';
    return 'new';
}

/** Форматировать дату в читаемый вид */
function formatDate(dateStr) {
    if (!dateStr) return '';
    const date = new Date(dateStr);
    return date.toLocaleDateString('ru-RU', {
        day: '2-digit',
        month: '2-digit',
        year: 'numeric',
        hour: '2-digit',
        minute: '2-digit',
    });
}

/** Экранировать HTML-спецсимволы (защита от XSS) */
function escapeHtml(text) {
    if (!text) return '';
    const div = document.createElement('div');
    div.textContent = text;
    return div.innerHTML;
}

// ============================================================
// 8. ОБРАБОТЧИКИ СОБЫТИЙ
// ============================================================

// Кнопка "Новая задача"
document.getElementById('add-task-btn').addEventListener('click', showCreateForm);

// Отправка формы создания задачи
document.getElementById('create-task-form').addEventListener('submit', function(e) {
    e.preventDefault();
    const title = document.getElementById('task-title').value.trim();
    const description = document.getElementById('task-description').value.trim();
    if (title) {
        createTask(title, description);
    }
});

// Кнопка «Назад» в Telegram (в хедере Mini App)
tg.BackButton.onClick(function() {
    const detailView = document.getElementById('task-detail-view');
    const createView = document.getElementById('create-task-view');

    if (!detailView.classList.contains('hidden') || !createView.classList.contains('hidden')) {
        showTaskList();
    } else {
        tg.close();
    }
});

// ============================================================
// 9. ЗАПУСК
// ============================================================
showTaskList();
