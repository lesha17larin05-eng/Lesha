// Custom element <lesson-editor> for admin course lesson editing.
// HTML editor with toolbar + live preview, video upload, save via API.
// Content is stored in the lessons.content_md field as raw HTML — the public
// renderer (marked.parse) passes HTML through as-is.

class LessonEditor extends HTMLElement {
  connectedCallback() {
    const courseId = this.dataset.courseId || '';
    const modules = JSON.parse(this.dataset.modules || '[]');
    this.innerHTML = `
      <style>
        .le-modal {
          position: fixed; inset: 0; background: rgba(0,0,0,.5); z-index: 100;
          display: flex; align-items: flex-start; justify-content: center;
          padding: 40px 20px; overflow: auto;
        }
        .le-modal[hidden] { display: none; }
        #le-modal { padding: 0; }
        .le-card {
          background: #fff; padding: 24px 32px;
          width: 100%; max-width: none; min-height: 100vh;
          border-radius: 0;
        }
        .le-card form.form { max-width: none; width: 100%; }
        .le-head { display:flex; justify-content:space-between; align-items:center; gap:10px; }
        .le-row { display: grid; gap: 12px; }
        .le-row.cols-2 { grid-template-columns: 2fr 1fr; }
        .le-row.cols-3 { grid-template-columns: 2fr 1fr 1fr; }
        .le-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 18px; margin-top: 8px; }
        .le-toolbar { display: flex; gap: 6px; flex-wrap: wrap; margin-bottom: 6px; }
        .le-toolbar button {
          font-size: 12px; padding: 4px 10px; border-radius: 6px; border: 1px solid #d6d2c8;
          background: #fff; cursor: pointer; font-family: inherit;
        }
        .le-toolbar button:hover { background: #f7f4ec; }
        textarea.le-html {
          width: 100%; min-height: 60vh; font-family: ui-monospace, monospace; font-size: 13px;
          padding: 12px; border: 1px solid #d6d2c8; border-radius: 8px; background: #fff; line-height: 1.5;
          resize: vertical;
        }
        .le-preview {
          border: 1px solid #d6d2c8; border-radius: 8px; padding: 18px; background: #fff;
          min-height: 60vh; overflow: auto;
        }
        .le-preview h2, .le-preview h3 { font-family: 'Playfair Display', serif; margin: 16px 0 8px; }
        .le-preview p { line-height: 1.7; margin-bottom: 10px; }
        .le-help { font-size: 12px; color: #777; margin-top: 4px; }
        @media (max-width: 900px) { .le-grid { grid-template-columns: 1fr; } }
      </style>
      <div class="le-modal" id="le-modal" hidden>
        <div class="le-card">
          <div class="le-head">
            <h2 id="le-title" style="margin:0;">Урок</h2>
            <button type="button" class="btn secondary" id="le-close" style="padding:4px 12px;">×</button>
          </div>
          <form class="form" id="le-form" style="margin-top:14px; gap:12px;">
            <input type="hidden" name="id" />
            <div class="le-row cols-2">
              <div><label class="label">Название</label><input class="input" name="title" required /></div>
              <div><label class="label">Slug</label><input class="input" name="slug" required pattern="[a-z0-9-]+" /></div>
            </div>
            <div class="le-row cols-3">
              <div>
                <label class="label">Модуль</label>
                <select class="input" name="module_id">
                  <option value="">— без модуля —</option>
                  ${modules.map((m) => `<option value="${m.id}">${escapeHtml(m.title)}</option>`).join('')}
                </select>
              </div>
              <div><label class="label">Порядок</label><input class="input" name="sort_order" type="number" value="1" /></div>
              <div><label class="label">Длит. (сек)</label><input class="input" name="duration_sec" type="number" value="0" /></div>
            </div>
            <label><input type="checkbox" name="is_preview" /> Превью (доступен без оплаты)</label>
            <div>
              <label class="label">Содержание (HTML)</label>
              <div class="le-toolbar">
                <button type="button" data-tag="h2">H2</button>
                <button type="button" data-tag="h3">H3</button>
                <button type="button" data-tag="p">P</button>
                <button type="button" data-tag="strong">B</button>
                <button type="button" data-tag="em">I</button>
                <button type="button" data-tag="ul">UL</button>
                <button type="button" data-tag="li">LI</button>
                <button type="button" data-tag="blockquote">QUOTE</button>
                <button type="button" data-tag="a">LINK</button>
              </div>
              <div class="le-grid">
                <textarea class="le-html" name="content_md" placeholder="<p>Текст урока...</p>"></textarea>
                <div>
                  <div class="le-help">Предпросмотр</div>
                  <div class="le-preview" id="le-preview"></div>
                </div>
              </div>
              <div class="le-help">HTML сохраняется в поле content_md и рендерится через marked на странице урока.</div>
            </div>
            <div>
              <label class="label">Видео</label>
              <div style="display:flex; align-items:center; gap:10px; flex-wrap:wrap;">
                <input class="input" name="video_id" placeholder="UUID видео или загрузите файл" style="flex:1; min-width:240px;" />
                <input type="file" id="le-video-upload" accept="video/*" />
                <span id="le-upload-status" class="muted"></span>
              </div>
              <div class="le-help">После загрузки файла появится UUID. ffmpeg нарежет HLS в фоне (status=processing → ready).</div>
            </div>
            <div style="display:flex; gap:10px;">
              <button class="btn" type="submit">Сохранить</button>
              <button class="btn secondary" type="button" id="le-cancel">Отмена</button>
              <span class="muted" id="le-msg"></span>
            </div>
          </form>
        </div>
      </div>
    `;

    const modal = this.querySelector('#le-modal');
    const form = this.querySelector('#le-form');
    const titleEl = this.querySelector('#le-title');
    const ta = form.querySelector('textarea[name="content_md"]');
    const preview = this.querySelector('#le-preview');
    const msg = this.querySelector('#le-msg');
    const uploadStatus = this.querySelector('#le-upload-status');

    const updatePreview = () => { preview.innerHTML = ta.value; };
    ta.addEventListener('input', updatePreview);

    this.querySelectorAll('.le-toolbar button').forEach((btn) => {
      btn.addEventListener('click', () => {
        const tag = btn.dataset.tag;
        const start = ta.selectionStart;
        const end = ta.selectionEnd;
        const sel = ta.value.slice(start, end) || (tag === 'a' ? 'текст ссылки' : 'текст');
        let snippet;
        if (tag === 'a') {
          const url = prompt('URL ссылки:', 'https://');
          if (!url) return;
          snippet = `<a href="${url}">${sel}</a>`;
        } else if (tag === 'ul') {
          snippet = `<ul>\n  <li>${sel}</li>\n</ul>`;
        } else {
          snippet = `<${tag}>${sel}</${tag}>`;
        }
        ta.setRangeText(snippet, start, end, 'end');
        updatePreview();
        ta.focus();
      });
    });

    const close = () => { modal.hidden = true; msg.textContent = ''; };
    this.querySelector('#le-close').addEventListener('click', close);
    this.querySelector('#le-cancel').addEventListener('click', close);
    modal.addEventListener('click', (e) => { if (e.target === modal) close(); });

    this.open = (lesson) => {
      titleEl.textContent = lesson ? `Редактировать: ${lesson.title}` : 'Новый урок';
      form.elements.id.value = lesson?.id || '';
      form.elements.title.value = lesson?.title || '';
      form.elements.slug.value = lesson?.slug || '';
      form.elements.module_id.value = lesson?.module_id || '';
      form.elements.sort_order.value = lesson?.sort_order ?? 1;
      form.elements.duration_sec.value = lesson?.duration_sec ?? 0;
      form.elements.is_preview.checked = !!lesson?.is_preview;
      form.elements.content_md.value = lesson?.content_md || '';
      form.elements.video_id.value = lesson?.video_id || '';
      uploadStatus.textContent = '';
      msg.textContent = '';
      updatePreview();
      modal.hidden = false;
    };

    form.addEventListener('submit', async (e) => {
      e.preventDefault();
      const fd = new FormData(form);
      const id = fd.get('id');
      const body = {
        course_id: courseId,
        module_id: fd.get('module_id') || null,
        title: fd.get('title'), slug: fd.get('slug'),
        content_md: fd.get('content_md') || '',
        video_id: fd.get('video_id') || null,
        duration_sec: parseInt(fd.get('duration_sec') || '0'),
        sort_order: parseInt(fd.get('sort_order') || '1'),
        is_preview: fd.has('is_preview'),
      };
      const url = id ? `/api/admin/lessons/${id}` : '/api/admin/lessons';
      const method = id ? 'PATCH' : 'POST';
      msg.textContent = 'Сохраняю…';
      const r = await fetch(url, { method, headers: { 'content-type': 'application/json' }, body: JSON.stringify(body) });
      const j = await r.json().catch(() => ({}));
      if (r.ok) { location.reload(); }
      else { msg.textContent = j.error || 'Ошибка'; }
    });

    this.querySelector('#le-video-upload').addEventListener('change', async (e) => {
      const file = e.target.files?.[0];
      if (!file) return;
      uploadStatus.textContent = `Загрузка ${(file.size / 1048576).toFixed(1)} MB...`;
      const fd = new FormData();
      fd.append('file', file);
      const csrfMatch = document.cookie.match(/(?:^|;\s*)csrf=([^;]+)/);
      const r = await fetch('/api/admin/videos/upload', {
        method: 'POST', body: fd,
        headers: { 'X-CSRF-Token': csrfMatch ? csrfMatch[1] : '' },
        credentials: 'include',
      });
      const j = await r.json().catch(() => ({}));
      if (r.ok && j.id) {
        form.elements.video_id.value = j.id;
        uploadStatus.textContent = `✅ Загружено. Status: ${j.status}`;
      } else {
        uploadStatus.textContent = '❌ ' + (j.error || 'ошибка');
      }
    });
  }
}

