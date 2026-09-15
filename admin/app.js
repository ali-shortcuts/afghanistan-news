/* Afghanistan News — editorial console logic.
 * Talks only to the same-origin /admin/api/* surface. No external dependencies. */
(() => {
  'use strict';

  const state = {
    token: localStorage.getItem('afnews.token') || '',
    user: null,
    view: 'dashboard',
    articles: { offset: 0, limit: 25, total: 0, filters: {} },
    feeds: { offset: 0, limit: 25, total: 0, filters: {} },
    audit: { offset: 0, limit: 30, total: 0 },
    reference: null,
  };

  const $ = (sel, root = document) => root.querySelector(sel);
  const $$ = (sel, root = document) => Array.from(root.querySelectorAll(sel));
  const esc = (s) => String(s ?? '').replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
  const fmtDate = (v) => (v ? new Date(v).toLocaleString('en-GB', { dateStyle: 'short', timeStyle: 'short' }) : '—');
  const fmtAgo = (v) => {
    if (!v) return 'never';
    const s = Math.max(0, (Date.now() - new Date(v).getTime()) / 1000);
    if (s < 60) return `${Math.round(s)}s ago`;
    if (s < 3600) return `${Math.round(s / 60)}m ago`;
    if (s < 86400) return `${Math.round(s / 3600)}h ago`;
    return `${Math.round(s / 86400)}d ago`;
  };
  const num = (n) => new Intl.NumberFormat('en-US').format(Number(n || 0));

  // ---------------------------------------------------------------- transport
  async function api(path, { method = 'GET', body, raw = false } = {}) {
    const res = await fetch(`/admin/api${path}`, {
      method,
      headers: {
        'Accept': 'application/json',
        ...(body ? { 'Content-Type': 'application/json' } : {}),
        ...(state.token ? { 'Authorization': `Bearer ${state.token}` } : {}),
      },
      body: body ? JSON.stringify(body) : undefined,
      credentials: 'same-origin',
    });
    const text = await res.text();
    let data = null;
    try { data = text ? JSON.parse(text) : null; } catch { data = { raw: text }; }
    if (!res.ok) {
      if (res.status === 401) { signOut(false); }
      const msg = data?.error?.message || `HTTP ${res.status}`;
      const err = new Error(msg);
      err.status = res.status;
      err.data = data;
      throw err;
    }
    return raw ? data : (data ?? {});
  }

  function toast(message, kind = '') {
    const el = $('#toast');
    el.className = `toast ${kind}`;
    el.textContent = message;
    el.hidden = false;
    clearTimeout(toast._t);
    toast._t = setTimeout(() => { el.hidden = true; }, 4200);
  }

  // ---------------------------------------------------------------- auth
  async function signIn(username, password) {
    const data = await api('/login', { method: 'POST', body: { username, password } });
    state.token = data.token;
    localStorage.setItem('afnews.token', data.token);
    if (data.sessionCookie) document.cookie = `afnews_session=${data.token}; path=/; max-age=28800; samesite=lax`;
    state.user = { username: data.username, role: data.role, expiresAt: data.expiresAt };
    return data;
  }

  function signOut(callServer = true) {
    if (callServer && state.token) { api('/logout', { method: 'POST' }).catch(() => {}); }
    state.token = '';
    state.user = null;
    localStorage.removeItem('afnews.token');
    $('#view-console').hidden = true;
    $('#view-login').hidden = false;
    $('#login-password').value = '';
  }

  // ---------------------------------------------------------------- router
  const VIEW_TITLES = {
    dashboard: 'Dashboard', articles: 'Article moderation', feeds: 'Feed registry',
    imports: 'Feed-pack imports', push: 'Push &amp; breaking', activation: 'Rollout waves',
    audit: 'Audit log', reference: 'Reference data',
  };

  function showView(name) {
    state.view = name;
    $$('.view').forEach((v) => { v.hidden = v.id !== `view-${name}`; });
    $$('.nav-item').forEach((b) => b.classList.toggle('active', b.dataset.view === name));
    $('#view-title').textContent = (VIEW_TITLES[name] || name).replace('&amp;', '&');
    closeDrawer();
    loadView(name);
  }

  function loadView(name) {
    switch (name) {
      case 'dashboard': return loadDashboard();
      case 'articles': return loadArticles();
      case 'feeds': return loadFeeds();
      case 'imports': return loadImports();
      case 'push': return loadPush();
      case 'activation': return loadWaves();
      case 'audit': return loadAudit();
      case 'reference': return loadReference();
    }
  }

  function refreshCurrent() { loadView(state.view); toast('Refreshed'); }

  // ---------------------------------------------------------------- dashboard
  async function loadDashboard() {
    const data = await api('/dashboard');
    const s = data.stats || {};
    const m = data.metrics || {};
    $('#env-pill').textContent = `API ok · ${state.user?.role || ''}`;
    $('#stat-grid').innerHTML = [
      stat('Articles total', num(s.articlesTotal), `${num(s.articlesLast24h)} in 24h`),
      stat('Feeds enabled', num(s.feedsEnabled), `${num(s.feedsTotal)} in registry`),
      stat('Sources', num(s.sourcesTotal), 'publisher identities'),
      stat('Breaking active', num(s.breakingActive), 'flagged breaking'),
      stat('Fetch failures 24h', num(s.fetchFailuresLast24h), `${num(m.feed_fetch_total || 0)} fetches lifetime`),
      stat('Duplicates prevented', num(s.duplicatesPreventedLast24h), `${num(m.articles_duplicate_total || 0)} lifetime`),
      stat('Pushes 24h', num(s.pushSentLast24h), 'breaking notifications'),
      stat('Avg fetch', `${num(s.avgFetchDurationMs)} ms`, 'per feed request'),
    ].join('');

    const health = s.feedsByHealth || {};
    const total = Object.values(health).reduce((a, b) => a + Number(b || 0), 0) || 1;
    const order = ['HEALTHY', 'DEGRADED', 'UNSTABLE', 'STALE', 'EMPTY', 'PARSER_ERROR', 'HTTP_ERROR', 'RATE_LIMITED', 'QUARANTINED', 'DISABLED', 'UNKNOWN'];
    $('#health-bars').innerHTML = order.filter((k) => health[k]).map((k) => {
      const v = Number(health[k] || 0);
      const cls = ['HEALTHY'].includes(k) ? '' : (['DEGRADED', 'UNSTABLE', 'STALE', 'EMPTY', 'UNKNOWN'].includes(k) ? 'warn' : 'bad');
      return `<div class="health-row"><span>${esc(k)}</span><div class="bar ${cls}"><i style="width:${Math.max(2, (v / total) * 100)}%"></i></div><span class="num">${num(v)}</span></div>`;
    }).join('') || '<p class="muted tiny">No health data yet — run the worker or test a feed.</p>';

    $('#failing-table tbody').innerHTML = (data.topFailingDomains || []).map((d) =>
      `<tr><td class="mono">${esc(d.domain)}</td><td class="num">${num(d.failures)}</td></tr>`).join('')
      || '<tr><td colspan="2" class="muted">No failures recorded.</td></tr>';

    $('#metrics-table tbody').innerHTML = Object.entries(m).map(([k, v]) =>
      `<tr><td class="mono">${esc(k)}</td><td class="num">${num(v)}</td></tr>`).join('')
      || '<tr><td colspan="2" class="muted">No counters yet.</td></tr>';

    $('#recent-imports').innerHTML = (data.recentImports || []).map((i) =>
      `<div class="list-item"><strong>${esc(i.version || '(unknown)')}</strong> · ${i.committed ? 'commit' : 'dry-run'}
       <div class="meta">${esc(i.importedBy || '—')} · ${fmtAgo(i.importedAt)} · new ${num(i.new)} / changed ${num(i.changed)} / missing ${num(i.missingFromPack ?? i.missing)} / invalid ${num(i.invalid)}</div></div>`).join('')
      || '<p class="muted tiny">No imports yet.</p>';

    $('#recent-pushes').innerHTML = (data.recentPushes || []).map((p) =>
      `<div class="list-item"><strong>${esc(p.title || '(no title)')}</strong>
       <div class="meta">${esc(p.topic || '')} · ${esc(p.status || '')} · ${fmtAgo(p.createdAt)}</div></div>`).join('')
      || '<p class="muted tiny">No pushes yet.</p>';
  }

  const stat = (label, value, sub) =>
    `<div class="stat"><div class="label">${esc(label)}</div><div class="value">${value}</div><div class="sub">${esc(sub || '')}</div></div>`;

  // ---------------------------------------------------------------- articles
  async function loadReferenceInto(selectEl, items, idKey = 'id', labelKey = 'en') {
    if (!selectEl) return;
    const current = selectEl.value;
    const opts = items.map((c) => `<option value="${esc(c[idKey])}">${esc((c.displayNames && (c.displayNames.en || c.displayNames.fa)) || c[labelKey] || c[idKey])}</option>`).join('');
    selectEl.innerHTML = selectEl.dataset.placeholder + opts;
    selectEl.value = current;
  }

  async function loadArticles() {
    const params = new URLSearchParams();
    params.set('limit', state.articles.limit);
    params.set('offset', state.articles.offset);
    const f = state.articles.filters;
    if (f.q) params.set('q', f.q);
    if (f.status) params.set('status', f.status);
    if (f.categoryId) params.set('categoryId', f.categoryId);
    if (f.sourceId) params.set('sourceId', f.sourceId);
    if (f.breaking) params.set('breaking', 'true');
    if (!state.reference) { await loadReference(); }
    if (!$('#art-category').dataset.placeholder) {
      $('#art-category').dataset.placeholder = '<option value="">category: any</option>';
      $('#art-source').dataset.placeholder = '<option value="">source: any</option>';
    }
    await loadReferenceInto($('#art-category'), state.reference?.categories || [], 'id', 'id');
    const sources = await api('/feeds?limit=200');
    const doms = [...new Map((sources.items || []).map((f) => [f.sourceId, { id: f.sourceId, label: f.sourceName || f.sourceId }])).values()];
    await loadReferenceInto($('#art-source'), doms.map((d) => ({ id: d.id, displayNames: { en: d.label } })), 'id', 'id');

    const data = await api(`/articles?${params}`);
    state.articles.total = data.total || 0;
    const rows = (data.items || []).map((a) => `
      <tr>
        <td class="title-cell">
          <strong>${esc(a.title || '(untitled)')}</strong>
          <div class="url">${esc(a.originalUrl || '')} · ${esc(a.id)}</div>
        </td>
        <td>${esc(a.source?.name || '—')}<div class="muted tiny">${esc(a.source?.transparencyLabel || a.source?.type || '')}</div></td>
        <td>${esc(a.category?.displayNames?.en || a.category?.id || '—')}</td>
        <td>${fmtDate(a.publishedAt || a.discoveredAt)}<div class="muted tiny">${fmtAgo(a.discoveredAt)}</div></td>
        <td class="center">${a.isBreaking ? '<span class="badge bad">BREAKING</span>' : '<span class="badge neutral">—</span>'}</td>
        <td class="center"><span class="badge neutral">${esc(a.status || 'PUBLISHED')}</span></td>
        <td><div class="row">
          <button class="btn tiny-btn" data-art-open="${esc(a.id)}">Open</button>
          <button class="btn tiny-btn" data-art-break="${esc(a.id)}" data-value="${a.isBreaking ? 'false' : 'true'}">${a.isBreaking ? 'Unflag' : 'Breaking'}</button>
          <button class="btn tiny-btn" data-art-hide="${esc(a.id)}" data-value="${a.status === 'HIDDEN' ? 'PUBLISHED' : 'HIDDEN'}">${a.status === 'HIDDEN' ? 'Publish' : 'Hide'}</button>
        </div></td>
      </tr>`).join('');
    $('#articles-table tbody').innerHTML = rows || '<tr><td colspan="7" class="muted">No articles match these filters.</td></tr>';
    $('#art-page-info').textContent = `${num(state.articles.offset + 1)}–${num(Math.min(state.articles.offset + state.articles.limit, state.articles.total))} of ${num(state.articles.total)}`;
    $('#art-prev').disabled = state.articles.offset === 0;
    $('#art-next').disabled = state.articles.offset + state.articles.limit >= state.articles.total;
  }

  async function openArticle(id) {
    const a = await api(`/articles/${encodeURIComponent(id)}`);
    openDrawer('Article', `
      <dl class="kv">
        <dt>Title</dt><dd>${esc(a.title)}</dd>
        <dt>Source</dt><dd>${esc(a.source?.name)} · ${esc(a.source?.transparencyLabel || '')} <a class="mono" href="${esc(a.originalUrl)}" target="_blank" rel="noreferrer">original ↗</a></dd>
        <dt>Category</dt><dd>${esc(a.category?.displayNames?.en || a.category?.id || '—')}</dd>
        <dt>Province</dt><dd>${esc(a.province?.displayNames?.en || a.province?.id || '—')}</dd>
        <dt>Published</dt><dd>${fmtDate(a.publishedAt)} <span class="muted tiny">(discovered ${fmtAgo(a.discoveredAt)})</span></dd>
        <dt>Language</dt><dd>${esc(a.language || '—')}</dd>
        <dt>Status</dt><dd>${esc(a.status || 'PUBLISHED')} ${a.isBreaking ? '<span class="badge bad">BREAKING</span>' : ''}</dd>
        <dt>Cluster</dt><dd>${a.cluster ? `${esc(a.cluster.id)} · coverage ${num(a.cluster.coverageCount)}` : '—'}</dd>
        <dt>Content hash</dt><dd class="mono tiny">${esc(a.contentHash || '—')}</dd>
      </dl>
      <div><h4>Summary</h4><p>${esc(a.summary || '(none)')}</p></div>
      <details><summary class="muted tiny">Feed-provided content (sanitised)</summary><div class="mono tiny">${esc((a.feedContent || '').slice(0, 4000))}</div></details>
      <div class="row">
        <select class="input" id="drawer-cat">${(state.reference?.categories || []).map((c) => `<option value="${esc(c.id)}" ${c.id === a.category?.id ? 'selected' : ''}>${esc(c.displayNames?.en || c.id)}</option>`).join('')}</select>
        <select class="input" id="drawer-prov">${(state.reference?.provinces || []).map((p) => `<option value="${esc(p.id)}" ${p.id === a.province?.id ? 'selected' : ''}>${esc(p.displayNames?.en || p.id)}</option>`).join('')}</select>
        <button class="btn primary" id="drawer-save">Save classification</button>
      </div>`);
    $('#drawer-save').onclick = async () => {
      try {
        await api(`/articles/${encodeURIComponent(id)}`, {
          method: 'PATCH',
          body: { categoryId: $('#drawer-cat').value, provinceId: $('#drawer-prov').value },
        });
        toast('Classification saved', 'ok');
        closeDrawer();
        loadArticles();
      } catch (e) { toast(e.message, 'bad'); }
    };
  }

  // ---------------------------------------------------------------- feeds
  async function loadFeeds() {
    const params = new URLSearchParams();
    params.set('limit', state.feeds.limit);
    params.set('offset', state.feeds.offset);
    const f = state.feeds.filters;
    Object.entries(f).forEach(([k, v]) => { if (v !== '' && v !== undefined && v !== null) params.set(k, v); });
    const data = await api(`/feeds?${params}`);
    state.feeds.total = data.total || 0;
    const healthClass = (h) => ({ HEALTHY: 'ok', DEGRADED: 'warn', UNSTABLE: 'warn', STALE: 'warn', EMPTY: 'warn', UNKNOWN: 'neutral', DISABLED: 'neutral' }[h] || 'bad');
    $('#feeds-table tbody').innerHTML = (data.items || []).map((fd) => `
      <tr>
        <td class="title-cell">
          <strong>${esc(fd.title || fd.xmlUrl)}</strong>
          <div class="url">${esc(fd.xmlUrl)}</div>
          <div class="muted tiny">${esc(fd.sourceName || fd.sourceId)} · ${esc(fd.categoryKey || '—')} · ${esc(fd.scope || '—')}${fd.needsReview ? ' · <span class="badge warn">needs review</span>' : ''}</div>
        </td>
        <td><span class="badge brand">${esc(fd.sourceType)}</span><div class="muted tiny">${esc(fd.language || '')}</div></td>
        <td class="center">${num(fd.priority)}</td>
        <td class="center">${esc(fd.pollTier || '—')}</td>
        <td class="center"><span class="badge ${healthClass(fd.healthStatus)}">${esc(fd.healthStatus)}</span>
          <div class="muted tiny">${num(fd.consecutiveFailures)} fail · ${fmtAgo(fd.lastCheckedAt)}</div></td>
        <td class="center">${num(fd.healthScore ?? '—')}</td>
        <td class="center">
          <input type="checkbox" data-feed-toggle="${esc(fd.id)}" ${fd.enabled ? 'checked' : ''} />
        </td>
        <td><div class="row">
          <button class="btn tiny-btn" data-feed-open="${esc(fd.id)}">Detail</button>
          <button class="btn tiny-btn" data-feed-test="${esc(fd.id)}">Test</button>
        </div></td>
      </tr>`).join('') || '<tr><td colspan="8" class="muted">No feeds match these filters.</td></tr>';
    $('#feed-page-info').textContent = `${num(state.feeds.offset + 1)}–${num(Math.min(state.feeds.offset + state.feeds.limit, state.feeds.total))} of ${num(state.feeds.total)}`;
    $('#feed-prev').disabled = state.feeds.offset === 0;
    $('#feed-next').disabled = state.feeds.offset + state.feeds.limit >= state.feeds.total;
  }

  async function openFeed(id) {
    const [feed, history] = await Promise.all([
      api(`/feeds/${encodeURIComponent(id)}`),
      api(`/feeds/${encodeURIComponent(id)}/history?limit=20`).catch(() => ({ items: [] })),
    ]);
    const events = (history.items || []).map((e) => `
      <div class="list-item"><strong>${esc(e.eventType)}</strong> <span class="muted tiny">${fmtDate(e.checkedAt)}</span>
        <div class="meta">${esc(e.errorCode || '')} ${esc(e.message || '')} · items ${num(e.itemCount || 0)} · ${num(e.durationMs || 0)} ms</div></div>`).join('')
      || '<p class="muted tiny">No health events recorded.</p>';
    openDrawer(feed.title || feed.xmlUrl, `
      <dl class="kv">
        <dt>Feed ID</dt><dd class="mono">${esc(feed.id)}</dd>
        <dt>Source</dt><dd>${esc(feed.sourceName || '')} <span class="mono tiny">${esc(feed.sourceId || '')}</span></dd>
        <dt>XML URL</dt><dd class="mono tiny">${esc(feed.xmlUrl)}</dd>
        <dt>Site</dt><dd>${feed.htmlUrl ? `<a href="${esc(feed.htmlUrl)}" target="_blank" rel="noreferrer">${esc(feed.htmlUrl)} ↗</a>` : '—'}</dd>
        <dt>Type / priority</dt><dd>${esc(feed.sourceType)} · P${num(feed.priority)}</dd>
        <dt>Tier</dt><dd>${esc(feed.pollTier)}</dd>
        <dt>Health</dt><dd>${esc(feed.healthStatus)} (score ${num(feed.healthScore ?? 0)}, ${num(feed.consecutiveFailures)} consecutive failures)</dd>
        <dt>Last checked</dt><dd>${fmtDate(feed.lastCheckedAt)} <span class="muted tiny">${fmtAgo(feed.lastCheckedAt)}</span></dd>
        <dt>Last success</dt><dd>${fmtDate(feed.lastSuccessAt)}</dd>
        <dt>Next poll</dt><dd>${fmtDate(feed.nextPollAt)}</dd>
        <dt>Feed pack</dt><dd>${esc(feed.feedPackVersion || '—')}${feed.needsReview ? ' · <span class="badge warn">needs review</span>' : ''}</dd>
        <dt>ETag / Last-Modified</dt><dd class="mono tiny">${esc(feed.etag || '—')} / ${esc(feed.lastModified || '—')}</dd>
      </dl>
      <div class="row">
        <label class="field"><span>Priority</span><input class="input" id="feed-pri" type="number" min="1" max="5" value="${num(feed.priority)}" /></label>
        <label class="field"><span>Tier</span>
          <select class="input" id="feed-tier">${['BREAKING', 'HIGH', 'NORMAL', 'SLOW', 'OPPORTUNITY'].map((t) => `<option ${t === feed.pollTier ? 'selected' : ''}>${t}</option>`).join('')}</select>
        </label>
        <label class="field"><span>Category key</span><input class="input" id="feed-cat" value="${esc(feed.categoryKey || '')}" /></label>
      </div>
      <button class="btn primary" id="feed-save">Save feed settings</button>
      <div><h4>Recent health events</h4>${events}</div>`);
    $('#feed-save').onclick = async () => {
      try {
        await api(`/feeds/${encodeURIComponent(id)}`, {
          method: 'PATCH',
          body: { priority: Number($('#feed-pri').value), pollTier: $('#feed-tier').value, categoryKey: $('#feed-cat').value },
        });
        toast('Feed updated', 'ok');
        closeDrawer();
        loadFeeds();
      } catch (e) { toast(e.message, 'bad'); }
    };
  }

  async function testFeed(id) {
    toast('Testing feed…');
    try {
      const res = await api(`/feeds/${encodeURIComponent(id)}/test`, { method: 'POST', body: {} });
      const diag = res.diagnostics || res;
      const kind = diag.ok ? 'ok' : 'bad';
      toast(`Test ${diag.ok ? 'passed' : 'failed'}: ${diag.summary || diag.error || 'see detail'}`, kind);
      openDrawer('Feed test result', `<pre class="mono tiny">${esc(JSON.stringify(diag, null, 2))}</pre>`);
    } catch (e) { toast(`Test failed: ${e.message}`, 'bad'); }
  }

  // ---------------------------------------------------------------- imports
  async function loadImports() {
    const data = await api('/feedpacks?limit=25');
    $('#imports-table tbody').innerHTML = (data.items || []).map((i) => `
      <tr>
        <td class="mono">${esc(i.version || '—')}</td>
        <td>${i.committed ? '<span class="badge ok">commit</span>' : '<span class="badge neutral">dry-run</span>'}</td>
        <td class="num">${num(i.total)}</td>
        <td class="num">${num(i.new)}</td>
        <td class="num">${num(i.changed)}</td>
        <td class="num">${num(i.missingFromPack ?? i.missing)}</td>
        <td class="num">${num(i.invalid)}</td>
        <td>${esc(i.importedBy || '—')}</td>
        <td>${fmtDate(i.importedAt)}</td>
      </tr>`).join('') || '<tr><td colspan="9" class="muted">No imports recorded for this registry.</td></tr>';
  }

  async function runImport(commit) {
    const path = $('#import-path').value.trim();
    const qs = `?commit=${commit ? 'true' : 'false'}${path ? `&path=${encodeURIComponent(path)}` : ''}`;
    $('#import-result').innerHTML = '<p class="muted tiny">Reconciling feed pack…</p>';
    try {
      const res = await api(`/feedpacks/import${qs}`, { method: 'POST', body: {} });
      const r = res.result || res;
      const notice = r.docError
        ? `<div class="notice bad"><strong>Document rejected:</strong> ${esc(r.docError)}</div>`
        : (commit
          ? '<div class="notice ok"><strong>Committed.</strong> Runtime fields (etag, last success, failures, health) were preserved; absent feeds were flagged <code>needs_review</code>, never deleted.</div>'
          : '<div class="notice"><strong>Dry run.</strong> Nothing was written. Review the diff, then commit.</div>');
      $('#import-result').innerHTML = notice + `
        <div class="import-summary">
          ${chip('version', esc(r.version || '—'))}
          ${chip('outlines', num(r.total))}
          ${chip('new', num(r.new))}
          ${chip('changed', num(r.changed))}
          ${chip('unchanged', num(r.unchanged))}
          ${chip('missing', num(r.missingFromPack ?? r.missing))}
          ${chip('invalid', num(r.invalid))}
          ${chip('disabled', num(r.disabled))}
        </div>
        ${(r.validationErrors || []).length ? `<div class="notice bad"><strong>Per-outline errors</strong><ul>${r.validationErrors.map((e) => `<li>${esc(e)}</li>`).join('')}</ul></div>` : ''}
        ${(r.sampleChanges || []).length ? `<table class="table compact"><thead><tr><th>Change</th><th>Title</th><th>Detail</th></tr></thead><tbody>${r.sampleChanges.map((c) => `<tr><td><span class="badge brand">${esc(c.kind)}</span></td><td>${esc(c.title || '')}<div class="mono tiny">${esc(c.xmlUrl || '')}</div></td><td class="muted tiny">${esc(c.detail || '')}</td></tr>`).join('')}</tbody></table>` : ''}`;
      toast(commit ? 'Feed pack committed' : 'Dry run complete', 'ok');
      if (commit) loadImports();
    } catch (e) {
      $('#import-result').innerHTML = `<div class="notice bad">${esc(e.message)}</div>`;
      toast(e.message, 'bad');
    }
  }

  const chip = (k, v) => `<div class="chip"><div class="k">${k}</div><div class="v">${v}</div></div>`;

  // ---------------------------------------------------------------- push
  async function loadPush() {
    const data = await api('/push/events?limit=30');
    $('#push-table tbody').innerHTML = (data.items || []).map((p) => `
      <tr><td>${fmtDate(p.createdAt)}<div class="muted tiny">${fmtAgo(p.createdAt)}</div></td>
        <td class="mono">${esc(p.topic || '')}</td>
        <td>${esc(p.title || '')}<div class="muted tiny">${esc(p.body || '').slice(0, 90)}</div></td>
        <td class="center"><span class="badge ${p.status === 'SENT' ? 'ok' : (p.status === 'FAILED' ? 'bad' : 'neutral')}">${esc(p.status)}</span></td>
        <td>${esc(p.actor || '')}</td></tr>`).join('') || '<tr><td colspan="5" class="muted">No push events yet.</td></tr>';
  }

  function pushBody(confirm) {
    return {
      articleId: $('#push-article').value.trim(),
      topic: $('#push-topic').value,
      title: $('#push-title').value.trim(),
      body: $('#push-body').value.trim(),
      confirm,
    };
  }

  // ---------------------------------------------------------------- waves
  async function loadWaves() {
    const data = await api('/activation');
    $('#waves').innerHTML = (data.waves || []).map((w) => `
      <div class="wave">
        <header>
          <div><strong>Wave ${w.wave} — ${esc(w.label)}</strong><div class="count">${esc(w.detail || '')}</div></div>
          <div class="row"><span class="badge brand">${num(w.enabledFeeds)} / ${num(w.totalFeeds)} feeds</span>
          <button class="btn tiny-btn" data-wave="${w.wave}">Activate</button></div>
        </header>
      </div>`).join('') + `<div class="notice">${(data.notes || []).map((n) => esc(n)).join('<br/>')}</div>`;
  }

  async function activateWave(wave) {
    if (!confirm(`Activate wave ${wave}? Feed history, health and articles are preserved.`)) return;
    try {
      const res = await api('/activation', { method: 'POST', body: { wave } });
      toast(`Wave ${res.wave}: ${num(res.enabled)} enabled, ${num(res.disabled)} disabled`, 'ok');
      loadWaves();
    } catch (e) { toast(e.message, 'bad'); }
  }

  // ---------------------------------------------------------------- audit
  async function loadAudit() {
    const params = new URLSearchParams({ limit: state.audit.limit, offset: state.audit.offset });
    const entity = $('#audit-entity').value;
    if (entity) params.set('entity', entity);
    const data = await api(`/audit?${params}`);
    state.audit.total = data.total || 0;
    $('#audit-table tbody').innerHTML = (data.items || []).map((e) => `
      <tr><td>${fmtDate(e.createdAt)}</td><td>${esc(e.actor)}</td><td><span class="badge neutral">${esc(e.action)}</span></td>
        <td>${esc(e.entity)}</td><td class="mono tiny">${esc(e.entityId || '')}</td></tr>`).join('')
      || '<tr><td colspan="5" class="muted">No audit entries.</td></tr>';
    $('#audit-page-info').textContent = `${num(state.audit.offset + 1)}–${num(Math.min(state.audit.offset + state.audit.limit, state.audit.total))} of ${num(state.audit.total)}`;
    $('#audit-prev').disabled = state.audit.offset === 0;
    $('#audit-next').disabled = state.audit.offset + state.audit.limit >= state.audit.total;
  }

  // ---------------------------------------------------------------- reference
  async function loadReference() {
    if (!state.reference) state.reference = await api('/reference');
    const r = state.reference;
    $('#categories-count').textContent = `${(r.categories || []).length} categories`;
    $('#categories-table tbody').innerHTML = (r.categories || []).map((c) => `
      <tr><td class="mono">${esc(c.id)}</td><td>${esc(c.displayNames?.en || '')}</td>
      <td dir="rtl">${esc(c.displayNames?.fa || '')}</td><td dir="rtl">${esc(c.displayNames?.ps || '')}</td></tr>`).join('');
    $('#provinces-count').textContent = `${(r.provinces || []).length} provinces`;
    $('#provinces-table tbody').innerHTML = (r.provinces || []).map((p) => `
      <tr><td class="mono">${esc(p.id)}</td><td>${esc(p.displayNames?.en || '')}</td>
      <td dir="rtl">${esc(p.displayNames?.fa || '')}</td><td class="num">${num(p.articleCount ?? 0)}</td></tr>`).join('');
    return r;
  }

  // ---------------------------------------------------------------- drawer
  function openDrawer(title, html) {
    $('#drawer-title').textContent = title;
    $('#drawer-body').innerHTML = html;
    $('#drawer').hidden = false;
  }
  function closeDrawer() { $('#drawer').hidden = true; }

  // ---------------------------------------------------------------- events
  function wire() {
    $('#login-form').addEventListener('submit', async (e) => {
      e.preventDefault();
      $('#login-error').hidden = true;
      $('#login-submit').disabled = true;
      try {
        await signIn($('#login-username').value.trim(), $('#login-password').value);
        await boot();
      } catch (err) {
        const el = $('#login-error');
        el.textContent = err.message;
        el.hidden = false;
      } finally {
        $('#login-submit').disabled = false;
      }
    });

    $('#logout').addEventListener('click', () => signOut());
    $('#refresh').addEventListener('click', refreshCurrent);
    $('#drawer-close').addEventListener('click', closeDrawer);
    $$('.nav-item').forEach((b) => b.addEventListener('click', () => showView(b.dataset.view)));

    $('#art-search').addEventListener('click', () => {
      state.articles.offset = 0;
      state.articles.filters = {
        q: $('#art-q').value.trim(), status: $('#art-status').value,
        categoryId: $('#art-category').value, sourceId: $('#art-source').value,
        breaking: $('#art-breaking').checked,
      };
      loadArticles().catch((e) => toast(e.message, 'bad'));
    });
    $('#art-prev').addEventListener('click', () => { state.articles.offset = Math.max(0, state.articles.offset - state.articles.limit); loadArticles(); });
    $('#art-next').addEventListener('click', () => { state.articles.offset += state.articles.limit; loadArticles(); });

    $('#articles-table').addEventListener('click', async (e) => {
      const open = e.target.closest('[data-art-open]');
      const brk = e.target.closest('[data-art-break]');
      const hide = e.target.closest('[data-art-hide]');
      try {
        if (open) return openArticle(open.dataset.artOpen);
        if (brk) { await api(`/articles/${brk.dataset.artBreak}`, { method: 'PATCH', body: { isBreaking: brk.dataset.value === 'true' } }); toast('Breaking flag updated', 'ok'); return loadArticles(); }
        if (hide) { await api(`/articles/${hide.dataset.artHide}`, { method: 'PATCH', body: { status: hide.dataset.value } }); toast('Status updated', 'ok'); return loadArticles(); }
      } catch (err) { toast(err.message, 'bad'); }
    });

    $('#feed-search').addEventListener('click', () => {
      state.feeds.offset = 0;
      state.feeds.filters = {
        q: $('#feed-q').value.trim(), health: $('#feed-health').value,
        sourceType: $('#feed-type').value, enabled: $('#feed-enabled').value,
      };
      loadFeeds().catch((e) => toast(e.message, 'bad'));
    });
    $('#feed-prev').addEventListener('click', () => { state.feeds.offset = Math.max(0, state.feeds.offset - state.feeds.limit); loadFeeds(); });
    $('#feed-next').addEventListener('click', () => { state.feeds.offset += state.feeds.limit; loadFeeds(); });
    $('#feed-new').addEventListener('click', () => {
      openDrawer('Register feed', `
        <label class="field"><span>XML URL</span><input class="input" id="nf-url" placeholder="https://publisher.example/rss.xml" /></label>
        <label class="field"><span>Title (optional)</span><input class="input" id="nf-title" /></label>
        <label class="field"><span>Category key</span><input class="input" id="nf-cat" value="afghanistan-direct-news" /></label>
        <label class="field"><span>Language</span><input class="input" id="nf-lang" value="fa" /></label>
        <label class="field"><span>Source type</span><select class="input" id="nf-type">
          <option>DIRECT_PUBLISHER</option><option>VALIDATED_DIRECT</option><option>OFFICIAL_REALTIME</option>
          <option>OFFICIAL_INSTITUTION</option><option>AGGREGATOR_TOPIC</option><option>AGGREGATOR_SEARCH</option>
        </select></label>
        <label class="field"><span>Scope</span><input class="input" id="nf-scope" value="afghanistan" /></label>
        <button class="btn primary" id="nf-save">Register feed</button>`);
      $('#nf-save').onclick = async () => {
        try {
          await api('/feeds', { method: 'POST', body: {
            xmlUrl: $('#nf-url').value.trim(), title: $('#nf-title').value.trim(),
            categoryKey: $('#nf-cat').value.trim(), language: $('#nf-lang').value.trim(),
            sourceType: $('#nf-type').value, scope: $('#nf-scope').value.trim(), priority: 3,
          } });
          toast('Feed registered', 'ok');
          closeDrawer();
          loadFeeds();
        } catch (err) { toast(err.message, 'bad'); }
      };
    });
    $('#feeds-table').addEventListener('click', async (e) => {
      const open = e.target.closest('[data-feed-open]');
      const test = e.target.closest('[data-feed-test]');
      if (open) return openFeed(open.dataset.feedOpen).catch((err) => toast(err.message, 'bad'));
      if (test) return testFeed(test.dataset.feedTest);
    });
    $('#feeds-table').addEventListener('change', async (e) => {
      const t = e.target.closest('[data-feed-toggle]');
      if (!t) return;
      try {
        await api(`/feeds/${t.dataset.feedToggle}`, { method: 'PATCH', body: { enabled: t.checked } });
        toast(t.checked ? 'Feed enabled' : 'Feed disabled', 'ok');
      } catch (err) { toast(err.message, 'bad'); t.checked = !t.checked; }
    });

    $('#import-dry').addEventListener('click', () => runImport(false));
    $('#import-commit').addEventListener('click', () => {
      if (confirm('Commit this feed pack? New feeds are inserted, changed metadata is updated, missing feeds are flagged needs_review — nothing is deleted.')) runImport(true);
    });

    $('#push-preview').addEventListener('click', async () => {
      try {
        const res = await api('/push/preview', { method: 'POST', body: pushBody(false) });
        $('#push-result').innerHTML = `Audience: <strong>${num(res.audienceSize)}</strong> devices on <code>${esc(res.topic)}</code> · sent last hour: ${num(res.sentLastHour)} · daily: ${num(res.sentToday)}<br/>${esc(res.throttleHint || '')}`;
      } catch (e) { $('#push-result').textContent = e.message; }
    });
    $('#push-send').addEventListener('click', async () => {
      if (!confirm('Send this push to all subscribers of the selected topic?')) return;
      try {
        const res = await api('/push/send', { method: 'POST', body: pushBody(true) });
        toast(res.duplicate ? 'Duplicate suppressed (already sent)' : `Push ${res.status}`, res.duplicate ? '' : 'ok');
        $('#push-result').textContent = JSON.stringify(res, null, 2);
        loadPush();
      } catch (e) { toast(e.message, 'bad'); }
    });

    $('#waves').addEventListener('click', (e) => {
      const b = e.target.closest('[data-wave]');
      if (b) activateWave(Number(b.dataset.wave));
    });

    $('#audit-refresh').addEventListener('click', () => { state.audit.offset = 0; loadAudit().catch((e) => toast(e.message, 'bad')); });
    $('#audit-prev').addEventListener('click', () => { state.audit.offset = Math.max(0, state.audit.offset - state.audit.limit); loadAudit(); });
    $('#audit-next').addEventListener('click', () => { state.audit.offset += state.audit.limit; loadAudit(); });

    document.addEventListener('keydown', (e) => { if (e.key === 'Escape') closeDrawer(); });
  }

  // ---------------------------------------------------------------- boot
  async function boot() {
    $('#view-login').hidden = true;
    $('#view-console').hidden = false;
    try {
      const me = await api('/me');
      state.user = me;
      $('#session-info').textContent = `${me.username} · ${me.role} · session until ${fmtDate(me.expiresAt)}`;
      $('#env-pill').textContent = `signed in as ${me.role}`;
    } catch (e) {
      toast(`Session check failed: ${e.message}`, 'bad');
    }
    showView(state.view);
  }

  wire();
  if (state.token) { boot(); } else { $('#view-console').hidden = true; $('#view-login').hidden = false; }
})();
