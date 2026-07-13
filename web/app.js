const tg = window.Telegram?.WebApp || {
    initData: '',
    initDataUnsafe: {},
    ready() {},
    expand() {},
    close() {},
    showAlert(message) { window.alert(message); },
    showConfirm(message, cb) { cb(window.confirm(message)); },
    BackButton: {
        show() {},
        hide() {},
        onClick() {},
    },
    HapticFeedback: {
        impactOccurred() {},
        notificationOccurred() {},
    },
};

tg.ready();
tg.expand();

function initDataFromLocation() {
    try {
        const searchParams = new URLSearchParams(window.location.search);
        const fromSearch = searchParams.get('tgWebAppData') || searchParams.get('initData');
        if (fromSearch) return fromSearch;

        const hash = window.location.hash.startsWith('#')
            ? window.location.hash.slice(1)
            : window.location.hash;
        if (!hash) return '';

        const hashParams = new URLSearchParams(hash);
        return hashParams.get('tgWebAppData') || hashParams.get('initData') || '';
    } catch (e) {
        return '';
    }
}

const initData = (
    tg.initData ||
    initDataFromLocation() ||
    window.localStorage.getItem('mtuci_init_data') ||
    ''
).trim();

if (initData) {
    window.localStorage.setItem('mtuci_init_data', initData);
}

function parseUserIdFromInitData(data) {
    if (!data) return 0;
    try {
        const params = new URLSearchParams(data);
        const userRaw = params.get('user');
        if (!userRaw) return 0;
        return Number(JSON.parse(userRaw).id || 0);
    } catch (e) {
        return 0;
    }
}

let currentUserId = Number(tg.initDataUnsafe?.user?.id || 0);
if (!currentUserId) {
    currentUserId = parseUserIdFromInitData(initData);
}

function normalizeUsername(value) {
    const trimmed = (value || '').trim();
    if (!trimmed) return '';
    return trimmed.startsWith('@') ? trimmed : '@' + trimmed;
}
const API_BASE = '/api';

let activeTab = 'personal';
let personalView = 'list';
let groupView = 'list';

let personalTasks = [];
let currentPersonalTask = null;

let groups = [];
let currentGroup = null;
let groupMembers = [];
let groupTasks = [];
let currentGroupTask = null;

async function api(method, path, body = null) {
    const options = {
        method,
        headers: {
            'ngrok-skip-browser-warning': 'true',
        },
    };

    if (initData) {
        options.headers.Authorization = 'tma ' + initData;
        options.headers['X-Telegram-Init-Data'] = initData;
    }

    if (body) {
        options.headers['Content-Type'] = 'application/json';
        options.body = JSON.stringify(body);
    }

    const response = await fetch(API_BASE + path, options);
    const contentType = response.headers.get('content-type') || '';

    if (!contentType.includes('application/json')) {
        throw new Error('Сервер вернул неожиданный формат ответа');
    }

    const data = await response.json();
    if (!response.ok) {
        throw new Error(data.error || 'Ошибка сервера');
    }

    return data;
}

async function loadCurrentUser() {
    try {
        const user = await api('GET', '/me');
        currentUserId = Number(user.id || 0);
    } catch (e) {
        if (!currentUserId) {
            currentUserId = parseUserIdFromInitData(initData);
        }
    }
}

function haptic(type) {
    try {
        if (type === 'success') {
            tg.HapticFeedback.notificationOccurred('success');
        } else {
            tg.HapticFeedback.impactOccurred('light');
        }
    } catch (e) {}
}

function setActiveTab(tab) {
    activeTab = tab;

    document.getElementById('tab-personal').classList.toggle('active', tab === 'personal');
    document.getElementById('tab-groups').classList.toggle('active', tab === 'groups');

    document.getElementById('personal-root').classList.toggle('hidden', tab !== 'personal');
    document.getElementById('groups-root').classList.toggle('hidden', tab !== 'groups');

    if (tab === 'personal') {
        showPersonalList();
    } else {
        showGroupsList();
    }
}

