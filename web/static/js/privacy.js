// Privacy policy modal loaded from GET /api/privacy with safe self-hosting defaults (zero innerHTML).
import { clearEl, el } from './dom.js';

function ensurePeriod(text) {
  const s = String(text || '').trim();
  if (!s) return '';
  return /[.!?]$/.test(s) ? s : `${s}.`;
}

function safeMailtoHref(email) {
  const clean = String(email || '').trim();
  if (!clean || !/^[^\s@<>]+@[^\s@<>]+\.[^\s@<>]+$/.test(clean)) return '';
  return `mailto:${clean}`;
}

export class PrivacyModal {
  constructor(modalEl) {
    this.modalEl = modalEl;
    this.dialogEl = null;
    this.bodyEl = null;
    this.updatedPillEl = null;
    this.closeBtnEl = null;
    this.returnFocusEl = null;
    this.config = null;
    this._loadPromise = null;
    this._onKeyDown = (e) => this.#handleKeyDown(e);
    this.#buildShell();
  }

  get isOpen() {
    return !this.modalEl.classList.contains('hidden');
  }

  async #ensureLoaded() {
    if (this.config) return this.config;
    if (!this._loadPromise) {
      this._loadPromise = fetch('/api/privacy', { cache: 'no-cache' })
        .then((res) => (res.ok ? res.json() : { configured: false }))
        .catch(() => ({ configured: false }))
        .then((data) => {
          this.config = data && typeof data === 'object' ? data : { configured: false };
          return this.config;
        });
    }
    return this._loadPromise;
  }

  open({ triggerEl = null } = {}) {
    this.returnFocusEl = triggerEl || null;
    this.modalEl.classList.remove('hidden');
    this.modalEl.setAttribute('aria-hidden', 'false');
    window.addEventListener('keydown', this._onKeyDown, true);

    if (this.config) {
      this.#render();
    } else {
      clearEl(this.bodyEl);
      this.bodyEl.appendChild(el('p', { className: 'privacy-text', text: 'Loading privacy policy…' }));
      this.#ensureLoaded().then(() => {
        if (this.isOpen) this.#render();
      });
    }

    requestAnimationFrame(() => {
      this.closeBtnEl?.focus({ preventScroll: true });
    });
  }

  close() {
    if (!this.isOpen) return;
    this.modalEl.classList.add('hidden');
    this.modalEl.setAttribute('aria-hidden', 'true');
    window.removeEventListener('keydown', this._onKeyDown, true);
    if (window.location.hash === '#privacy') {
      window.history.replaceState(null, '', window.location.pathname + window.location.search);
    }
    if (this.returnFocusEl && typeof this.returnFocusEl.focus === 'function') {
      this.returnFocusEl.focus();
    }
    this.returnFocusEl = null;
  }

  #handleKeyDown(e) {
    if (!this.isOpen || e.key !== 'Tab' || !this.dialogEl) return;
    const focusable = Array.from(
      this.dialogEl.querySelectorAll('button:not([disabled]), a[href], [tabindex]:not([tabindex="-1"])')
    );
    if (!focusable.length) return;
    const first = focusable[0];
    const last = focusable[focusable.length - 1];
    if (e.shiftKey && (document.activeElement === first || !this.dialogEl.contains(document.activeElement))) {
      e.preventDefault();
      last.focus();
    } else if (!e.shiftKey && (document.activeElement === last || !this.dialogEl.contains(document.activeElement))) {
      e.preventDefault();
      first.focus();
    }
  }

  #buildShell() {
    clearEl(this.modalEl);

    const backdrop = el('div', { className: 'editor-backdrop', onClick: () => this.close() });
    this.updatedPillEl = el('span', { className: 'mono changelog-ver-pill hidden' });

    this.dialogEl = el('div', {
      className: 'editor-dialog changelog-dialog privacy-dialog hud',
      role: 'dialog',
      'aria-modal': 'true',
      'aria-labelledby': 'privacy-title'
    },
      el('div', { className: 'editor-header' },
        el('div', { className: 'changelog-title-row' },
          el('div', { className: 'hud-kicker', id: 'privacy-title', text: 'PRIVACY POLICY' }),
          this.updatedPillEl
        ),
        el('button', {
          type: 'button',
          className: 'icon-btn',
          title: 'Close privacy policy (Esc)',
          'aria-label': 'Close privacy policy',
          text: '×',
          onClick: () => this.close()
        })
      ),
      (this.bodyEl = el('div', { className: 'editor-body privacy-body' })),
      el('div', { className: 'editor-footer changelog-footer' },
        (this.closeBtnEl = el('button', {
          type: 'button',
          className: 'btn btn-ink',
          text: 'Close',
          onClick: () => this.close()
        }))
      )
    );

    this.modalEl.append(backdrop, this.dialogEl);
  }

  #emailNode(email) {
    const href = safeMailtoHref(email);
    if (!href) return document.createTextNode(String(email || ''));
    return el('a', { className: 'privacy-link', href, text: String(email).trim() });
  }

  #section(heading, items) {
    const card = el('section', { className: 'changelog-entry privacy-section' },
      el('h3', { className: 'privacy-heading', text: heading })
    );
    for (const item of items) {
      if (!item) continue;
      if (typeof item === 'string') {
        card.appendChild(el('p', { className: 'privacy-text', text: item }));
      } else if (Array.isArray(item)) {
        const cleanItems = item.filter(Boolean);
        if (!cleanItems.length) continue;
        const ul = el('ul', { className: 'changelog-notes' });
        for (const li of cleanItems) ul.appendChild(el('li', { text: li }));
        card.appendChild(ul);
      } else if (item.nodeType) {
        card.appendChild(item);
      }
    }
    return card;
  }

  #render() {
    if (!this.bodyEl) return;
    clearEl(this.bodyEl);

    const cfg = this.config || { configured: false };
    const isConfigured = Boolean(cfg.configured);

    if (isConfigured && cfg.last_updated) {
      this.updatedPillEl.textContent = `Updated ${cfg.last_updated}`;
      this.updatedPillEl.classList.remove('hidden');
    } else {
      this.updatedPillEl.classList.add('hidden');
    }

    const nodes = [
      el('div', { className: 'privacy-summary' },
        el('strong', { text: 'Short version: ' }),
        'FOES uses no cookies, no analytics, no ads, no user accounts, and makes no third-party requests from your browser while you use the app.'
      )
    ];

    if (!isConfigured) {
      nodes.push(
        el('div', { className: 'privacy-notice' },
          el('strong', { text: 'Instance notice not configured: ' }),
          'The operator of this FOES instance has not configured a privacy policy yet. The sections below describe only how the FOES software behaves as shipped. Hosting, reverse proxies, and server logs are the responsibility of this instance\'s operator.'
        )
      );
    } else {
      const controllerParts = [`Controller: ${cfg.operator}`];
      if (cfg.postal_address) controllerParts.push(cfg.postal_address);

      nodes.push(
        this.#section('1. Controller & Contact', [
          el('p', { className: 'privacy-text' },
            `${controllerParts.join(' · ')} · Contact: `,
            this.#emailNode(cfg.contact_email),
            '.'
          ),
          'Emails sent to this address are used solely to respond to your inquiry and deleted once resolved.'
        ])
      );

      const hostingBullets = [
        'Necessity & legal basis: Processing your IP address and basic HTTP request metadata (timestamp, requested path, status code) is technically required to deliver the site and maintain security (Art. 6(1)(f) GDPR).'
      ];
      if (cfg.hosting_provider) {
        hostingBullets.push(ensurePeriod(`Hosting provider: ${cfg.hosting_provider}`));
      }
      if (cfg.hosting_log_retention) {
        hostingBullets.push(ensurePeriod(`Server logs: ${cfg.hosting_log_retention}`));
      }
      if (cfg.data_transfer_note) {
        hostingBullets.push(ensurePeriod(`Recipients & transfers: ${cfg.data_transfer_note}`));
      }
      nodes.push(this.#section('2. Hosting & Server Delivery', [hostingBullets]));
    }

    const stepOffset = isConfigured ? 2 : 0;
    const num = (n) => `${n + stepOffset}.`;

    nodes.push(
      this.#section(`${num(1)} FOES Software Behavior (In-Memory Processing)`, [
        [
          'In-memory abuse protection: FOES temporarily holds your IP address in server memory solely to cap concurrent live streams and forecast requests per IP and to rate-limit custom scenario submissions (1-minute window). The FOES application itself does not write IP addresses to disk.',
          'Custom scenarios & share links: Scenarios submitted via the Scenario Editor (or loaded from a shared #share= link) are stored in server memory for up to 15 minutes, not linked to an account or name, then deleted automatically.',
          'Share links & screenshots: Shared #share= URLs encode scenario edits in the URL hash. Map screenshots are rendered locally in your browser and never uploaded.'
        ],
        'These points describe FOES as shipped. Any reverse-proxy, web-server, or container logs are controlled by the operator of the instance you are visiting.'
      ]),
      this.#section(`${num(2)} Local Browser Storage (No Cookies)`, [
        'FOES stores three strictly functional preferences in your browser localStorage: theme choice (ps-theme), last seen version (ps-seen-version), and whether the intro tour was dismissed (ps-tour-seen). This data never leaves your browser and can be cleared anytime in your browser settings.'
      ]),
      this.#section(`${num(3)} No Third-Party Browser Requests`, [
        'All fonts, scripts, and map files are self-hosted (Content-Security-Policy: default-src \'self\', Referrer-Policy: no-referrer). External links such as GitHub only load if you click them.'
      ])
    );

    if (isConfigured) {
      const rightsItems = [
        el('p', { className: 'privacy-text' },
          'Under the GDPR, you have the right to request access (Art. 15), rectification (Art. 16), erasure (Art. 17), or restriction (Art. 18) of personal data processed about you. Contact: ',
          this.#emailNode(cfg.contact_email),
          '.'
        ),
        'Right to object (Art. 21 GDPR): Where processing is based on legitimate interest (Art. 6(1)(f) GDPR), you have the right to object on grounds relating to your particular situation.'
      ];
      if (cfg.supervisory_authority) {
        rightsItems.push(ensurePeriod(`Right to lodge a complaint (Art. 77 GDPR): You may contact a data protection supervisory authority, such as ${cfg.supervisory_authority}`));
      } else {
        rightsItems.push('Right to lodge a complaint (Art. 77 GDPR): You have the right to lodge a complaint with a data protection supervisory authority.');
      }
      nodes.push(
        this.#section(`${num(4)} Your Rights`, rightsItems),
        this.#section(`${num(5)} Changes & Self-Hosted Instances`, [
          'This privacy policy may be updated if hosting or software behavior changes; the latest revision date is shown at the top of this dialog. FOES is open-source software (AGPL-3.0); self-hosted instances are operated independently by their respective hosts.'
        ])
      );
    }

    this.bodyEl.append(...nodes);
  }
}
