// Interactive spotlight tour introducing FOES in plain English (zero innerHTML).
import { clearEl, el } from './dom.js';

const SEEN_TOUR_KEY = 'ps-tour-seen';
const SPOTLIGHT_PAD = 6;
const VIEWPORT_MARGIN = 12;
const CARD_GAP = 14;

function isMobileOrTouch() {
  return (
    window.matchMedia('(max-width: 820px)').matches ||
    window.matchMedia('(pointer: coarse)').matches
  );
}

function getViewportBox(rootEl) {
  const vv = window.visualViewport;
  const vw = Math.round(vv ? vv.width : window.innerWidth);
  const vh = Math.round(vv ? vv.height : window.innerHeight);
  const cs = rootEl ? window.getComputedStyle(rootEl) : null;
  const insetTop = parseFloat(cs?.paddingTop) || 0;
  const insetRight = parseFloat(cs?.paddingRight) || 0;
  const insetBottom = parseFloat(cs?.paddingBottom) || 0;
  const insetLeft = parseFloat(cs?.paddingLeft) || 0;
  return {
    vw,
    vh,
    minLeft: VIEWPORT_MARGIN + insetLeft,
    minTop: VIEWPORT_MARGIN + insetTop,
    maxRight: vw - VIEWPORT_MARGIN - insetRight,
    maxBottom: vh - VIEWPORT_MARGIN - insetBottom
  };
}

function isElementVisible(node) {
  if (!node) return false;
  const style = window.getComputedStyle(node);
  if (style.display === 'none' || style.visibility === 'hidden' || style.opacity === '0') {
    return false;
  }
  const rect = node.getBoundingClientRect();
  return rect.width > 1 && rect.height > 1;
}

function getTargetRect(selectors, box) {
  if (!selectors || !selectors.length) return null;
  let left = Infinity;
  let top = Infinity;
  let right = -Infinity;
  let bottom = -Infinity;
  let found = false;

  for (const sel of selectors) {
    const node = typeof sel === 'string' ? document.querySelector(sel) : sel;
    if (!isElementVisible(node)) continue;
    const r = node.getBoundingClientRect();
    left = Math.min(left, r.left);
    top = Math.min(top, r.top);
    right = Math.max(right, r.right);
    bottom = Math.max(bottom, r.bottom);
    found = true;
  }

  if (!found) return null;
  const padMin = VIEWPORT_MARGIN / 2;
  const clLeft = Math.max(padMin, left - SPOTLIGHT_PAD);
  const clTop = Math.max(padMin, top - SPOTLIGHT_PAD);
  const clRight = Math.min(box.vw - padMin, right + SPOTLIGHT_PAD);
  const clBottom = Math.min(box.vh - padMin, bottom + SPOTLIGHT_PAD);
  return {
    left: clLeft,
    top: clTop,
    right: clRight,
    bottom: clBottom,
    width: Math.max(0, clRight - clLeft),
    height: Math.max(0, clBottom - clTop)
  };
}

export class TourGuide {
  constructor(rootEl, { getContext, onBeforeOpen, onStartElection } = {}) {
    this.rootEl = rootEl;
    this.getContext = getContext || (() => ({}));
    this.onBeforeOpen = onBeforeOpen || null;
    this.onStartElection = onStartElection || null;

    this.stepIndex = 0;
    this.steps = [];
    this.backdropEl = null;
    this.spotlightEl = null;
    this.cardEl = null;
    this.returnFocusEl = null;
    this._resizeRaf = 0;
    this._onResize = () => {
      if (!this.isOpen || this._resizeRaf) return;
      this._resizeRaf = requestAnimationFrame(() => {
        this._resizeRaf = 0;
        if (!this.isOpen) return;
        this.steps = this.#buildSteps();
        this.#renderStep({ refocus: false });
      });
    };
    this._onKeyDown = (e) => this.#handleKeyDown(e);

    this.#buildShell();
  }

  get isOpen() {
    return !this.rootEl.classList.contains('hidden');
  }

  hasSeen() {
    try {
      return localStorage.getItem(SEEN_TOUR_KEY) === '1';
    } catch {
      return false;
    }
  }