function escapeHtml(s) {
  return String(s).replace(/[&<>"']/g, (c) => ({ '&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;' }[c]));
}

customElements.define('lesson-editor', LessonEditor);

// --- Module modal element (rename + reorder) ---

class ModuleEditor extends HTMLElement {
  connectedCallback() {
    const courseId = this.dataset.courseId || '';
    this.innerHTML = `
      <div class="le-modal" id="me-modal" hidden>
        <div style="background:#fff; border-radius:12px; padding:28px; max-width:480px; width:100%;">
          <div style="display:flex; justify-content:space-between; align-items:center;">
            <h2 id="me-title" style="margin:0;">Модуль</h2>
            <button type="button" class="btn secondary" id="me-close" style="padding:4px 12px;">×</button>
          </div>
          <form class="form" id="me-form" style="margin-top:14px;">
            <input type="hidden" name="id" />
            <div><label class="label">Название</label><input class="input" name="title" required /></div>
            <div><label class="label">Порядок</label><input class="input" name="sort_order" type="number" value="1" /></div>
            <div style="display:flex; gap:10px;">
              <button class="btn" type="submit">Сохранить</button>
              <button class="btn secondary" type="button" id="me-cancel">Отмена</button>
              <span class="muted" id="me-msg"></span>
            </div>
          </form>
        </div>
      </div>
    `;
    const modal = this.querySelector('#me-modal');
    const form = this.querySelector('#me-form');
    const titleEl = this.querySelector('#me-title');
    const msg = this.querySelector('#me-msg');
    const close = () => { modal.hidden = true; msg.textContent = ''; };
    this.querySelector('#me-close').addEventListener('click', close);
    this.querySelector('#me-cancel').addEventListener('click', close);
    modal.addEventListener('click', (e) => { if (e.target === modal) close(); });

    this.open = (mod) => {
      titleEl.textContent = mod ? `Редактировать модуль` : 'Новый модуль';
      form.elements.id.value = mod?.id || '';
      form.elements.title.value = mod?.title || '';
      form.elements.sort_order.value = mod?.sort_order ?? 1;
      msg.textContent = '';
      modal.hidden = false;
    };

    form.addEventListener('submit', async (e) => {
      e.preventDefault();
      const fd = new FormData(form);
      const id = fd.get('id');
      const body = id
        ? { title: fd.get('title'), sort_order: parseInt(fd.get('sort_order') || '1') }
        : { course_id: courseId, title: fd.get('title'), sort_order: parseInt(fd.get('sort_order') || '1') };
      const url = id ? `/api/admin/modules/${id}` : '/api/admin/modules';
      const method = id ? 'PATCH' : 'POST';
      msg.textContent = 'Сохраняю…';
      const r = await fetch(url, { method, headers: { 'content-type': 'application/json' }, body: JSON.stringify(body) });
      const j = await r.json().catch(() => ({}));
      if (r.ok) location.reload();
      else msg.textContent = j.error || 'Ошибка';
    });
  }
}
customElements.define('module-editor', ModuleEditor);