function setPersonalView(view) {
    personalView = view;
    document.getElementById('personal-list-view').classList.toggle('hidden', view !== 'list');
    document.getElementById('personal-create-view').classList.toggle('hidden', view !== 'create');
    document.getElementById('personal-detail-view').classList.toggle('hidden', view !== 'detail');
    updateBackButton();
}

function setGroupView(view) {
    groupView = view;
    document.getElementById('groups-list-view').classList.toggle('hidden', view !== 'list');
    document.getElementById('group-board-view').classList.toggle('hidden', view !== 'board');
    document.getElementById('group-task-create-view').classList.toggle('hidden', view !== 'create-task');
    document.getElementById('group-task-detail-view').classList.toggle('hidden', view !== 'detail-task');
    updateBackButton();
}

function updateBackButton() {
    const onRootPersonal = activeTab === 'personal' && personalView === 'list';
    const onRootGroups = activeTab === 'groups' && groupView === 'list';
    if (onRootPersonal || onRootGroups) {
        tg.BackButton.hide();
    } else {
        tg.BackButton.show();
    }
}

function handleBackNavigation() {
    if (activeTab === 'personal') {
        if (personalView === 'create' || personalView === 'detail') {
            showPersonalList();
        } else {
            tg.close();
        }
        return;
    }

    if (groupView === 'detail-task' || groupView === 'create-task') {
        showGroupBoard();
        return;
    }

    if (groupView === 'board') {
        showGroupsList();
        return;
    }

    tg.close();
}

tg.BackButton.onClick(handleBackNavigation);

function showError(message) {
    tg.showAlert(message);
}

function getStatusKey(status) {
    if (status.includes('Новая')) return 'new';
    if (status.includes('В работе')) return 'progress';
    if (status.includes('Выполнена')) return 'done';
    return 'new';
}

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

function escapeHtml(text) {
    if (!text) return '';
    const div = document.createElement('div');
    div.textContent = text;
    return div.innerHTML;
}

// ============================================================
// Личные задачи
// ============================================================

async function loadPersonalTasks() {
    try {
        personalTasks = await api('GET', '/tasks');
        if (!Array.isArray(personalTasks)) personalTasks = [];
        renderPersonalTasks();
    } catch (err) {
        document.getElementById('tasks-container').innerHTML =
            `<div class="loading">Ошибка загрузки: ${escapeHtml(err.message)}</div>`;
    }
}

function renderPersonalTasks() {
    const container = document.getElementById('tasks-container');
    const empty = document.getElementById('empty-state');

    if (personalTasks.length === 0) {
        container.classList.add('hidden');
        empty.classList.remove('hidden');
        return;
    }

    empty.classList.add('hidden');
    container.classList.remove('hidden');

    container.innerHTML = personalTasks.map(task => `
        <div class="task-card" data-open-personal-task="${task.id}">
            <div class="task-card-header">
                <span class="task-card-title">${escapeHtml(task.title)}</span>
                <span class="task-card-status">${escapeHtml(task.status)}</span>
            </div>
            ${task.description ? `<div class="task-card-desc">${escapeHtml(task.description)}</div>` : ''}
            <div class="task-card-date">${formatDate(task.created_at)}</div>
        </div>
    `).join('');
}

function showPersonalList() {
    setPersonalView('list');
    currentPersonalTask = null;
    loadPersonalTasks();
}

function showPersonalCreate() {
    document.getElementById('task-title').value = '';
    document.getElementById('task-description').value = '';
    setPersonalView('create');
    document.getElementById('task-title').focus();
}

