// Custom element <article-editor> for admin blog create/edit.
// Renders a form + live HTML preview, handles save/publish via API.

class ArticleEditor extends HTMLElement {
  connectedCallback() {
    const mode = this.dataset.mode || 'create';
    const id = this.dataset.id || '';
    let initial = {
      slug: '', title: '', tag: '', excerpt: '',
      cover_image_url: '', content_html: '', reading_minutes: 4,
      is_published: false, published_at: '',
    };
    if (this.dataset.article) {
      try {
        const parsed = JSON.parse(this.dataset.article);
        initial = { ...initial, ...parsed };
      } catch (e) {}
    }
    this.innerHTML = `
      <style>
        .ed-grid { display:grid; grid-template-columns: 1fr 1fr; gap:24px; margin-top:20px; }
        .ed-grid > div { display:flex; flex-direction:column; }
        .ed-row { display:grid; grid-template-columns: 1fr 1fr; gap: 12px; }
        .ed-row .input { width:100%; }
        .ed-help { font-size:12px; color:#777; margin-top:4px; }
        .ed-toolbar { display:flex; gap:6px; margin-bottom:6px; flex-wrap:wrap; }
        .ed-toolbar button {
          font-size: 12px; padding: 4px 10px; border-radius: 6px; border: 1px solid #d6d2c8;
          background: #fff; cursor: pointer; font-family: inherit;
        }
        .ed-toolbar button:hover { background: #f7f4ec; }
        textarea.ed-html {
          width:100%; min-height:480px; font-family: ui-monospace, monospace; font-size:13px;
          padding:14px; border:1px solid #d6d2c8; border-radius:8px; background:#fff; line-height:1.5;
        }
        .ed-preview {
          border:1px solid #d6d2c8; border-radius:8px; padding:24px; background:#fff;
          min-height:480px; max-height: 700px; overflow:auto;
        }
        .ed-preview h2 { font-family: 'Playfair Display', serif; margin: 24px 0 12px; }
        .ed-preview p { line-height: 1.7; margin-bottom: 14px; }
        .ed-actions { display:flex; gap:10px; align-items:center; margin-top:20px; flex-wrap:wrap; }
        .ed-status { color: #1a7a3a; font-size: 14px; }
        .ed-error { color: #a23c0d; font-size: 14px; }
        @media (max-width: 900px) { .ed-grid { grid-template-columns: 1fr; } }
      </style>
      <form class="form" style="max-width:none; gap:14px;" id="ed-form">
        <div class="ed-row">
          <div>
            <label class="label">Slug (URL)</label>
            <input class="input" name="slug" required pattern="[a-z0-9-]+" placeholder="my-article" />
            <div class="ed-help">только латиница, цифры и дефис</div>
          </div>
          <div>
            <label class="label">Тег</label>
            <input class="input" name="tag" placeholder="Начало пути" />
          </div>
        </div>
        <div>
          <label class="label">Заголовок</label>
          <input class="input" name="title" required placeholder="Заголовок статьи" />
        </div>
        <div>
          <label class="label">Краткое описание (excerpt)</label>
          <textarea class="input" name="excerpt" rows="2" placeholder="2-3 предложения, показывается в карточке и в meta-description"></textarea>
        </div>
        <div class="ed-row">
          <div>
            <label class="label">URL обложки (опционально)</label>
            <input class="input" name="cover_image_url" type="url" placeholder="https://..." />
          </div>
          <div>
            <label class="label">Минут на чтение</label>
            <input class="input" name="reading_minutes" type="number" min="1" max="60" value="4" />
          </div>
        </div>
        <div>
          <label class="label">Содержание (HTML)</label>
          <div class="ed-toolbar">
            <button type="button" data-tag="h2">H2</button>
            <button type="button" data-tag="h3">H3</button>
            <button type="button" data-tag="p">P</button>
            <button type="button" data-tag="strong">B</button>
            <button type="button" data-tag="em">I</button>
            <button type="button" data-tag="ul">UL</button>
            <button type="button" data-tag="li">LI</button>
            <button type="button" data-tag="blockquote">QUOTE</button>
            <button type="button" data-tag="a">LINK</button>
            <button type="button" data-pull="1">PULL</button>
          </div>
          <div class="ed-grid">
            <div>
              <textarea class="ed-html" name="content_html" placeholder="<p>Текст статьи...</p>"></textarea>
            </div>
            <div>
              <div class="ed-help">Предпросмотр</div>
              <div class="ed-preview" id="ed-preview"></div>
            </div>
          </div>
        </div>
        <div>
          <label><input type="checkbox" name="is_published" /> Опубликовать сразу</label>
        </div>
        <div class="ed-actions">
          <button class="btn" type="submit">${mode === 'edit' ? 'Сохранить' : 'Создать'}</button>
          <a class="btn secondary" href="/admin/blog">Отмена</a>
          <span class="ed-status" id="ed-status"></span>
          <span class="ed-error" id="ed-error"></span>
        </div>
      </form>
    `;

    const form = this.querySelector('#ed-form');
    const ta = form.querySelector('textarea[name="content_html"]');
    const preview = this.querySelector('#ed-preview');
    const statusEl = this.querySelector('#ed-status');
    const errorEl = this.querySelector('#ed-error');

    // hydrate initial values
    for (const [k, v] of Object.entries(initial)) {
      const el = form.elements.namedItem(k);
      if (!el) continue;
      if (el.type === 'checkbox') el.checked = !!v;
      else el.value = v == null ? '' : v;
    }

    const updatePreview = () => { preview.innerHTML = ta.value; };
    ta.addEventListener('input', updatePreview);
    updatePreview();

    // Toolbar: insert simple HTML wrappers around selection.
    this.querySelectorAll('.ed-toolbar button[data-tag]').forEach((btn) => {
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
    this.querySelector('.ed-toolbar button[data-pull]').addEventListener('click', () => {
      const start = ta.selectionStart;
      const sel = ta.value.slice(ta.selectionStart, ta.selectionEnd) || 'выделенная мысль';
      ta.setRangeText(`<div class="article-pull">${sel}</div>`, start, ta.selectionEnd, 'end');
      updatePreview();
      ta.focus();
    });

    form.addEventListener('submit', async (e) => {
      e.preventDefault();
      errorEl.textContent = '';
      statusEl.textContent = 'Сохраняю…';
      const fd = new FormData(form);
      const body = {
        slug: fd.get('slug'),
        title: fd.get('title'),
        tag: fd.get('tag') || '',
        excerpt: fd.get('excerpt') || '',
        cover_image_url: fd.get('cover_image_url') || '',
        content_html: fd.get('content_html') || '',
        reading_minutes: parseInt(fd.get('reading_minutes') || '0', 10),
        is_published: fd.get('is_published') === 'on',
        sort_order: 0,
      };
      const url = mode === 'edit' ? `/api/admin/articles/${id}` : '/api/admin/articles';
      const method = mode === 'edit' ? 'PATCH' : 'POST';
      try {
        const r = await fetch(url, {
          method,
          headers: { 'content-type': 'application/json' },
          body: JSON.stringify(body),
        });
        const j = await r.json().catch(() => ({}));
        if (!r.ok) {
          errorEl.textContent = j.error || `Ошибка ${r.status}`;
          statusEl.textContent = '';
          return;
        }
        statusEl.textContent = 'Сохранено ✓';
        if (mode === 'create' && j.id) {
          location.href = `/admin/blog/${j.id}`;
        }
      } catch (err) {
        errorEl.textContent = 'Сетевая ошибка';
        statusEl.textContent = '';
      }
    });
  }
}
customElements.define('article-editor', ArticleEditor);