  markSeen() {
    try {
      localStorage.setItem(SEEN_TOUR_KEY, '1');
    } catch {
      // Ignore storage failures in private browsing modes.
    }
  }

  maybeAutoOpen() {
    if (this.hasSeen() || this.isOpen) return false;
    this.open({ triggerEl: null });
    return true;
  }

  open({ triggerEl = null, startStep = 0 } = {}) {
    this.returnFocusEl = triggerEl || null;
    this.onBeforeOpen?.();
    this.steps = this.#buildSteps();
    this.stepIndex = Math.max(0, Math.min(startStep, this.steps.length - 1));

    this.rootEl.classList.remove('hidden');
    this.rootEl.setAttribute('aria-hidden', 'false');
    window.addEventListener('resize', this._onResize);
    window.addEventListener('orientationchange', this._onResize);
    window.visualViewport?.addEventListener('resize', this._onResize);
    window.addEventListener('keydown', this._onKeyDown, true);

    this.#renderStep({ refocus: true });
  }

  close({ markSeen = true } = {}) {
    if (!this.isOpen) return;
    if (markSeen) this.markSeen();
    if (this._resizeRaf) {
      cancelAnimationFrame(this._resizeRaf);
      this._resizeRaf = 0;
    }

    this.rootEl.classList.add('hidden');
    this.rootEl.setAttribute('aria-hidden', 'true');
    window.removeEventListener('resize', this._onResize);
    window.removeEventListener('orientationchange', this._onResize);
    window.visualViewport?.removeEventListener('resize', this._onResize);
    window.removeEventListener('keydown', this._onKeyDown, true);

    if (this.returnFocusEl && typeof this.returnFocusEl.focus === 'function') {
      this.returnFocusEl.focus();
    }
    this.returnFocusEl = null;
  }

  next() {
    if (this.stepIndex < this.steps.length - 1) {
      this.stepIndex += 1;
      this.#renderStep();
    } else {
      this.close();
    }
  }

  prev() {
    if (this.stepIndex > 0) {
      this.stepIndex -= 1;
      this.#renderStep();
    }
  }