function showPersonalTaskDetail(taskID) {
    const task = personalTasks.find(item => item.id === taskID);
    if (!task) return;

    currentPersonalTask = task;
    const statusKey = getStatusKey(task.status);

    const content = document.getElementById('task-detail-content');
    content.innerHTML = `
        <div class="task-detail-title">${escapeHtml(task.title)}</div>
        <div class="task-detail-status">${escapeHtml(task.status)}</div>
        ${task.description ? `<div class="task-detail-desc">${escapeHtml(task.description)}</div>` : ''}
        <div class="task-detail-date">Создана: ${formatDate(task.created_at)}</div>

        <div class="section-title">Изменить статус</div>
        <div class="task-actions">
            <button class="btn-status ${statusKey === 'new' ? 'active' : ''}" data-personal-status="new">🆕 Новая</button>
            <button class="btn-status ${statusKey === 'progress' ? 'active' : ''}" data-personal-status="progress">🔄 В работе</button>
            <button class="btn-status ${statusKey === 'done' ? 'active' : ''}" data-personal-status="done">✅ Выполнена</button>
        </div>

        <button class="btn-delete" id="delete-personal-task">🗑 Удалить задачу</button>
    `;

    content.querySelectorAll('[data-personal-status]').forEach(btn => {
        btn.addEventListener('click', () => changePersonalTaskStatus(task.id, btn.dataset.personalStatus));
    });

    document.getElementById('delete-personal-task').addEventListener('click', () => deletePersonalTask(task.id));

    setPersonalView('detail');
}

async function createPersonalTask(title, description) {
    try {
        await api('POST', '/tasks', { title, description });
        haptic('success');
        showPersonalList();
    } catch (err) {
        showError('Ошибка создания задачи: ' + err.message);
    }
}

async function changePersonalTaskStatus(taskID, status) {
    try {
        await api('PATCH', `/tasks/${taskID}/status`, { status });
        haptic('light');
        await loadPersonalTasks();
        showPersonalTaskDetail(taskID);
    } catch (err) {
        showError('Ошибка обновления статуса: ' + err.message);
    }
}

function deletePersonalTask(taskID) {
    tg.showConfirm('Удалить эту задачу?', async (confirmed) => {
        if (!confirmed) return;

        try {
            await api('DELETE', `/tasks/${taskID}`);
            haptic('success');
            showPersonalList();
        } catch (err) {
            showError('Ошибка удаления задачи: ' + err.message);
        }
    });
}

// ============================================================
// Группы
// ============================================================

async function loadGroups() {
    try {
        groups = await api('GET', '/groups');
        if (!Array.isArray(groups)) groups = [];
        renderGroups();
    } catch (err) {
        document.getElementById('groups-container').innerHTML =
            `<div class="loading">Ошибка загрузки: ${escapeHtml(err.message)}</div>`;
    }
}

function renderGroups() {
    const container = document.getElementById('groups-container');
    const empty = document.getElementById('groups-empty');

    if (groups.length === 0) {
        container.classList.add('hidden');
        empty.classList.remove('hidden');
        return;
    }

    empty.classList.add('hidden');
    container.classList.remove('hidden');

    container.innerHTML = groups.map(group => `
        <div class="group-card" data-open-group="${group.id}">
            <div>
                <div class="group-title">${escapeHtml(group.name)}</div>
                <div class="group-meta">ID: ${group.id}</div>
            </div>
            <button class="btn-secondary" data-open-group="${group.id}">Открыть</button>
        </div>
    `).join('');
}

function showGroupsList() {
    currentGroup = null;
    currentGroupTask = null;
    setGroupView('list');
    loadGroups();
}

async function createGroup(name) {
    try {
        await api('POST', '/groups', { name });
        haptic('success');
        document.getElementById('group-name').value = '';
        await loadGroups();
    } catch (err) {
        showError('Ошибка создания группы: ' + err.message);
    }
}

async function openGroupBoard(groupID) {
    currentGroup = groups.find(item => item.id === groupID);
    if (!currentGroup) return;

    document.getElementById('group-title').textContent = currentGroup.name;

    const canDeleteGroup = currentUserId > 0 && currentGroup.creator_id === currentUserId;
    document.getElementById('delete-group-btn').classList.toggle('hidden', !canDeleteGroup);

    setGroupView('board');

    await Promise.all([loadGroupMembers(), loadGroupTasks()]);
}

