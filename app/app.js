/* Afghanistan News — mobile client mirror (offline-first, RTL).
 * Same public contract as the Android client: /v1/*, no auth, cursor pagination,
 * always-visible source attribution, cached-first rendering. */
(() => {
  'use strict';

  const API = '/v1';
  const CACHE_KEY = 'afnews.cache.v1';
  const BOOKMARK_KEY = 'afnews.bookmarks.v1';
  const PREFS_KEY = 'afnews.prefs.v1';

  const state = {
    tab: 'home',
    screen: 'list',            // list | detail | search | provinces | sources | settings
    articles: [],              // rendered list
    cursor: null,
    nextCursor: null,
    loading: false,
    offline: false,
    fromCache: false,
    prefs: load(PREFS_KEY, { theme: 'system', language: 'fa', province: '' }),
    home: load(CACHE_KEY, null),
    bookmarks: load(BOOKMARK_KEY, []),
    current: null,
    chipKey: null,
  };

  const $ = (s) => document.querySelector(s);
  const content = $('#content');
  const esc = (s) => String(s ?? '').replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
  const fa = (n) => new Intl.NumberFormat('fa-AF', { useGrouping: false }).format(Number(n || 0));
  const nums = (s) => String(s ?? '').replace(/[0-9]/g, (d) => '۰۱۲۳۴۵۶۷۸۹'[d]);

  function load(key, fallback) { try { const v = localStorage.getItem(key); return v ? JSON.parse(v) : fallback; } catch { return fallback; } }
  function save(key, value) { try { localStorage.setItem(key, JSON.stringify(value)); } catch {} }

  function ago(iso) {
    if (!iso) return 'زمان نامعلوم';
    const s = Math.max(0, (Date.now() - new Date(iso).getTime()) / 1000);
    if (s < 60) return 'همین لحظه';
    if (s < 3600) return `${fa(Math.round(s / 60))} دقیقه پیش`;
    if (s < 86400) return `${fa(Math.round(s / 3600))} ساعت پیش`;
    return `${fa(Math.round(s / 86400))} روز پیش`;
  }

  function snack(msg, ms = 2600) {
    const el = $('#snackbar');
    el.textContent = msg;
    el.hidden = false;
    clearTimeout(snack._t);
    snack._t = setTimeout(() => { el.hidden = true; }, ms);
  }

  function setNet(status) {
    const el = $('#net-badge');
    el.className = `net ${status}`;
    el.textContent = status === 'ok' ? 'آنلاین' : (status === 'cache' ? 'ذخیره‌شده' : 'آفلاین');
  }

  // ------------------------------------------------------------------ data
  async function api(path, { cache = true } = {}) {
    try {
      const res = await fetch(`${API}${path}`, { headers: { 'Accept': 'application/json' } });
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      const data = await res.json();
      state.offline = false;
      state.fromCache = false;
      setNet('ok');
      return data;
    } catch (err) {
      state.offline = true;
      setNet('offline');
      if (cache) throw err;
      throw err;
    }
  }

  // ------------------------------------------------------------------ rendering
  function articleCard(a, { compact = false } = {}) {
    const img = a.imageUrl ? `<img class="thumb" src="${esc(a.imageUrl)}" alt="" loading="lazy" onerror="this.style.display='none'" />` : '';
    const badge = a.isBreaking ? '<span class="badge breaking">فوری</span>' : '';
    const source = `<span class="badge brand">${esc(a.source?.name || 'منبع')}</span>`;
    const cat = a.category?.displayNames?.fa ? `<span class="badge">${esc(a.category.displayNames.fa)}</span>` : '';
    const prov = a.province?.displayNames?.fa ? `<span class="badge">${esc(a.province.displayNames.fa)}</span>` : '';
    if (compact) {
      return `<article class="card compact" data-article="${esc(a.id)}">
        <div style="display:grid;gap:6px">
          ${a.imageUrl ? `<img class="thumb-sm" src="${esc(a.imageUrl)}" alt="" loading="lazy" onerror="this.style.display='none'" />` : ''}
        </div>
        <div>
          <h3>${esc(a.title)}</h3>
          <div class="meta">${source}${cat}${badge}<span>${ago(a.publishedAt || a.discoveredAt)}</span></div>
        </div>
      </article>`;
    }
    return `<article class="card" data-article="${esc(a.id)}">
      ${img}
      <div class="body">
        <h3>${esc(a.title)}</h3>
        ${a.summary ? `<p class="summary">${esc(a.summary)}</p>` : ''}
        <div class="meta">${source}${cat}${prov}${badge}<span>${ago(a.publishedAt || a.discoveredAt)}</span></div>
      </div>
    </article>`;
  }

  function section(title, items, { compact = false, more = null } = {}) {
    if (!items || !items.length) return '';
    const cards = items.map((a) => articleCard(a, { compact })).join('');
    return `<section class="section">
      <div class="section-head"><h2>${esc(title)}</h2>${more ? `<button data-goto="${more}">همه ←</button>` : ''}</div>
      ${cards}
    </section>`;
  }

  function breakingStrip(items) {
    if (!items || !items.length) return '';
    const a = items[0];
    return `<div class="breaking-strip" data-article="${esc(a.id)}">
      <div class="label">خبر فوری</div>
      <h3>${esc(a.title)}</h3>
      <div class="meta">${esc(a.source?.name || '')} · ${ago(a.publishedAt || a.discoveredAt)} · ${fa(items.length)} منبع پوشش داده‌اند</div>
    </div>`;
  }

  // ------------------------------------------------------------------ screens
  async function renderHome(force = false) {
    content.innerHTML = '<div id="initial-skeleton"><div class="skel card-skel"></div><div class="skel line"></div><div class="skel card-skel"></div></div>';
    let home = state.home;
    try {
      home = await api('/home');
      state.home = home;
      save(CACHE_KEY, home);
    } catch (e) {
      if (!home) {
        content.innerHTML = `<div class="state"><div class="big">⚠</div><p>اتصال برقرار نشد و دادهٔ ذخیره‌شده‌ای موجود نیست.</p><button class="btn" onclick="location.reload()">تلاش دوباره</button></div>`;
        return;
      }
      state.fromCache = true;
      setNet('cache');
      snack('نمایش از حافظهٔ محلی (آفلاین)');
    }
    const h = home;
    content.innerHTML = `
      ${state.fromCache ? '<div class="toast-cache">آفلاین — نمایش آخرین نسخهٔ ذخیره‌شده</div>' : ''}
      ${breakingStrip(h.breaking)}
      ${section('مهم‌ترین خبرهای افغانستان', h.topStories, { more: 'afghanistan' })}
      ${section('تازه‌ترین اخبار افغانستان', h.latestAfghanistan, { compact: true, more: 'afghanistan' })}
      ${h.followedProvincePreview?.length ? section(`ولایت ${esc(state.prefs.province || '')}`, h.followedProvincePreview, { compact: true }) : ''}
      ${section('اقتصاد و بازار', h.economy, { compact: true })}
      ${section('کار و فرصت‌ها', h.jobs, { compact: true })}
      ${section('جهان', h.world, { compact: true, more: 'world' })}
      ${(h.personalizedSections || []).map((s) => section(s.title || 'برای شما', s.items || [], { compact: true })).join('')}
      <p class="panel-meta" style="text-align:center">ساخته‌شده در ${esc(new Date(h.generatedAt || Date.now()).toLocaleString('fa-AF'))}</p>`;
    wireCards();
  }

  async function renderFeed({ title, categoryId, provinceId, q } = {}) {
    state.cursor = null;
    state.nextCursor = null;
    state.articles = [];
    $('#screen-title').textContent = title || 'اخبار';
    content.innerHTML = '<div class="skel card-skel"></div><div class="skel line"></div><div class="skel card-skel"></div>';
    const params = new URLSearchParams();
    if (categoryId) params.set('categoryId', categoryId);
    if (provinceId) params.set('provinceId', provinceId);
    if (q) params.set('q', q);
    params.set('limit', '20');
    const path = q ? `/search?${params}` : `/articles?${params}`;
    try {
      const data = await api(path);
      state.articles = data.items || [];
      state.nextCursor = data.nextCursor || null;
      renderListInto();
    } catch (e) {
      content.innerHTML = `<div class="error-box">بارگیری ناموفق: ${esc(e.message)}</div>
        <div class="state"><div class="big">📡</div><p>اتصال شبکه را بررسی کنید.</p><button class="btn" id="retry">تلاش دوباره</button></div>`;
      $('#retry')?.addEventListener('click', () => renderFeed({ title, categoryId, provinceId, q }));
    }
  }

  function renderListInto() {
    if (!state.articles.length) {
      content.innerHTML = `<div class="empty"><div class="big">🗞</div><p>در این بخش هنوز خبری منتشر نشده است.</p></div>`;
      return;
    }
    content.innerHTML = state.articles.map((a) => articleCard(a)).join('') +
      (state.nextCursor ? `<button class="btn" id="more" style="width:100%">بیشتر</button>` : '<p class="panel-meta" style="text-align:center">پایان فهرست</p>');
    wireCards();
    $('#more')?.addEventListener('click', loadMore);
  }

  async function loadMore() {
    if (!state.nextCursor) return;
    const btn = $('#more');
    if (btn) { btn.textContent = 'در حال بارگیری…'; btn.disabled = true; }
    const params = new URLSearchParams({ cursor: state.nextCursor, limit: '20' });
    try {
      const data = await api(`/articles?${params}`);
      state.articles = state.articles.concat(data.items || []);
      state.nextCursor = data.nextCursor || null;
      renderListInto();
    } catch (e) {
      if (btn) { btn.textContent = 'تلاش دوباره'; btn.disabled = false; }
      snack('بارگیری بیشتر ناموفق بود');
    }
  }

  function renderSearch() {
    state.screen = 'search';
    $('#screen-title').textContent = 'جستجو';
    content.innerHTML = `
      <input id="q" class="btn" style="width:100%;text-align:right;padding:12px" placeholder="جستجو در عنوان و خلاصه…" autocomplete="off" />
      <div class="chips" id="search-chips">
        <button class="chip" data-q="کابل">کابل</button>
        <button class="chip" data-q="اقتصاد">اقتصاد</button>
        <button class="chip" data-q="زنان">زنان</button>
        <button class="chip" data-q="کار">کار</button>
        <button class="chip" data-q="زلزله">زلزله</button>
      </div>
      <div id="results"></div>`;
    const run = async () => {
      const q = $('#q').value.trim();
      if (!q) { $('#results').innerHTML = '<div class="empty">عبارت جستجو را بنویسید یا یکی از برچسب‌ها را بزنید.</div>'; return; }
      $('#results').innerHTML = '<div class="skel card-skel"></div>';
      try {
        const data = await api(`/search?q=${encodeURIComponent(q)}&limit=20`);
        $('#results').innerHTML = (data.items || []).map((a) => articleCard(a)).join('') ||
          '<div class="empty">نتیجه‌ای یافت نشد.</div>';
        wireCards();
      } catch (e) { $('#results').innerHTML = `<div class="error-box">${esc(e.message)}</div>`; }
    };
    $('#q').addEventListener('keydown', (e) => { if (e.key === 'Enter') run(); });
    document.querySelectorAll('#search-chips .chip').forEach((c) => c.addEventListener('click', () => { $('#q').value = c.dataset.q; run(); }));
  }

  async function renderArticle(id) {
    state.screen = 'detail';
    content.innerHTML = '<div class="skel card-skel"></div><div class="skel line"></div><div class="skel line short"></div>';
    try {
      const a = await api(`/articles/${encodeURIComponent(id)}`);
      state.current = a;
      const saved = state.bookmarks.some((b) => b.id === a.id);
      $('#screen-title').textContent = a.source?.name || 'خبر';
      content.innerHTML = `<article class="detail" dir="auto">
        <h1>${esc(a.title)}</h1>
        <div class="attribution">
          <span class="badge brand">${esc(a.source?.name || '')}</span>
          <span class="badge ${a.source?.type === 'OFFICIAL_REALTIME' ? 'official' : ''}">${esc(a.source?.transparencyLabel || a.source?.type || '')}</span>
          ${a.isBreaking ? '<span class="badge breaking">فوری</span>' : ''}
          <span>${ago(a.publishedAt || a.discoveredAt)}</span>
        </div>
        ${a.imageUrl ? `<img class="hero" src="${esc(a.imageUrl)}" alt="" />` : ''}
        ${a.summary ? `<p class="lead">${esc(a.summary)}</p>` : ''}
        <div class="detail-actions">
          <button class="btn ${saved ? 'primary' : ''}" id="save-btn">${saved ? '★ ذخیره شد' : '☆ ذخیره'}</button>
          <button class="btn" id="share-btn">اشتراک‌گذاری</button>
          <button class="btn" id="text-btn">متن کامل‌تر</button>
        </div>
        <div class="feedbody" id="feedbody" hidden>${a.feedContent || ''}</div>
        <table class="meta-table">
          <tr><td>منبع</td><td>${esc(a.source?.name || '')} — ${esc(a.source?.type || '')}</td></tr>
          <tr><td>پیوند اصلی</td><td><a href="${esc(a.originalUrl)}" target="_blank" rel="noreferrer">${esc(a.originalUrl)}</a></td></tr>
          <tr><td>دسته</td><td>${esc(a.category?.displayNames?.fa || a.category?.id || '—')}</td></tr>
          <tr><td>ولایت</td><td>${esc(a.province?.displayNames?.fa || '—')}</td></tr>
          <tr><td>زبان</td><td>${esc(a.language || '—')}</td></tr>
          <tr><td>زمان انتشار</td><td>${esc(new Date(a.publishedAt || a.discoveredAt).toLocaleString('fa-AF'))}</td></tr>
          <tr><td>پوشش خبری</td><td>${a.cluster ? `${fa(a.cluster.coverageCount)} منبع` : 'تک‌منبع'}</td></tr>
        </table>
        <p class="panel-meta">خلاصهٔ ارسالی از فید منبع نمایش داده می‌شود؛ متن کامل نزد ناشر اصلی است.</p>
      </article>`;
      $('#save-btn').addEventListener('click', () => toggleBookmark(a));
      $('#share-btn').addEventListener('click', () => shareArticle(a));
      $('#text-btn').addEventListener('click', () => {
        const body = $('#feedbody');
        body.hidden = !body.hidden;
        if (!body.hidden && !body.innerHTML.trim()) body.textContent = 'این منبع متن کامل را در فید منتشر نکرده است.';
      });
    } catch (e) {
      content.innerHTML = `<div class="error-box">خبر یافت نشد: ${esc(e.message)}</div>`;
    }
  }

  function renderSaved() {
    state.screen = 'saved';
    $('#screen-title').textContent = 'ذخیره‌شده';
    const items = state.bookmarks;
    content.innerHTML = items.length
      ? `<p class="panel-meta">${fa(items.length)} خبر ذخیره‌شده — بدون نیاز به اینترنت قابل مطالعه است.</p>` + items.map((a) => articleCard(a, { compact: true })).join('')
      : `<div class="empty"><div class="big">🔖</div><p>هیچ خبری ذخیره نشده است. در صفحهٔ خبر روی «ذخیره» بزنید تا آفلاین بخوانید.</p></div>`;
    wireCards();
  }

  async function renderProvinces() {
    state.screen = 'provinces';
    $('#screen-title').textContent = 'ولایت‌ها';
    content.innerHTML = '<div class="skel card-skel"></div>';
    try {
      const data = await api('/provinces');
      content.innerHTML = data.items.map((p) => `
        <div class="list-row" data-province="${esc(p.id)}">
          <div><strong>${esc(p.displayNames?.fa || p.id)}</strong><div class="count">${esc(p.displayNames?.en || '')}</div></div>
          <div class="count">${fa(p.recentStoryCount ?? p.articleCount ?? 0)} خبر</div>
        </div>`).join('');
      document.querySelectorAll('[data-province]').forEach((row) => row.addEventListener('click', () => {
        const id = row.dataset.province;
        const name = row.querySelector('strong').textContent;
        state.prefs.province = name; save(PREFS_KEY, state.prefs);
        push('feed', { title: name, provinceId: id });
      }));
    } catch (e) { content.innerHTML = `<div class="error-box">${esc(e.message)}</div>`; }
  }

  async function renderSources() {
    state.screen = 'sources';
    $('#screen-title').textContent = 'منابع';
    content.innerHTML = '<div class="skel card-skel"></div>';
    try {
      const data = await api('/sources?limit=50');
      content.innerHTML = `<p class="panel-meta">شفافیت منابع: هر خبر با نام و نوع منبع منتشر می‌شود.</p>` +
        data.items.map((s) => `
        <div class="list-row" data-source="${esc(s.id)}">
          <div><strong>${esc(s.name)}</strong><div class="count">${esc(s.transparencyLabel || s.type)} · اعتبار ${fa(s.trustWeight || 0)}/۵</div></div>
          <div class="count">${esc(s.language || '')}</div>
        </div>`).join('');
      document.querySelectorAll('[data-source]').forEach((row) => row.addEventListener('click', () => {
        const el = row;
        push('feed', { title: el.querySelector('strong').textContent, sourceId: el.dataset.source });
      }));
    } catch (e) { content.innerHTML = `<div class="error-box">${esc(e.message)}</div>`; }
  }

  async function renderMore() {
    state.screen = 'more';
    $('#screen-title').textContent = 'بیشتر';
    let version = { feedPackVersion: '—', enabledFeeds: '—' };
    try { version = await api('/feed-pack/version'); } catch {}
    content.innerHTML = `
      <div class="grid-2">
        <div class="list-row" data-more="provinces"><div><strong>ولایت‌ها</strong><div class="count">۳۴ ولایت</div></div><span>›</span></div>
        <div class="list-row" data-more="sources"><div><strong>منابع</strong><div class="count">شفافیت</div></div><span>›</span></div>
        <div class="list-row" data-more="jobs"><div><strong>کار و فرصت‌ها</strong></div><span>›</span></div>
        <div class="list-row" data-more="economy"><div><strong>اقتصاد</strong></div><span>›</span></div>
        <div class="list-row" data-more="notifications"><div><strong>اعلان‌ها</strong></div><span>›</span></div>
        <div class="list-row" data-more="settings"><div><strong>تنظیمات</strong></div><span>›</span></div>
      </div>
      <div class="list-row" style="margin-top:10px" data-more="about"><div><strong>درباره</strong><div class="count">نسخهٔ بستهٔ فید ${esc(version.feedPackVersion || '—')} · ${fa(version.enabledFeeds || 0)} فید فعال</div></div><span>›</span></div>`;
    document.querySelectorAll('[data-more]').forEach((row) => row.addEventListener('click', () => {
      const kind = row.dataset.more;
      if (kind === 'provinces') return renderProvinces();
      if (kind === 'sources') return renderSources();
      if (kind === 'settings') return renderSettings();
      if (kind === 'notifications') return renderNotifications();
      if (kind === 'about') return renderAbout();
      return push('feed', { title: row.querySelector('strong').textContent, categoryId: kind });
    }));
  }

  function renderSettings() {
    $('#screen-title').textContent = 'تنظیمات';
    content.innerHTML = `
      <div class="section-head"><h2>ظاهر</h2></div>
      <div class="chips">
        ${['system', 'light', 'dark'].map((t) => `<button class="chip ${state.prefs.theme === t ? 'active' : ''}" data-theme="${t}">${t === 'system' ? 'خودکار' : t === 'light' ? 'روشن' : 'تیره'}</button>`).join('')}
      </div>
      <div class="section-head"><h2>زبان</h2></div>
      <div class="chips">
        ${['fa', 'ps', 'en'].map((l) => `<button class="chip ${state.prefs.language === l ? 'active' : ''}" data-lang="${l}">${l === 'fa' ? 'دری' : l === 'ps' ? 'پښتو' : 'English'}</button>`).join('')}
      </div>
      <div class="section-head"><h2>داده</h2></div>
      <div class="list-row" data-action="clear"><div><strong>پاک کردن حافظهٔ محلی</strong><div class="count">مقاله‌های ذخیره‌شده و کش</div></div><span>⌫</span></div>
      <p class="panel-meta">در اپ اندروید این تنظیمات در DataStore ذخیره می‌شود؛ در این پیش‌نمای وب از حافظهٔ محلی مرورگر استفاده می‌شود.</p>`;
    document.querySelectorAll('[data-theme]').forEach((b) => b.addEventListener('click', () => {
      state.prefs.theme = b.dataset.theme;
      document.body.dataset.theme = b.dataset.theme === 'system' ? '' : b.dataset.theme;
      save(PREFS_KEY, state.prefs); renderSettings();
    }));
    document.querySelectorAll('[data-lang]').forEach((b) => b.addEventListener('click', () => {
      state.prefs.language = b.dataset.lang; save(PREFS_KEY, state.prefs); renderSettings();
      snack('زبان رابط تغییر کرد');
    }));
    document.querySelector('[data-action="clear"]').addEventListener('click', () => {
      localStorage.removeItem(CACHE_KEY); localStorage.removeItem(BOOKMARK_KEY);
      state.home = null; state.bookmarks = [];
      snack('حافظهٔ محلی پاک شد');
    });
  }

  async function renderNotifications() {
    $('#screen-title').textContent = 'اعلان‌ها';
    content.innerHTML = '<div class="skel card-skel"></div>';
    try {
      const data = await api('/notifications');
      const items = data.items || [];
      content.innerHTML = items.length ? items.map((n) => `
        <div class="list-row" ${n.articleId ? `data-article="${esc(n.articleId)}"` : ''}>
          <div><strong>${esc(n.title || 'خبر فوری')}</strong><div class="count">${esc(n.topic || '')} · ${ago(n.createdAt)}</div></div>
        </div>`).join('') : `<div class="empty"><div class="big">🔔</div><p>در ۲۴ ساعت گذشته اعلان فوری منتشر نشده است.</p>
        <button class="btn" id="enable-push">فعال کردن اعلان‌ها</button></div>`;
      wireCards();
      $('#enable-push')?.addEventListener('click', subscribePush);
    } catch (e) { content.innerHTML = `<div class="error-box">${esc(e.message)}</div>`; }
  }

  async function renderAbout() {
    $('#screen-title').textContent = 'درباره';
    let cfg = {};
    try { cfg = await api('/config'); } catch {}
    content.innerHTML = `
      <div class="section-head"><h2>پلتفرم خبر افغانستان</h2></div>
      <p class="lead">این سرویس اخبار را از فیدهای رسمی و ناشران معتبر گردآوری می‌کند، آن‌ها را دسته‌بندی و بر اساس ولایت برچسب می‌زند و با ذکر همیشگی منبع منتشر می‌کند.</p>
      <table class="meta-table">
        <tr><td>مسیر API</td><td><code>/v1</code></td></tr>
        <tr><td>صفحه‌بندی</td><td>مکان‌نما (cursor)</td></tr>
        <tr><td>حساب کاربری</td><td>لازم نیست</td></tr>
        <tr><td>نرخ بازار</td><td>فقط از منابع معتبر (بدون حدس از تیتر)</td></tr>
        <tr><td>وابستگی‌ها</td><td>Jetpack Compose · Room · Hilt · Paging 3 · WorkManager</td></tr>
        <tr><td>سازگاری</td><td>Android 5.0 (API 21) تا Android 16 · arm32/arm64</td></tr>
        <tr><td>پیکربندی سرور</td><td>${esc(JSON.stringify(cfg.defaults || cfg).slice(0, 160))}</td></tr>
      </table>`;
  }

  // ------------------------------------------------------------------ actions
  function wireCards() {
    content.querySelectorAll('[data-article]').forEach((el) => {
      el.addEventListener('click', (e) => {
        if (e.target.closest('a')) return;
        push('article', { id: el.dataset.article });
      });
    });
    content.querySelectorAll('[data-goto]').forEach((el) => {
      el.addEventListener('click', (e) => {
        e.stopPropagation();
        const target = el.dataset.goto;
        state.tab = target === 'world' ? 'world' : 'afghanistan';
        syncTabs();
        renderFeed(target === 'world' ? { title: 'جهان', categoryId: 'world' } : { title: 'افغانستان', categoryId: 'afghanistan' });
      });
    });
  }

  function toggleBookmark(a) {
    const idx = state.bookmarks.findIndex((b) => b.id === a.id);
    if (idx >= 0) { state.bookmarks.splice(idx, 1); snack('از ذخیره‌ها حذف شد'); }
    else { state.bookmarks.unshift(a); snack('ذخیره شد — آفلاین قابل مطالعه است'); }
    save(BOOKMARK_KEY, state.bookmarks);
    renderArticle(a.id);
  }

  async function shareArticle(a) {
    const text = `${a.title}\n${a.source?.name || ''}\n${a.originalUrl}`;
    try {
      if (navigator.share) { await navigator.share({ title: a.title, text, url: a.originalUrl }); return; }
      await navigator.clipboard.writeText(text);
      snack('متن خبر و پیوند کپی شد');
    } catch { snack('اشتراک‌گذاری در این مرورگر پشتیبانی نمی‌شود'); }
  }

  async function subscribePush() {
    const token = 'web-' + Math.random().toString(36).slice(2, 12);
    try {
      await fetch(`${API}/push/register`, {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ token, platform: 'WEB_PREVIEW', language: state.prefs.language, topics: ['breaking', 'afghanistan'] }),
      });
      snack('ثبت شد — اعلان‌های فوری + افغانستان');
    } catch { snack('ثبت اعلان ناموفق بود'); }
  }

  // ------------------------------------------------------------------ navigation
  const history_ = [];
  async function push(screen, params = {}) {
    history_.push({ screen: state.screen, params: state.screenParams || {} });
    state.screen = screen;
    state.screenParams = params;
    $('#back-btn').hidden = history_.length === 0;
    if (screen === 'article') await renderArticle(params.id);
    if (screen === 'feed') await renderFeed(params);
    if (screen === 'search') renderSearch();
    if (screen === 'saved') renderSaved();
    if (screen === 'provinces') await renderProvinces();
    if (screen === 'sources') await renderSources();
    if (screen === 'more') await renderMore();
  }

  async function goBack() {
    history_.pop();
    $('#back-btn').hidden = history_.length === 0;
    await activateTab(state.tab, true);
  }

  function syncTabs() {
    document.querySelectorAll('.nav-btn').forEach((b) => b.classList.toggle('active', b.dataset.tab === state.tab));
  }

  async function activateTab(tab, skipHistory = false) {
    state.tab = tab;
    if (!skipHistory) { history_.length = 0; $('#back-btn').hidden = true; }
    syncTabs();
    switch (tab) {
      case 'home': $('#screen-title').textContent = 'خانه'; return renderHome();
      case 'afghanistan': return renderFeed({ title: 'افغانستان', categoryId: 'afghanistan' });
      case 'world': return renderFeed({ title: 'جهان', categoryId: 'world' });
      case 'saved': return renderSaved();
      case 'more': return renderMore();
    }
  }

  // ------------------------------------------------------------------ boot
  function wireChrome() {
    document.querySelectorAll('.nav-btn').forEach((b) => b.addEventListener('click', () => activateTab(b.dataset.tab)));
    $('#search-btn').addEventListener('click', () => push('search'));
    $('#back-btn').addEventListener('click', goBack);
    $('#refresh-btn').addEventListener('click', async () => {
      snack('تازه‌سازی…');
      if (state.screen === 'detail' && state.current) return renderArticle(state.current.id);
      const home = await (async () => { try { const h = await api('/home'); state.home = h; save(CACHE_KEY, h); return h; } catch { return state.home; } })();
      await activateTab(state.tab, true);
      if (home) snack('به‌روز شد');
    });
    window.addEventListener('online', () => { setNet('ok'); snack('اتصال برگشت — به‌روزرسانی…'); activateTab(state.tab, true); });
    window.addEventListener('offline', () => { setNet('offline'); snack('آفلاین — نمایش دادهٔ ذخیره‌شده'); });
    setInterval(() => {
      $('#clock').textContent = nums(new Date().toLocaleTimeString('en-GB', { hour: '2-digit', minute: '2-digit' }));
      $('#battery').textContent = nums('87%');
    }, 30000);
  }

  async function boot() {
    wireChrome();
    if (state.prefs.theme && state.prefs.theme !== 'system') document.body.dataset.theme = state.prefs.theme;
    try {
      const v = await api('/feed-pack/version');
      $('#panel-meta').innerHTML = `نسخهٔ بستهٔ فید: <strong>${esc(v.feedPackVersion || '—')}</strong><br/>
        فیدهای فعال: <strong>${fa(v.enabledFeeds || 0)}</strong><br/>
        منابع: <strong>${fa(v.sourcesTotal || 0)}</strong> · مقالات: <strong>${fa(v.articlesTotal || 0)}</strong><br/>
        حالت سرور: ${esc(v.env || 'local')} · ${esc(v.driver || '')}`;
    } catch {}
    await activateTab('home');
  }

  boot();
})();
