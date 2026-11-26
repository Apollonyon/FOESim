// App version pill and user-authored changelog modal loaded from /changelog.json (zero innerHTML).
import { clearEl, el } from './dom.js';

const SEEN_VERSION_KEY = 'ps-seen-version';
const MAX_VISIBLE_IN_SERIES = 5;

function minorSeries(ver) {
  const clean = String(ver || '').trim().replace(/^v/i, '');
  const parts = clean.split('.');
  return parts.length >= 2 ? `${parts[0]}.${parts[1]}` : '';
}

export class ChangelogModal {
  constructor(modalEl) {
    this.modalEl = modalEl;
    this.data = null;
    this.bodyEl = null;
    this.titleVersionEl = null;
    this.#buildShell();
  }

  get isOpen() {
    return !this.modalEl.classList.contains('hidden');
  }

  async init(versionBtnEl) {
    try {
      const res = await fetch('/changelog.json', { cache: 'no-cache' });
      if (!res.ok) return;
      const parsed = await res.json();
      if (!parsed || typeof parsed.version !== 'string' || !parsed.version.trim()) return;
      const version = parsed.version.trim();
      this.data = parsed;

      if (versionBtnEl) {
        versionBtnEl.textContent = `v${version}`;
        versionBtnEl.classList.remove('hidden');
      }

      const entries = Array.isArray(parsed.entries) ? parsed.entries : [];
      const latest = entries[0];
      const hasContent = Boolean(
        latest &&
        ((Array.isArray(latest.notes) && latest.notes.some((n) => String(n).trim() !== '')) ||
          (typeof latest.title === 'string' && latest.title.trim() !== ''))
      );
      const seen = localStorage.getItem(SEEN_VERSION_KEY);
      if (!seen) {
        localStorage.setItem(SEEN_VERSION_KEY, version);
      } else if (hasContent && seen !== version) {
        this.open();
      }
    } catch {
      // Ignore changelog fetch failure so dashboard startup stays unaffected.
    }
  }

  open() {
    this.#render();
    this.modalEl.classList.remove('hidden');
    this.modalEl.setAttribute('aria-hidden', 'false');
  }

  close({ markSeen = true } = {}) {
    this.modalEl.classList.add('hidden');
    this.modalEl.setAttribute('aria-hidden', 'true');
    if (markSeen && this.data?.version) {
      localStorage.setItem(SEEN_VERSION_KEY, this.data.version.trim());
    }
  }

  #buildShell() {
    clearEl(this.modalEl);

    const backdrop = el('div', { className: 'editor-backdrop', onClick: () => this.close() });
    this.titleVersionEl = el('span', { className: 'mono changelog-ver-pill', text: 'v0.1.0' });

    const dialog = el('div', {
      className: 'editor-dialog changelog-dialog hud',
      role: 'dialog',
      'aria-modal': 'true',
      'aria-labelledby': 'changelog-title'
    },
      el('div', { className: 'editor-header' },
        el('div', { className: 'changelog-title-row' },
          el('div', { className: 'hud-kicker', id: 'changelog-title', text: "WHAT'S NEW IN FOES" }),
          this.titleVersionEl
        ),
        el('button', {
          type: 'button',
          className: 'icon-btn',
          title: 'Close changelog',
          'aria-label': 'Close changelog',
          text: '×',
          onClick: () => this.close()
        })
      ),
      (this.bodyEl = el('div', { className: 'editor-body' })),
      el('div', { className: 'editor-footer changelog-footer' },
        el('button', {
          type: 'button',
          className: 'btn btn-ink',
          text: 'Got it',
          onClick: () => this.close()
        })
      )
    );

    this.modalEl.append(backdrop, dialog);
  }

  #renderEntry(entry) {
    const head = el('div', { className: 'changelog-entry-head' },
      el('span', { className: 'mono changelog-ver-pill', text: entry.version ? `v${entry.version}` : '—' })
    );
    if (entry.date) head.appendChild(el('span', { className: 'mono editor-muted', text: entry.date }));
    if (entry.title) head.appendChild(el('strong', { text: entry.title }));

    const list = el('ul', { className: 'changelog-notes' });
    for (const note of (entry.notes || [])) {
      const clean = String(note).trim();
      if (clean) list.appendChild(el('li', { text: clean }));
    }

    const card = el('div', { className: 'changelog-entry' }, head);
    if (list.children.length > 0) card.appendChild(list);
    return card;
  }

  #renderGroup(summaryText, entries) {
    const list = el('div', { className: 'changelog-group-list' });
    for (const entry of entries) list.appendChild(this.#renderEntry(entry));
    return el('details', { className: 'editor-details changelog-group' },
      el('summary', { className: 'mono', text: summaryText }),
      list
    );
  }

  #render() {
    if (!this.bodyEl) return;
    clearEl(this.bodyEl);

    const version = (this.data?.version || '').trim();
    this.titleVersionEl.textContent = version ? `v${version}` : 'dev';

    const entries = Array.isArray(this.data?.entries) ? this.data.entries : [];
    const populated = entries.filter((entry) => (
      (typeof entry?.title === 'string' && entry.title.trim() !== '') ||
      (Array.isArray(entry?.notes) && entry.notes.some((n) => String(n).trim() !== ''))
    ));

    if (!populated.length) {
      this.bodyEl.appendChild(el('div', {
        className: 'editor-hint',
        text: 'No changelog notes for this version yet.'
      }));
      return;
    }

    const activeSeries = minorSeries(version) || minorSeries(populated[0]?.version);
    const currentEntries = [];
    const olderGroups = new Map();

    for (const entry of populated) {
      const series = minorSeries(entry.version);
      if (!activeSeries || series === activeSeries) {
        currentEntries.push(entry);
      } else {
        const key = series || 'other';
        if (!olderGroups.has(key)) olderGroups.set(key, []);
        olderGroups.get(key).push(entry);
      }
    }

    const visibleCurrent = currentEntries.slice(0, MAX_VISIBLE_IN_SERIES);
    const overflowCurrent = currentEntries.slice(MAX_VISIBLE_IN_SERIES);

    for (const entry of visibleCurrent) {
      this.bodyEl.appendChild(this.#renderEntry(entry));
    }

    if (overflowCurrent.length > 0) {
      const newest = overflowCurrent[0]?.version || '';
      const oldest = overflowCurrent[overflowCurrent.length - 1]?.version || '';
      const range = newest && oldest && newest !== oldest ? `v${oldest} – v${newest}` : (newest ? `v${newest}` : `v${activeSeries}.x`);
      const label = `${range} (${overflowCurrent.length} older ${overflowCurrent.length === 1 ? 'patch' : 'patches'})`;
      this.bodyEl.appendChild(this.#renderGroup(label, overflowCurrent));
    }

    for (const [series, groupEntries] of olderGroups.entries()) {
      const newest = groupEntries[0]?.version || '';
      const oldest = groupEntries[groupEntries.length - 1]?.version || '';
      const range = newest && oldest && newest !== oldest
        ? `v${oldest} – v${newest}`
        : (newest ? `v${newest}` : `v${series}.x`);
      const label = `${range} (${groupEntries.length} ${groupEntries.length === 1 ? 'release' : 'releases'})`;
      this.bodyEl.appendChild(this.#renderGroup(label, groupEntries));
    }
  }
}