function showGroupBoard() {
    if (!currentGroup) {
        showGroupsList();
        return;
    }
    setGroupView('board');
}

async function loadGroupMembers() {
    if (!currentGroup) return;

    try {
        groupMembers = await api('GET', `/groups/${currentGroup.id}/members`);
        if (!Array.isArray(groupMembers)) groupMembers = [];
        renderGroupMembers();
    } catch (err) {
        document.getElementById('members-container').innerHTML =
            `<div class="loading">Ошибка загрузки участников: ${escapeHtml(err.message)}</div>`;
    }
}

function renderGroupMembers() {
    const container = document.getElementById('members-container');
    if (groupMembers.length === 0) {
        container.innerHTML = '<div class="loading">Участники не найдены</div>';
        return;
    }

    container.innerHTML = groupMembers.map(member => `
        <div class="member-row">
            <div>
                <div class="member-name">${escapeHtml(member.first_name || '')} ${escapeHtml(member.last_name || '')}</div>
                <div class="member-username">${escapeHtml(member.username || '')}</div>
            </div>
            ${member.is_creator
                ? '<span class="badge">Creator</span>'
                : `<button class="btn-secondary" data-remove-member="${member.user_id}">Удалить</button>`}
        </div>
    `).join('');
}

async function addGroupMember(username) {
    if (!currentGroup) return;

    try {
        await api('POST', `/groups/${currentGroup.id}/members`, { username: normalizeUsername(username) });
        haptic('success');
        document.getElementById('member-username').value = '';
        await Promise.all([loadGroupMembers(), loadGroupTasks()]);
    } catch (err) {
        showError('Ошибка добавления участника: ' + err.message);
    }
}

function removeGroupMember(memberID) {
    if (!currentGroup) return;

    tg.showConfirm('Удалить участника из группы?', async (confirmed) => {
        if (!confirmed) return;

        try {
            await api('DELETE', `/groups/${currentGroup.id}/members/${memberID}`);
            haptic('light');
            await Promise.all([loadGroupMembers(), loadGroupTasks()]);
        } catch (err) {
            showError('Ошибка удаления участника: ' + err.message);
        }
    });
}

async function deleteCurrentGroup() {
    if (!currentGroup) return;

    tg.showConfirm('Удалить группу целиком?', async (confirmed) => {
        if (!confirmed) return;

        try {
            await api('DELETE', `/groups/${currentGroup.id}`);
            haptic('success');
            showGroupsList();
        } catch (err) {
            showError('Ошибка удаления группы: ' + err.message);
        }
    });
}

async function loadGroupTasks() {
    if (!currentGroup) return;

    try {
        groupTasks = await api('GET', `/groups/${currentGroup.id}/tasks`);
        if (!Array.isArray(groupTasks)) groupTasks = [];
        renderGroupTasks();
    } catch (err) {
        document.getElementById('group-tasks-container').innerHTML =
            `<div class="loading">Ошибка загрузки задач: ${escapeHtml(err.message)}</div>`;
    }
}

function renderGroupTasks() {
    const container = document.getElementById('group-tasks-container');
    const empty = document.getElementById('group-tasks-empty');

    if (groupTasks.length === 0) {
        container.classList.add('hidden');
        empty.classList.remove('hidden');
        return;
    }

    empty.classList.add('hidden');
    container.classList.remove('hidden');

    container.innerHTML = groupTasks.map(task => `
        <div class="task-card" data-open-group-task="${task.id}">
            <div class="task-card-header">
                <span class="task-card-title">${escapeHtml(task.title)}</span>
                <span class="task-card-status">${escapeHtml(task.status)}</span>
            </div>
            ${task.description ? `<div class="task-card-desc">${escapeHtml(task.description)}</div>` : ''}
            <div class="task-card-date">${task.assignee_username ? 'Ответственный: ' + escapeHtml(task.assignee_username) : 'Общая задача'}</div>
        </div>
    `).join('');
}