  #handleKeyDown(e) {
    if (!this.isOpen) return;
    if (e.key === 'Escape') {
      e.preventDefault();
      e.stopPropagation();
      this.close();
      return;
    }
    if (e.key === 'ArrowRight') {
      e.preventDefault();
      this.next();
      return;
    }
    if (e.key === 'ArrowLeft') {
      e.preventDefault();
      this.prev();
      return;
    }
    if (e.key === 'Tab' && this.cardEl) {
      const focusable = Array.from(
        this.cardEl.querySelectorAll('button:not([disabled]), [href], [tabindex]:not([tabindex="-1"])')
      );
      if (!focusable.length) return;
      const first = focusable[0];
      const last = focusable[focusable.length - 1];
      if (e.shiftKey && (document.activeElement === first || !this.cardEl.contains(document.activeElement))) {
        e.preventDefault();
        last.focus();
      } else if (!e.shiftKey && (document.activeElement === last || !this.cardEl.contains(document.activeElement))) {
        e.preventDefault();
        first.focus();
      }
    }
  }

  #buildShell() {
    clearEl(this.rootEl);

    this.backdropEl = el('div', {
      className: 'tour-backdrop',
      onClick: () => this.close()
    });
    this.spotlightEl = el('div', { className: 'tour-spotlight hidden', 'aria-hidden': 'true' });
    this.cardEl = el('div', {
      className: 'tour-card hud',
      role: 'dialog',
      'aria-modal': 'true',
      'aria-labelledby': 'tour-title',
      'aria-describedby': 'tour-desc'
    });

    this.rootEl.append(this.backdropEl, this.spotlightEl, this.cardEl);
  }

  #buildSteps() {
    const ctx = this.getContext() || {};
    const scenarioNames = Array.isArray(ctx.scenarioNames) && ctx.scenarioNames.length
      ? ctx.scenarioNames
      : ['European Parliament', 'UK General Election 2024', 'U.S. Presidential Election 2024'];
    const scenarioSummary = scenarioNames.length > 3
      ? `${scenarioNames.slice(0, 3).join(', ')}, and more`
      : scenarioNames.length === 3
        ? `${scenarioNames[0]}, ${scenarioNames[1]}, and ${scenarioNames[2]}`
        : scenarioNames.join(' and ');
    const hasEditorBtn = isElementVisible(document.getElementById('btn-editor'));
    const hasScenarioMenu = isElementVisible(document.getElementById('scenario-trigger'));
    const mobile = isMobileOrTouch();
    const verb = mobile ? 'Tap' : 'Click';

    return [
      {
        id: 'welcome',
        kicker: 'FOES',
        title: 'Free Open Election Simulator',
        body: 'Watch simulated election nights unfold live, or forecast who wins.',
        targets: null,
        primaryLabel: 'Quick tour →'
      },
      {
        id: 'scenario',
        kicker: '1 / 5',
        title: 'Pick an election',
        body: hasScenarioMenu
          ? `Switch between ${scenarioSummary}.`
          : 'Shows the active election map and rules.',
        targets: hasScenarioMenu ? ['#scenario-trigger'] : ['.brand']
      },
      {
        id: 'modes',
        kicker: '2 / 5',
        title: 'Live or Forecast',
        body: `${mobile ? 'Tap' : 'Press'} ▶ Start for a live vote count, or Forecast for win odds.`,
        targets: ['#mode-switch', '#btn-run', '#btn-run-forecast']
      },
      {
        id: 'map-layers',
        kicker: '3 / 5',
        title: 'Explore the map',
        body: `${verb} any region for vote details, or open Layers to switch map views.`,
        targets: ['#layers-hud']
      },
      {
        id: 'parliament',
        kicker: '4 / 5',
        title: 'Seats & coalitions',
        body: 'Open this bar to track seats, majorities, and coalitions.',
        targets: ['#hemicycle-hud']
      },
      {
        id: 'sandbox',
        kicker: '5 / 5',
        title: 'Test "what if?"',
        body: hasEditorBtn
          ? 'Shift polls or swap voting rules in ⚙, or customize maps in ✎ Editor.'
          : 'Tap ⚙ to shift polls, swap voting rules, or open the Editor.',
        targets: hasEditorBtn ? ['#btn-editor', '#btn-mobile-controls'] : ['#btn-mobile-controls']
      }
    ];
  }

  #renderStep({ refocus = true } = {}) {
    const step = this.steps[this.stepIndex];
    if (!step) return;

    const isFirst = this.stepIndex === 0;
    const isLast = this.stepIndex === this.steps.length - 1;
    const ctx = this.getContext() || {};
    const canAutoStart = isLast && ctx.canStartLive && typeof this.onStartElection === 'function';

    clearEl(this.cardEl);

    const header = el('div', { className: 'tour-head' },
      el('span', { className: 'hud-kicker tour-kicker', text: step.kicker }),
      el('button', {
        type: 'button',
        className: 'icon-btn tour-close-btn',
        title: 'Close tour (Esc)',
        'aria-label': 'Close tour',
        text: '✕',
        onClick: () => this.close()
      })
    );

    const title = el('h2', { id: 'tour-title', className: 'tour-title', text: step.title });
    const desc = el('p', { id: 'tour-desc', className: 'tour-desc', text: step.body });
    const bodyNodes = [title, desc];

    const dots = el('nav', { className: 'tour-dots', 'aria-label': 'Tour steps' },
      ...this.steps.map((s, idx) => el('button', {
        type: 'button',
        className: `tour-dot${idx === this.stepIndex ? ' is-active' : ''}`,
        'aria-label': `Go to step ${idx + 1}: ${s.title}`,
        'aria-current': idx === this.stepIndex ? 'step' : null,
        onClick: () => {
          this.stepIndex = idx;
          this.#renderStep();
        }
      }))
    );

    const actionsLeft = [];
    if (isFirst) {
      actionsLeft.push(el('button', {
        type: 'button',
        className: 'btn tour-ghost-btn',
        text: 'Skip',
        onClick: () => this.close()
      }));
    } else {
      actionsLeft.push(el('button', {
        type: 'button',
        className: 'btn',
        text: '← Back',
        onClick: () => this.prev()
      }));
    }

    const actionsRight = [];
    if (canAutoStart) {
      actionsRight.push(el('button', {
        type: 'button',
        className: 'btn btn-accent',
        'data-tour-primary': 'true',
        text: '▶ Start',
        onClick: () => {
          this.close();
          this.onStartElection();
        }
      }));
    } else {
      actionsRight.push(el('button', {
        type: 'button',
        className: `btn ${isFirst || isLast ? 'btn-accent' : 'btn-ink'}`,
        'data-tour-primary': 'true',
        text: step.primaryLabel || (isLast ? 'Done' : 'Next →'),
        onClick: () => this.next()
      }));
    }

    const footer = el('div', { className: 'tour-footer' },
      el('div', { className: 'tour-footer-left' }, ...actionsLeft),
      dots,
      el('div', { className: 'tour-footer-right' }, ...actionsRight)
    );

    this.cardEl.append(header, el('div', { className: 'tour-body' }, ...bodyNodes), footer);
    this.#positionCard();
    if (refocus) {
      requestAnimationFrame(() => {
        this.cardEl?.querySelector('[data-tour-primary]')?.focus({ preventScroll: true });
      });
    }
  }

  #positionCard() {
    const step = this.steps[this.stepIndex];
    if (!step || !this.cardEl) return;

    const box = getViewportBox(this.rootEl);
    const target = getTargetRect(step.targets, box);

    if (!target) {
      this.backdropEl.classList.remove('is-transparent');
      this.spotlightEl.classList.add('hidden');
      this.cardEl.classList.add('is-centered');
      this.cardEl.style.left = '';
      this.cardEl.style.top = '';
      return;
    }

    this.backdropEl.classList.add('is-transparent');
    this.spotlightEl.classList.remove('hidden');
    this.spotlightEl.style.left = `${Math.round(target.left)}px`;
    this.spotlightEl.style.top = `${Math.round(target.top)}px`;
    this.spotlightEl.style.width = `${Math.round(target.width)}px`;
    this.spotlightEl.style.height = `${Math.round(target.height)}px`;

    this.cardEl.classList.remove('is-centered');
    this.cardEl.style.left = '0px';
    this.cardEl.style.top = '0px';
    const cardRect = this.cardEl.getBoundingClientRect();
    const cw = cardRect.width || Math.min(320, box.maxRight - box.minLeft);
    const ch = cardRect.height || 160;

    const spaceBelow = box.maxBottom - target.bottom;
    const spaceAbove = target.top - box.minTop;
    const spaceRight = box.maxRight - target.right;
    const spaceLeft = target.left - box.minLeft;

    let left = box.minLeft;
    let top = box.minTop;

    const clampX = (x) => Math.max(box.minLeft, Math.min(x, box.maxRight - cw));
    const clampY = (y) => Math.max(box.minTop, Math.min(y, box.maxBottom - ch));
    const centerX = target.left + target.width / 2 - cw / 2;
    const centerY = target.top + target.height / 2 - ch / 2;

    if (spaceBelow >= ch + CARD_GAP) {
      top = target.bottom + CARD_GAP;
      left = clampX(centerX);
    } else if (spaceAbove >= ch + CARD_GAP) {
      top = target.top - ch - CARD_GAP;
      left = clampX(centerX);
    } else if (spaceRight >= cw + CARD_GAP) {
      left = target.right + CARD_GAP;
      top = clampY(centerY);
    } else if (spaceLeft >= cw + CARD_GAP) {
      left = target.left - cw - CARD_GAP;
      top = clampY(centerY);
    } else if (spaceBelow >= spaceAbove) {
      top = clampY(target.bottom + CARD_GAP);
      left = clampX(centerX);
    } else {
      top = clampY(target.top - ch - CARD_GAP);
      left = clampX(centerX);
    }

    this.cardEl.style.left = `${Math.round(left)}px`;
    this.cardEl.style.top = `${Math.round(top)}px`;
  }
}
