document.addEventListener('DOMContentLoaded', () => {
  const authOverlay = document.getElementById('authOverlay');
  const authSecretInput = document.getElementById('authSecretInput');
  const authSubmitBtn = document.getElementById('authSubmitBtn');
  const authStatus = document.getElementById('authStatus');

  const adminLayout = document.getElementById('adminLayout');
  const changeSecretBtn = document.getElementById('changeSecretBtn');

  const statusFilter = document.getElementById('statusFilter');
  const refreshBtn = document.getElementById('refreshBtn');

  const exportFrom = document.getElementById('exportFrom');
  const exportTo = document.getElementById('exportTo');
  const exportBtn = document.getElementById('exportBtn');

  const issuesTableBody = document.getElementById('issuesTableBody');
  const issuesCounter = document.getElementById('issuesCounter');
  const listStatus = document.getElementById('listStatus');
  const emptyState = document.getElementById('emptyState');

  const detailsTitle = document.getElementById('detailsTitle');
  const detailsStatusPill = document.getElementById('detailsStatusPill');
  const detailsBody = document.getElementById('detailsBody');

  const state = {
    token: '',
    issues: [],
    selectedId: null,
    loading: false,
  };

  function setToken(token) {
    state.token = (token || '').trim();
    if (state.token) {
      localStorage.setItem('adminSecret112', state.token);
    } else {
      localStorage.removeItem('adminSecret112');
    }
  }

  function showAuthOverlay() {
    if (authOverlay) authOverlay.style.display = 'flex';
    if (adminLayout) adminLayout.style.display = 'none';
  }

  function hideAuthOverlay() {
    if (authOverlay) authOverlay.style.display = 'none';
    if (adminLayout) adminLayout.style.display = 'grid';
  }

  function showAuthStatus(message, type = 'info') {
    if (!authStatus) return;
    authStatus.textContent = message || '';
    authStatus.dataset.type = type;
  }

  function setListStatus(message, type = 'info') {
    if (!listStatus) return;
    listStatus.textContent = message || '';
    listStatus.dataset.type = type;
  }

  function formatDate(iso) {
    if (!iso) return '';
    try {
      const d = new Date(iso);
      if (Number.isNaN(d.getTime())) return iso;
      return d.toLocaleString('ru-RU', {
        year: 'numeric',
        month: '2-digit',
        day: '2-digit',
        hour: '2-digit',
        minute: '2-digit',
      });
    } catch {
      return iso;
    }
  }

  function trimText(text, maxLen) {
    if (!text) return '';
    const t = String(text).trim();
    if (t.length <= maxLen) return t;
    return t.slice(0, maxLen - 1) + '…';
  }

  function statusToClass(status) {
    switch (status) {
      case 'Новая':
        return 'status-pill-new';
      case 'В обработке':
        return 'status-pill-inprogress';
      case 'Завершено':
        return 'status-pill-done';
      case 'Отклонено':
        return 'status-pill-rejected';
      default:
        return 'status-pill-muted';
    }
  }

  function clearDetails() {
    detailsTitle.textContent = 'Не выбрана';
    detailsStatusPill.textContent = '—';
    detailsStatusPill.className = 'status-pill status-pill-muted';
    detailsBody.className = 'admin-details-body admin-details-body-empty';
    detailsBody.innerHTML = '<p class="admin-details-placeholder">Выберите обращение в списке слева, чтобы посмотреть детали и изменить статус.</p>';
  }

  function renderIssues() {
    issuesTableBody.innerHTML = '';
    if (!state.issues.length) {
      emptyState.style.display = 'flex';
      issuesCounter.textContent = 'Заявок не найдено';
      return;
    }
    emptyState.style.display = 'none';
    issuesCounter.textContent = `Заявок: ${state.issues.length}`;

    const fragment = document.createDocumentFragment();

    state.issues.forEach((issue) => {
      const tr = document.createElement('tr');
      tr.className = 'admin-table-row';
      tr.dataset.id = String(issue.id);

      const text = issue.text || '';
      const district = issue.district || '—';
      const category = issue.category || '—';

      tr.innerHTML = `
        <td class="cell-id">#${issue.id}</td>
        <td><span class="status-pill ${statusToClass(issue.status)}">${issue.status}</span></td>
        <td>${district}</td>
        <td>${category}</td>
        <td class="cell-text">${trimText(text, 80)}</td>
        <td>${formatDate(issue.created_at)}</td>
      `;

      tr.addEventListener('click', () => {
        selectIssue(issue.id);
      });

      fragment.appendChild(tr);
    });

    issuesTableBody.appendChild(fragment);
  }

  function escapeHTML(str) {
    return String(str)
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;')
      .replace(/'/g, '&#39;');
  }

  async function pingToken(token) {
    if (!token) {
      showAuthStatus('Введите admin_secret.', 'warning');
      return false;
    }
    try {
      const resp = await fetch('/admin/ping?token=' + encodeURIComponent(token), {
        method: 'GET',
        cache: 'no-store',
      });
      if (resp.ok) {
        return true;
      }
      if (resp.status === 401) {
        showAuthStatus('Неверный admin_secret.', 'error');
        return false;
      }
      showAuthStatus('Ошибка проверки доступа: ' + resp.status, 'error');
      return false;
    } catch (e) {
      console.error(e);
      showAuthStatus('Сетевая ошибка при проверке доступа.', 'error');
      return false;
    }
  }

  async function handleAuthSubmit() {
    const secret = authSecretInput.value || '';
    showAuthStatus('Проверка…', 'info');
    const ok = await pingToken(secret);
    if (!ok) {
      return;
    }
    setToken(secret);
    showAuthStatus('Доступ разрешён.', 'success');
    hideAuthOverlay();
    await fetchIssues();
  }

  
  function normalizeIssue(raw) {
    if (!raw || typeof raw !== 'object') return {};

    const id = raw.id ?? raw.ID ?? null;
    const status = raw.status ?? raw.Status ?? '';
    const district = raw.district ?? raw.District ?? null;
    const category = raw.category ?? raw.Category ?? null;
    const text = raw.text ?? raw.Text ?? '';
    const latitude = raw.latitude ?? raw.Latitude ?? null;
    const longitude = raw.longitude ?? raw.Longitude ?? null;

    const created_at = raw.created_at ?? raw.createdAt ?? raw.CreatedAt ?? null;
    const updated_at = raw.updated_at ?? raw.updatedAt ?? raw.UpdatedAt ?? null;

    return {
      id,
      status,
      district,
      category,
      text,
      latitude,
      longitude,
      created_at,
      updated_at,
    };
  }

async function fetchIssues() {
    if (!state.token) {
      setListStatus('Укажите admin_secret для загрузки заявок.', 'warning');
      state.issues = [];
      renderIssues();
      clearDetails();
      return;
    }

    state.loading = true;
    setListStatus('Загрузка заявок…', 'info');
    issuesTableBody.innerHTML = '';
    emptyState.style.display = 'none';

    const status = statusFilter.value;
    const params = new URLSearchParams();
    params.set('token', state.token);
    if (status && status !== 'all') {
      params.set('status', status);
    }

    try {
      const resp = await fetch('/admin/issues?' + params.toString(), { cache: 'no-store' });
      if (!resp.ok) {
        if (resp.status === 401) {
          setListStatus('Неверный admin_secret. Попробуйте войти заново.', 'error');
          showAuthOverlay();
          return;
        }
        const text = await resp.text();
        setListStatus('Ошибка загрузки: ' + (text || resp.status), 'error');
        state.issues = [];
        renderIssues();
        clearDetails();
        return;
      }

      const data = await resp.json();
      if (!Array.isArray(data)) {
        setListStatus('Некорректный формат ответа от сервера.', 'error');
        state.issues = [];
        renderIssues();
        clearDetails();
        return;
      }

      state.issues = data.map(normalizeIssue);
      setListStatus('Заявки успешно загружены.', 'success');
      renderIssues();
      clearDetails();
    } catch (e) {
      console.error(e);
      setListStatus('Сетевая ошибка при загрузке заявок.', 'error');
      state.issues = [];
      renderIssues();
      clearDetails();
    } finally {
      state.loading = false;
    }
  }

  function selectIssue(id) {
    const issue = state.issues.find((x) => x.id === id);
    if (!issue) return;
    state.selectedId = id;

    detailsTitle.textContent = `#${issue.id}`;
    detailsStatusPill.textContent = issue.status || '—';
    detailsStatusPill.className = 'status-pill ' + statusToClass(issue.status);

    const district = issue.district || 'Не указан';
    const category = issue.category || 'Не указана';
    const text = issue.text || '';
    const created = formatDate(issue.created_at);
    const updated = formatDate(issue.updated_at);

    let locationBlock = '';
    if (issue.latitude && issue.longitude) {
      const lat = issue.latitude;
      const lng = issue.longitude;
      const yandexUrl = `https://yandex.ru/maps/?pt=${lng},${lat}&z=18&l=map`;
      const googleUrl = `https://www.google.com/maps?q=${lat},${lng}`;
      locationBlock = `
        <div class="admin-details-section">
          <h3 class="admin-details-section-title">Геолокация</h3>
          <p class="admin-details-text">
            Широта: <strong>${lat}</strong><br/>
            Долгота: <strong>${lng}</strong>
          </p>
          <div class="admin-links">
            <a href="${yandexUrl}" target="_blank" rel="noopener noreferrer">Открыть в Яндекс.Картах</a>
            <a href="${googleUrl}" target="_blank" rel="noopener noreferrer">Открыть в Google Maps</a>
          </div>
        </div>
      `;
    }

    detailsBody.className = 'admin-details-body';
    detailsBody.innerHTML = `
      <div class="admin-details-section">
        <h3 class="admin-details-section-title">Основная информация</h3>
        <p class="admin-details-meta">
          Район: <strong>${district}</strong> · Категория: <strong>${category}</strong>
        </p>
        <p class="admin-details-meta">
          Создано: <strong>${created}</strong>${updated ? ' · Обновлено: <strong>' + updated + '</strong>' : ''}
        </p>
      </div>

      <div class="admin-details-section">
        <h3 class="admin-details-section-title">Описание обращения</h3>
        <p class="admin-details-text">${text ? escapeHTML(text).replace(/\n/g, '<br/>') : '<span class="muted">Текст не указан</span>'}</p>
      </div>

      ${locationBlock}

      <div class="admin-details-section" id="attachmentsSection">
        <h3 class="admin-details-section-title">Вложения</h3>
        <div id="attachmentsContainer" class="attachments-grid">
          <p class="admin-details-text muted">Загрузка вложений…</p>
        </div>
      </div>

      <div class="admin-details-section">
        <h3 class="admin-details-section-title">Изменить статус</h3>
        <div class="status-buttons-row">
          <button class="status-btn status-btn-new" data-status="Новая">Новая</button>
          <button class="status-btn status-btn-inprogress" data-status="В обработке">В обработке</button>
          <button class="status-btn status-btn-done" data-status="Завершено">Завершено</button>
          <button class="status-btn status-btn-rejected" data-status="Отклонено">Отклонено</button>
        </div>
        <label class="admin-label" for="statusComment">Комментарий администратора (необязательно)</label>
        <textarea id="statusComment" class="field admin-input admin-textarea" rows="3" placeholder="Кратко опишите, что сделано по обращению…"></textarea>
        <div class="status-comment-row">
          <button id="sendCommentBtn" type="button" class="ghost-button admin-ghost-button status-comment-btn">
            Отправить комментарий пользователю
          </button>
        </div>
        <p id="statusResult" class="admin-hint"></p>
      </div>
    `;

    const buttons = detailsBody.querySelectorAll('.status-btn');
    const commentInput = detailsBody.querySelector('#statusComment');
    const statusResult = detailsBody.querySelector('#statusResult');

    buttons.forEach((btn) => {
      btn.addEventListener('click', async () => {
        const newStatus = btn.dataset.status;
        if (!newStatus) return;
        statusResult.textContent = 'Отправка…';
        statusResult.dataset.type = 'info';

        try {
          const resp = await fetch('/admin/status', {
            method: 'POST',
            headers: {
              'Content-Type': 'application/json',
            },
            body: JSON.stringify({
              token: state.token,
              issue_id: issue.id,
              status: newStatus,
              comment: commentInput.value ? commentInput.value : null,
              admin_tg: null,
            }),
          });

          if (!resp.ok) {
            if (resp.status === 401) {
              statusResult.textContent = 'Неверный admin_secret. Попробуйте войти заново.';
              statusResult.dataset.type = 'error';
              showAuthOverlay();
              return;
            }
            const text = await resp.text();
            statusResult.textContent = 'Ошибка: ' + (text || resp.status);
            statusResult.dataset.type = 'error';
            return;
          }

          statusResult.textContent = 'Статус обновлён.';
          statusResult.dataset.type = 'success';

          issue.status = newStatus;
          renderIssues();
          selectIssue(issue.id);
        } catch (e) {
          console.error(e);
          statusResult.textContent = 'Сетевая ошибка при обновлении статуса.';
          statusResult.dataset.type = 'error';
        }
      });
    });


    const sendCommentBtn = detailsBody.querySelector('#sendCommentBtn');
    if (sendCommentBtn) {
      sendCommentBtn.addEventListener('click', async () => {
        if (!state.token) {
          statusResult.textContent = 'admin_secret не задан.';
          statusResult.dataset.type = 'warning';
          showAuthOverlay();
          return;
        }
        const text = (commentInput.value || '').trim();
        if (!text) {
          statusResult.textContent = 'Введите текст комментария.';
          statusResult.dataset.type = 'warning';
          return;
        }
        statusResult.textContent = 'Отправка комментария…';
        statusResult.dataset.type = 'info';
        try {
          const resp = await fetch('/admin/comment', {
            method: 'POST',
            headers: {
              'Content-Type': 'application/json',
            },
            body: JSON.stringify({
              token: state.token,
              issue_id: issue.id,
              text,
            }),
          });
          if (!resp.ok) {
            if (resp.status === 401) {
              statusResult.textContent = 'Неверный admin_secret. Попробуйте войти заново.';
              statusResult.dataset.type = 'error';
              showAuthOverlay();
              return;
            }
            const textResp = await resp.text();
            statusResult.textContent = 'Ошибка отправки комментария: ' + (textResp || resp.status);
            statusResult.dataset.type = 'error';
            return;
          }
          statusResult.textContent = 'Комментарий отправлен пользователю.';
          statusResult.dataset.type = 'success';
          commentInput.value = '';
        } catch (e) {
          console.error(e);
          statusResult.textContent = 'Сетевая ошибка при отправке комментария.';
          statusResult.dataset.type = 'error';
        }
      });
    }

        loadAttachments(issue.id);
  }

  async function loadAttachments(issueId) {
    const container = detailsBody.querySelector('#attachmentsContainer');
    if (!container) return;

    container.innerHTML = '<p class="admin-details-text muted">Загрузка вложений…</p>';

    if (!state.token) {
      container.innerHTML = '<p class="admin-details-text muted">admin_secret не задан.</p>';
      return;
    }

    try {
      const resp = await fetch(`/admin/issues/${issueId}/attachments?token=${encodeURIComponent(state.token)}`, { cache: 'no-store' });
      if (!resp.ok) {
        if (resp.status === 401) {
          container.innerHTML = '<p class="admin-details-text error">Неверный admin_secret.</p>';
          showAuthOverlay();
          return;
        }
        container.innerHTML = '<p class="admin-details-text error">Ошибка загрузки вложений.</p>';
        return;
      }

      const data = await resp.json();
      if (!Array.isArray(data) || !data.length) {
        container.innerHTML = '<p class="admin-details-text muted">Нет вложений.</p>';
        return;
      }

      container.innerHTML = '';
      data.forEach((att) => {
        const item = document.createElement('div');
        item.className = 'attachment-item';
        const path = att.local_path || att.LocalPath || '';
        const type = att.file_type || att.FileType || '';

        const url = path.startsWith('/') ? path : '/' + path;

        if (type.startsWith('image/')) {
          item.innerHTML = `
            <a href="${url}" target="_blank" rel="noopener noreferrer">
              <img src="${url}" alt="Вложение" />
            </a>
          `;
        } else if (type.startsWith('video/')) {
          item.innerHTML = `
            <video src="${url}" controls></video>
          `;
        } else {
          const label = type || 'файл';
          item.innerHTML = `
            <a href="${url}" target="_blank" rel="noopener noreferrer" class="attachment-link">
              Скачать ${label}
            </a>
          `;
        }

        container.appendChild(item);
      });
    } catch (e) {
      console.error(e);
      container.innerHTML = '<p class="admin-details-text error">Сетевая ошибка при загрузке вложений.</p>';
    }
  }

  function initFromStorage() {
    const saved = localStorage.getItem('adminSecret112');
    if (!saved) {
      showAuthOverlay();
      showAuthStatus('Введите admin_secret, чтобы войти.', 'info');
      return;
    }
    authSecretInput.value = saved;
    setToken(saved);
    hideAuthOverlay();
    fetchIssues();
  }

  // Обработчики
  if (authSubmitBtn) {
    authSubmitBtn.addEventListener('click', (e) => {
      e.preventDefault();
      handleAuthSubmit();
    });
  }

  if (authSecretInput) {
    authSecretInput.addEventListener('keydown', (e) => {
      if (e.key === 'Enter') {
        e.preventDefault();
        handleAuthSubmit();
      }
    });
  }

  if (changeSecretBtn) {
    changeSecretBtn.addEventListener('click', () => {
      setToken('');
      if (authSecretInput) authSecretInput.value = '';
      showAuthStatus('Введите новый admin_secret.', 'info');
      showAuthOverlay();
    });
  }

  if (statusFilter) {
    statusFilter.addEventListener('change', () => {
      fetchIssues();
    });
  }

  if (refreshBtn) {
    refreshBtn.addEventListener('click', () => {
      fetchIssues();
    });
  }

  if (exportBtn) {
    exportBtn.addEventListener('click', () => {
      if (!state.token) {
        showAuthStatus('Сначала введите admin_secret.', 'warning');
        showAuthOverlay();
        return;
      }
      const from = exportFrom.value;
      const to = exportTo.value;
      if (!from || !to) {
        showAuthStatus('Укажите период для экспорта.', 'warning');
        return;
      }
      const params = new URLSearchParams();
      params.set('token', state.token);
      params.set('from', from);
      params.set('to', to);
      const url = '/export?' + params.toString();
      window.open(url, '_blank');
    });
  }

  initFromStorage();
});