function showGroupTaskCreate() {
    if (!currentGroup) return;

    document.getElementById('group-task-title').value = '';
    document.getElementById('group-task-description').value = '';
    document.getElementById('group-task-assignee').value = '';

    setGroupView('create-task');
    document.getElementById('group-task-title').focus();
}

async function createGroupTask(title, description, assigneeUsername) {
    if (!currentGroup) return;

    try {
        await api('POST', `/groups/${currentGroup.id}/tasks`, {
            title,
            description,
            assignee_username: normalizeUsername(assigneeUsername),
        });

        haptic('success');
        setGroupView('board');
        await loadGroupTasks();
    } catch (err) {
        showError('Ошибка создания групповой задачи: ' + err.message);
    }
}

function showGroupTaskDetail(taskID) {
    const task = groupTasks.find(item => item.id === taskID);
    if (!task) return;

    currentGroupTask = task;
    const statusKey = getStatusKey(task.status);

    const content = document.getElementById('group-task-detail-content');
    content.innerHTML = `
        <div class="task-detail-title">${escapeHtml(task.title)}</div>
        <div class="task-detail-status">${escapeHtml(task.status)}</div>
        ${task.description ? `<div class="task-detail-desc">${escapeHtml(task.description)}</div>` : ''}
        <div class="task-detail-date">Создана: ${formatDate(task.created_at)}</div>
        <div class="task-detail-date">${task.assignee_username ? 'Ответственный: ' + escapeHtml(task.assignee_username) : 'Общая задача'}</div>

        <div class="section-title">Изменить статус</div>
        <div class="task-actions">
            <button class="btn-status ${statusKey === 'new' ? 'active' : ''}" data-group-status="new">🆕 Новая</button>
            <button class="btn-status ${statusKey === 'progress' ? 'active' : ''}" data-group-status="progress">🔄 В работе</button>
            <button class="btn-status ${statusKey === 'done' ? 'active' : ''}" data-group-status="done">✅ Выполнена</button>
        </div>

        <div class="section-title">Ответственный</div>
        <form id="set-assignee-form" class="inline-form">
            <input type="text" id="set-assignee-username" placeholder="@username" value="${escapeHtml(task.assignee_username || '')}">
            <button type="submit" class="btn-primary">Назначить</button>
        </form>
        <button id="clear-assignee-btn" class="btn-secondary full-width">Сделать общей задачей</button>

        <button id="delete-group-task-btn" class="btn-delete">🗑 Удалить задачу</button>
    `;

    content.querySelectorAll('[data-group-status]').forEach(btn => {
        btn.addEventListener('click', () => changeGroupTaskStatus(task.id, btn.dataset.groupStatus));
    });

    document.getElementById('set-assignee-form').addEventListener('submit', (e) => {
        e.preventDefault();
        const value = document.getElementById('set-assignee-username').value.trim();
        setGroupTaskAssignee(task.id, value);
    });

    document.getElementById('clear-assignee-btn').addEventListener('click', () => {
        setGroupTaskAssignee(task.id, '');
    });

    document.getElementById('delete-group-task-btn').addEventListener('click', () => {
        deleteGroupTask(task.id);
    });

    setGroupView('detail-task');
}

async function changeGroupTaskStatus(taskID, status) {
    if (!currentGroup) return;

    try {
        const updated = await api('PATCH', `/groups/${currentGroup.id}/tasks/${taskID}/status`, { status });
        haptic('light');
        await loadGroupTasks();
        showGroupTaskDetail(updated.id);
    } catch (err) {
        showError('Ошибка обновления статуса: ' + err.message);
    }
}

async function setGroupTaskAssignee(taskID, username) {
    if (!currentGroup) return;

    try {
        const updated = await api('PATCH', `/groups/${currentGroup.id}/tasks/${taskID}/assignee`, {
            assignee_username: normalizeUsername(username),
        });
        haptic('light');
        await loadGroupTasks();
        showGroupTaskDetail(updated.id);
    } catch (err) {
        showError('Ошибка назначения ответственного: ' + err.message);
    }
}

function deleteGroupTask(taskID) {
    if (!currentGroup) return;

    tg.showConfirm('Удалить групповую задачу?', async (confirmed) => {
        if (!confirmed) return;

        try {
            await api('DELETE', `/groups/${currentGroup.id}/tasks/${taskID}`);
            haptic('success');
            setGroupView('board');
            await loadGroupTasks();
        } catch (err) {
            showError('Ошибка удаления задачи: ' + err.message);
        }
    });
}

// ============================================================
// Статические обработчики
// ============================================================

document.getElementById('tab-personal').addEventListener('click', () => setActiveTab('personal'));
document.getElementById('tab-groups').addEventListener('click', () => setActiveTab('groups'));

document.getElementById('add-task-btn').addEventListener('click', showPersonalCreate);
document.getElementById('create-first-task').addEventListener('click', showPersonalCreate);
document.getElementById('cancel-create-task').addEventListener('click', showPersonalList);
document.getElementById('personal-back-btn').addEventListener('click', showPersonalList);

document.getElementById('create-task-form').addEventListener('submit', (e) => {
    e.preventDefault();
    const title = document.getElementById('task-title').value.trim();
    const description = document.getElementById('task-description').value.trim();
    if (title) {
        createPersonalTask(title, description);
    }
});

document.getElementById('tasks-container').addEventListener('click', (e) => {
    const open = e.target.closest('[data-open-personal-task]');
    if (!open) return;
    showPersonalTaskDetail(Number(open.dataset.openPersonalTask));
});

document.getElementById('create-group-form').addEventListener('submit', (e) => {
    e.preventDefault();
    const name = document.getElementById('group-name').value.trim();
    if (name) {
        createGroup(name);
    }
});

document.getElementById('groups-container').addEventListener('click', (e) => {
    const open = e.target.closest('[data-open-group]');
    if (!open) return;
    openGroupBoard(Number(open.dataset.openGroup));
});

document.getElementById('group-board-back-btn').addEventListener('click', showGroupsList);
document.getElementById('delete-group-btn').addEventListener('click', deleteCurrentGroup);

document.getElementById('add-member-form').addEventListener('submit', (e) => {
    e.preventDefault();
    const username = document.getElementById('member-username').value.trim();
    if (username) {
        addGroupMember(username);
    }
});

document.getElementById('members-container').addEventListener('click', (e) => {
    const btn = e.target.closest('[data-remove-member]');
    if (!btn) return;
    removeGroupMember(Number(btn.dataset.removeMember));
});

document.getElementById('add-group-task-btn').addEventListener('click', showGroupTaskCreate);
document.getElementById('group-task-create-back-btn').addEventListener('click', showGroupBoard);
document.getElementById('cancel-group-task-create').addEventListener('click', showGroupBoard);
document.getElementById('group-task-detail-back-btn').addEventListener('click', showGroupBoard);

document.getElementById('create-group-task-form').addEventListener('submit', (e) => {
    e.preventDefault();
    const title = document.getElementById('group-task-title').value.trim();
    const description = document.getElementById('group-task-description').value.trim();
    const assignee = document.getElementById('group-task-assignee').value.trim();
    if (title) {
        createGroupTask(title, description, assignee);
    }
});

document.getElementById('group-tasks-container').addEventListener('click', (e) => {
    const open = e.target.closest('[data-open-group-task]');
    if (!open) return;
    showGroupTaskDetail(Number(open.dataset.openGroupTask));
});

// ============================================================
// Запуск
// ============================================================
(async function init() {
    await loadCurrentUser();
    setActiveTab('personal');
})();
