// Full-featured in-browser scenario & simulation modifier editor built via dom.js (zero innerHTML).
import { clearEl, el, safeColor, swatch } from './dom.js';
import { computeScenarioPatch, encodeSharePayload } from './share.js';

const METHOD_OPTIONS = [
  { value: '', label: 'Default / Inherit' },
  { value: 'sainte_lague', label: 'Sainte-Laguë' },
  { value: 'dhondt', label: "D'Hondt" },
  { value: 'hare', label: 'Hare-Niemeyer' },
  { value: 'fptp', label: 'First Past the Post (FPTP)' },
  { value: 'stv', label: 'Single Transferable Vote (STV approx)' }
];

const GEOPACK_OPTIONS = [
  { value: '', label: 'None (Synthetic Precincts)' },
  { value: 'eu_nuts3', label: 'EU NUTS-3 (eu_nuts3)' },
  { value: 'us_counties', label: 'US Counties (us_counties)' },
  { value: 'uk_constituencies', label: 'UK Constituencies (uk_constituencies)' }
];

const CHART_OPTIONS = [
  { value: 'hemicycle', label: 'Hemicycle' },
  { value: 'bar', label: 'Bar (270-to-Win Style)' }
];

const WAVES_OPTIONS = [
  { value: '0', label: 'Auto (3 animated / 1 instant)' },
  { value: '1', label: '1 Wave' },
  { value: '2', label: '2 Waves' },
  { value: '3', label: '3 Waves' },
  { value: '4', label: '4 Waves' }
];

const BASE_AXIS_LABELS = ['Economic', 'Social', 'EU / Axis 2', 'Defense', 'Environment', 'Migration'];

const DEFAULT_MODIFIERS = {
  swing: 0.15,
  uncertainty: 0.15,
  tactical: 0,
  shiftParty: '',
  shiftVal: 0,
  temp: 0.45,
  seed: 42,
  wavesOverride: 0,
  methodOverride: ''
};

function round4(n) {
  return Math.round(n * 10000) / 10000;
}

function clamp(n, lo, hi) {
  return Math.min(hi, Math.max(lo, n));
}

function normalizeVec6(arr, fill = 0) {
  return Array.from({ length: 6 }, (_, i) => {
    const v = Number(arr?.[i]);
    return Number.isFinite(v) ? v : fill;
  });
}

function toHex6(color) {
  const safe = safeColor(color, '#64748b');
  if (/^#[0-9a-fA-F]{6}$/.test(safe)) return safe.toLowerCase();
  if (/^#[0-9a-fA-F]{3}$/.test(safe)) {
    const [, r, g, b] = safe;
    return `#${r}${r}${g}${g}${b}${b}`.toLowerCase();
  }
  const m = /^rgb\(\s*(\d{1,3})\s*,\s*(\d{1,3})\s*,\s*(\d{1,3})\s*\)$/.exec(safe);
  if (m) {
    const hex = [m[1], m[2], m[3]]
      .map((x) => clamp(parseInt(x, 10) || 0, 0, 255).toString(16).padStart(2, '0'))
      .join('');
    return `#${hex}`;
  }
  return '#64748b';
}

function makeSelect(options, currentVal, ariaLabel, onChange, className = '') {
  const sel = el('select', { className: className || null, 'aria-label': ariaLabel });
  for (const opt of options) {
    sel.appendChild(el('option', { value: opt.value, text: opt.label }));
  }
  sel.value = currentVal;
  sel.addEventListener('change', (e) => onChange(e.target.value));
  return sel;
}

function makeTextInput(value, ariaLabel, onInput, { placeholder = '', maxlength = null, className = '' } = {}) {
  const inp = el('input', {
    type: 'text',
    className: className || null,
    placeholder: placeholder || null,
    maxlength,
    'aria-label': ariaLabel
  });
  inp.value = value ?? '';
  inp.addEventListener('input', (e) => onInput(e.target.value));
  return inp;
}

function makeNumInput(value, ariaLabel, { min = null, max = null, step = '1', className = '', onInput, onChange } = {}) {
  const inp = el('input', {
    type: 'number',
    min: min !== null ? String(min) : null,
    max: max !== null ? String(max) : null,
    step: String(step),
    className: className || null,
    'aria-label': ariaLabel
  });
  inp.value = String(value);
  if (onInput) inp.addEventListener('input', (e) => onInput(e.target.value));
  if (onChange) inp.addEventListener('change', (e) => onChange(e.target.value));
  return inp;
}

function makeSliderNumRow({ label, help = '', min, max, step, value, ariaLabel, decimals = 2, onChange }) {
  const valNum = clamp(Number.isFinite(Number(value)) ? Number(value) : 0, min, max);
  const range = el('input', {
    type: 'range',
    min: String(min),
    max: String(max),
    step: String(step),
    'aria-label': `${ariaLabel} slider`
  });
  range.value = String(valNum);

  const numInp = makeNumInput(round4(valNum).toFixed(decimals), `${ariaLabel} value`, {
    min,
    max,
    step,
    className: 'editor-num-input',
    onInput: (raw) => {
      const parsed = parseFloat(raw);
      if (!Number.isFinite(parsed)) return;
      const clamped = clamp(round4(parsed), min, max);
      range.value = String(clamped);
      onChange(clamped);
    },
    onChange: (raw) => {
      const parsed = parseFloat(raw);
      const clamped = clamp(round4(Number.isFinite(parsed) ? parsed : valNum), min, max);
      numInp.value = clamped.toFixed(decimals);
      range.value = String(clamped);
      onChange(clamped);
    }
  });

  range.addEventListener('input', (e) => {
    const next = clamp(round4(parseFloat(e.target.value) || 0), min, max);
    numInp.value = next.toFixed(decimals);
    onChange(next);
  });

  const row = el('div', { className: 'editor-slider-num-row' },
    el('span', { className: 'editor-mini-label', text: label }),
    range,
    numInp
  );
  if (!help) return row;
  return el('div', { className: 'editor-control-block' },
    row,
    el('div', { className: 'editor-help', text: help })
  );
}

export class ScenarioEditor {
  constructor(modalEl, { onApplied } = {}) {
    this.modalEl = modalEl;
    this.onApplied = onApplied;

    this.baseScenario = null;
    this.draft = null;
    this.baseModifiers = { ...DEFAULT_MODIFIERS };
    this.modifiers = { ...DEFAULT_MODIFIERS };

    this.baseTurnoutByRegion = {};
    this.turnoutScales = {};
    this.globalTurnoutScale = 100;
    this.groupOffsets = {};
    this.globalAxisOffsets = [0, 0, 0, 0, 0, 0];

    this.activeTab = 'overview'; // 'overview' | 'parties' | 'regions' | 'voters' | 'json'
    this.partyScope = 'groups';
    this.regionFilter = '';
    this.voterRegionId = '';
    this.jsonDirty = false;
    this.isSubmitting = false;

    this.statsBarEl = null;
    this.errorEl = null;
    this.bodyEl = null;
    this.nameInputEl = null;
    this.tabButtons = {};
    this.applyReadyBtn = null;
    this.applyStartBtn = null;
    this.jsonAreaEl = null;
    this.fileInputEl = null;

    this.#buildShell();
  }

  get isOpen() {
    return !this.modalEl.classList.contains('hidden');
  }

  open(baseScenario, currentScenario = null, modifiers = null, opts = {}) {
    this.baseScenario = structuredClone(baseScenario || currentScenario);
    this.baseScenarioId = opts.baseScenarioId || '';
    const rawMod = modifiers || {};
    const uncVal = rawMod.uncertainty ?? rawMod.swing ?? DEFAULT_MODIFIERS.swing;
    const mergedMods = {
      ...DEFAULT_MODIFIERS,
      ...rawMod,
      swing: uncVal,
      uncertainty: uncVal
    };
    this.baseModifiers = { ...mergedMods };
    this.modifiers = { ...mergedMods };
    this.#loadDraftFrom(currentScenario || this.baseScenario);
    this.#clearError();
    this.activeTab = 'overview';
    this.partyScope = this.draft?.groups?.length ? 'groups' : (this.draft?.parties?.length ? 'top' : (this.draft?.regions?.[0]?.id || 'top'));
    this.voterRegionId = this.draft?.regions?.[0]?.id || '';
    this.regionFilter = '';
    this.modalEl.classList.remove('hidden');
    this.modalEl.setAttribute('aria-hidden', 'false');
    this.#render();
    this.nameInputEl?.focus();
  }

  close() {
    if (this.isSubmitting) return;
    this.modalEl.classList.add('hidden');
    this.modalEl.setAttribute('aria-hidden', 'true');
  }

  #axisLabels() {
    const custom2 = this.draft?.meta?.axis2_label?.trim();
    return [
      BASE_AXIS_LABELS[0],
      BASE_AXIS_LABELS[1],
      custom2 || BASE_AXIS_LABELS[2],
      BASE_AXIS_LABELS[3],
      BASE_AXIS_LABELS[4],
      BASE_AXIS_LABELS[5]
    ];
  }

  #loadDraftFrom(sc) {
    this.draft = structuredClone(sc);
    this.groupOffsets = {};
    this.globalAxisOffsets = [0, 0, 0, 0, 0, 0];
    this.turnoutScales = {};
    this.baseTurnoutByRegion = {};
    this.jsonDirty = false;

    const src = this.baseScenario || this.draft;
    for (const r of (src.regions || [])) {
      this.baseTurnoutByRegion[r.id] = (r.groups || []).map((g) => Number(g.turnout) || 0);
    }

    let sumRegionScales = 0;
    let regionCount = 0;
    for (const r of (this.draft.regions || [])) {
      if (!this.baseTurnoutByRegion[r.id]) {
        this.baseTurnoutByRegion[r.id] = (r.groups || []).map((g) => Number(g.turnout) || 0);
      }
      const baseArr = this.baseTurnoutByRegion[r.id];
      const curArr = r.groups || [];
      let sumRatio = 0;
      let count = 0;
      for (let i = 0; i < curArr.length && i < baseArr.length; i++) {
        if (baseArr[i] > 0) {
          sumRatio += curArr[i].turnout / baseArr[i];
          count++;
        }
      }
      const scale = count > 0 ? Math.round((sumRatio / count) * 100) : 100;
      this.turnoutScales[r.id] = scale;
      sumRegionScales += scale;
      regionCount++;
    }
    this.globalTurnoutScale = regionCount > 0 ? Math.round(sumRegionScales / regionCount) : 100;
    if (!this.voterRegionId || !(this.draft.regions || []).some((r) => r.id === this.voterRegionId)) {
      this.voterRegionId = this.draft.regions?.[0]?.id || '';
    }
    if (this.nameInputEl) {
      this.nameInputEl.value = this.draft.name || '';
    }
  }

  #buildShell() {
    clearEl(this.modalEl);

    const backdrop = el('div', { className: 'editor-backdrop', onClick: () => this.close() });
    const dialog = el('div', {
      className: 'editor-dialog hud',
      role: 'dialog',
      'aria-modal': 'true',
      'aria-labelledby': 'editor-title'
    });

    this.nameInputEl = el('input', {
      type: 'text',
      className: 'editor-name-input',
      'aria-label': 'Custom scenario name',
      maxlength: '120',
      onInput: (e) => {
        if (!this.draft) return;
        this.draft.name = e.target.value;
        if (this.activeTab === 'json' && this.jsonAreaEl && !this.jsonDirty) {
          this.jsonAreaEl.value = JSON.stringify(this.draft, null, 2);
        }
      }
    });

    const header = el('div', { className: 'editor-header' },
      el('div', { className: 'editor-title-wrap' },
        el('div', { className: 'hud-kicker', id: 'editor-title', text: 'SCENARIO & SIMULATION EDITOR' }),
        this.nameInputEl
      ),
      el('button', {
        type: 'button',
        className: 'icon-btn',
        title: 'Close editor',
        'aria-label': 'Close editor',
        text: '×',
        onClick: () => this.close()
      })
    );

    const tabs = el('div', { className: 'editor-tabs', role: 'tablist' });
    const tabSpecs = [
      ['overview', 'Modifiers & Meta'],
      ['parties', 'Parties & Groups'],
      ['regions', 'Regions'],
      ['voters', 'Voters'],
      ['json', 'JSON']
    ];
    for (const [key, label] of tabSpecs) {
      const btn = el('button', {
        type: 'button',
        className: 'editor-tab-btn',
        role: 'tab',
        text: label,
        onClick: () => this.#switchTab(key)
      });
      this.tabButtons[key] = btn;
      tabs.appendChild(btn);
    }

    this.statsBarEl = el('div', { className: 'editor-stats-bar' });
    this.errorEl = el('div', { className: 'editor-error hidden', role: 'alert' });
    this.bodyEl = el('div', { className: 'editor-body' });

    const resetBtn = el('button', {
      type: 'button',
      className: 'btn',
      text: 'Reset to Base',
      onClick: () => {
        if (this.isSubmitting || !this.baseScenario) return;
        this.#clearError();
        this.modifiers = { ...DEFAULT_MODIFIERS };
        this.#loadDraftFrom(this.baseScenario);
        this.#render();
      }
    });

    this.shareBtn = el('button', {
      type: 'button',
      className: 'btn',
      title: 'Copy a permanent shareable URL for your edits',
      text: 'Copy Share Link',
      onClick: () => this.#copyShareLink()
    });

    const cancelBtn = el('button', {
      type: 'button',
      className: 'btn',
      text: 'Cancel',
      onClick: () => this.close()
    });

    this.applyReadyBtn = el('button', {
      type: 'button',
      className: 'btn',
      text: 'Apply & Ready',
      onClick: () => this.#submit(false)
    });

    this.applyStartBtn = el('button', {
      type: 'button',
      className: 'btn btn-ink',
      text: 'Apply & Start ▶',
      onClick: () => this.#submit(true)
    });

    const footer = el('div', { className: 'editor-footer' },
      el('div', { className: 'editor-footer-left' }, resetBtn, this.shareBtn),
      el('div', { className: 'editor-footer-right' }, cancelBtn, this.applyReadyBtn, this.applyStartBtn)
    );

    dialog.appendChild(header);
    dialog.appendChild(tabs);
    dialog.appendChild(this.statsBarEl);
    dialog.appendChild(this.errorEl);
    dialog.appendChild(this.bodyEl);
    dialog.appendChild(footer);

    this.modalEl.appendChild(backdrop);
    this.modalEl.appendChild(dialog);
  }

  #updateStatsBar() {
    if (!this.statsBarEl || !this.draft) return;
    clearEl(this.statsBarEl);
    const regions = this.draft.regions || [];
    const totalSeats = regions.reduce((acc, r) => acc + (parseInt(r.seats, 10) || 0), 0);
    const partySet = new Set();
    for (const p of (this.draft.parties || [])) {
      if (p.id) partySet.add(p.id);
    }
    for (const r of regions) {
      for (const p of (r.parties || [])) {
        if (p.id) partySet.add(p.id);
      }
    }

    const items = [
      ['Regions', regions.length],
      ['Seats', totalSeats],
      ['Parties', partySet.size],
      ['Uncertainty', this.modifiers.swing.toFixed(2)],
      ['Temp', this.modifiers.temp.toFixed(2)],
      ['Seed', this.modifiers.seed]
    ];
    for (const [k, v] of items) {
      this.statsBarEl.appendChild(el('span', null, `${k}: `, el('strong', { className: 'mono', text: String(v) })));
    }
  }

  #setError(msg) {
    this.errorEl.textContent = msg;
    this.errorEl.classList.remove('hidden');
  }

  #clearError() {
    this.errorEl.textContent = '';
    this.errorEl.classList.add('hidden');
  }

  #syncJsonIfDirty() {
    if (!this.jsonDirty || !this.jsonAreaEl) return true;
    try {
      const parsed = JSON.parse(this.jsonAreaEl.value);
      if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
        throw new Error('Scenario root must be a JSON object');
      }
      this.#loadDraftFrom(parsed);
      this.#updateStatsBar();
      this.#clearError();
      return true;
    } catch (err) {
      this.#setError(`JSON error: ${err.message}`);
      return false;
    }
  }

  #switchTab(nextTab) {
    if (this.activeTab === nextTab) return;
    if (this.activeTab === 'json' && !this.#syncJsonIfDirty()) return;
    this.#clearError();
    this.activeTab = nextTab;
    this.#render();
  }

  #render() {
    if (!this.draft) return;
    this.nameInputEl.value = this.draft.name || '';
    for (const [k, btn] of Object.entries(this.tabButtons)) {
      const active = k === this.activeTab;
      btn.classList.toggle('active', active);
      btn.setAttribute('aria-selected', String(active));
    }
    this.#updateStatsBar();

    clearEl(this.bodyEl);
    if (this.activeTab === 'overview') this.#renderOverviewTab();
    else if (this.activeTab === 'parties') this.#renderPartiesTab();
    else if (this.activeTab === 'regions') this.#renderRegionsTab();
    else if (this.activeTab === 'voters') this.#renderVotersTab();
    else this.#renderJsonTab();
  }

  #ensureMeta() {
    if (!this.draft.meta || typeof this.draft.meta !== 'object') {
      this.draft.meta = {};
    }
    return this.draft.meta;
  }

  #buildAxesGrid(vec, { min = -1, max = 1, step = 0.01, decimals = 2, prefix = 'Axis', onUpdate }) {
    const labels = this.#axisLabels();
    const grid = el('div', { className: 'editor-axes-grid' });
    const norm = normalizeVec6(vec, min === 0 ? 1 : 0);
    for (let i = 0; i < 6; i++) {
      grid.appendChild(makeSliderNumRow({
        label: labels[i],
        min,
        max,
        step,
        decimals,
        value: norm[i],
        ariaLabel: `${prefix} ${labels[i]}`,
        onChange: (val) => onUpdate(i, val)
      }));
    }
    return grid;
  }

  #renderOverviewTab() {
    // 1. Simulation Modifiers
    const seedInput = makeNumInput(this.modifiers.seed, 'Random seed', {
      step: '1',
      onInput: (raw) => {
        const parsed = parseInt(raw, 10);
        if (!Number.isNaN(parsed)) {
          this.modifiers.seed = parsed;
          this.#updateStatsBar();
        }
      }
    });
    const seedWrap = el('div', { className: 'editor-inline-row' },
      Object.assign(seedInput, { className: 'editor-inline-grow' }),
      el('button', {
        type: 'button',
        className: 'btn editor-btn-sm',
        title: 'Randomize seed',
        text: '↻',
        onClick: () => {
          const next = Math.floor(Math.random() * 9999) + 1;
          this.modifiers.seed = next;
          seedInput.value = String(next);
          this.#updateStatsBar();
        }
      })
    );

    const shiftChoices = [{ value: '', label: 'None' }];
    const shiftList = this.draft?.groups?.length ? this.draft.groups : (this.draft?.parties || []);
    for (const item of shiftList) {
      shiftChoices.push({ value: item.id, label: `${item.short_name || item.id} — ${item.name || item.id}` });
    }
    const shiftSliderWrap = makeSliderNumRow({
      label: 'Shift Amount',
      help: 'Directional national appeal shift applied to the selected party/group across all regions (even at Uncertainty 0).',
      min: -0.40,
      max: 0.40,
      step: 0.02,
      decimals: 2,
      value: this.modifiers.shiftVal || 0,
      ariaLabel: 'National party shift magnitude',
      onChange: (val) => {
        this.modifiers.shiftVal = val;
      }
    });

    const modSec = el('section', { className: 'editor-section' },
      el('div', { className: 'editor-section-title', text: 'Simulation Modifiers' }),
      makeSliderNumRow({
        label: 'Uncertainty (Volatility)',
        help: 'Random election-night polling uncertainty. 0 repeats the calibrated baseline; 0.15 is typical polling error. Note: symmetric noise pulls winner-take-all means toward 50/50.',
        min: 0,
        max: 2,
        step: 0.01,
        decimals: 2,
        value: this.modifiers.swing,
        ariaLabel: 'Uncertainty volatility',
        onChange: (val) => {
          this.modifiers.swing = val;
          this.modifiers.uncertainty = val;
          this.#updateStatsBar();
        }
      }),
      el('label', { className: 'editor-field' },
        el('span', { text: 'National Party Shift Target' }),
        makeSelect(shiftChoices, this.modifiers.shiftParty || '', 'National party shift target', (v) => {
          this.modifiers.shiftParty = v;
        })
      ),
      shiftSliderWrap,
      makeSliderNumRow({
        label: 'Voter Temp',
        help: 'How strictly voters stick to their closest party. Low (0.20) = disciplined partisan voting; high (1.0+) = lots of cross-party split.',
        min: 0.05,
        max: 5,
        step: 0.05,
        decimals: 2,
        value: this.modifiers.temp,
        ariaLabel: 'Voter temperature',
        onChange: (val) => {
          this.modifiers.temp = val;
          this.#updateStatsBar();
        }
      }),
      makeSliderNumRow({
        label: 'Tactical Voting',
        help: 'Strategic voting squeeze (0 = calibrated baseline). Under FPTP, trailing 3rd+ parties shift toward the closer top-2 contender; under PR thresholds, sub-threshold parties shift toward the closest viable party.',
        min: 0,
        max: 1,
        step: 0.05,
        decimals: 2,
        value: this.modifiers.tactical || 0,
        ariaLabel: 'Tactical voting squeeze strength',
        onChange: (val) => {
          this.modifiers.tactical = val;
          this.#updateStatsBar();
        }
      }),
      el('div', { className: 'editor-grid-3' },
        el('label', { className: 'editor-field' },
          el('span', { text: 'Random Seed' }),
          seedWrap,
          el('span', { className: 'editor-help', text: 'Same seed + same settings always gives the same night.' })
        ),
        el('label', { className: 'editor-field' },
          el('span', { text: 'Reporting Waves' }),
          makeSelect(WAVES_OPTIONS, String(this.modifiers.wavesOverride || 0), 'Reporting waves', (v) => {
            this.modifiers.wavesOverride = parseInt(v, 10) || 0;
          }),
          el('span', { className: 'editor-help', text: 'Batches per precinct. 1 sends final totals in one go.' })
        ),
        el('label', { className: 'editor-field' },
          el('span', { text: 'System Override' }),
          makeSelect(METHOD_OPTIONS, this.modifiers.methodOverride || '', 'Global electoral system override', (v) => {
            this.modifiers.methodOverride = v;
          }),
          el('span', { className: 'editor-help', text: 'Force every region onto one counting rule (drops two-tier district splits).' })
        )
      )
    );

    // 2. Global Electorate Shifts
    const turnoutInput = makeSliderNumRow({
      label: 'Global Turnout %',
      help: 'Scales turnout up or down everywhere at once (clamped to 100%).',
      min: 25,
      max: 200,
      step: 5,
      decimals: 0,
      value: this.globalTurnoutScale,
      ariaLabel: 'Global turnout scale percentage',
      onChange: (pct) => {
        this.globalTurnoutScale = pct;
        const factor = pct / 100;
        for (const r of (this.draft.regions || [])) {
          this.turnoutScales[r.id] = pct;
          const baseArr = this.baseTurnoutByRegion[r.id] || [];
          (r.groups || []).forEach((g, idx) => {
            const baseT = idx < baseArr.length ? baseArr[idx] : Number(g.turnout || 0);
            g.turnout = clamp(round4(baseT * factor), 0, 1);
          });
        }
      }
    });

    const shiftDetails = el('details', { className: 'editor-details' },
      el('summary', { text: 'Global Policy Shift (Nudge every voter group along an issue axis)' }),
      el('div', { className: 'editor-help', text: 'Moves all voter groups together before ballots are cast (-1 left/open, +1 right/strict).' }),
      this.#buildAxesGrid(this.globalAxisOffsets, {
        min: -0.5,
        max: 0.5,
        step: 0.01,
        decimals: 2,
        prefix: 'Global Voter Shift',
        onUpdate: (ax, nextOffset) => {
          const prevOffset = this.globalAxisOffsets[ax] || 0;
          const delta = nextOffset - prevOffset;
          if (!delta) return;
          this.globalAxisOffsets[ax] = nextOffset;
          for (const r of (this.draft.regions || [])) {
            for (const vg of (r.groups || [])) {
              const ideal = normalizeVec6(vg.ideal, 0);
              ideal[ax] = clamp(round4(ideal[ax] + delta), -1, 1);
              vg.ideal = ideal;
            }
          }
        }
      })
    );

    const electorateSec = el('section', { className: 'editor-section' },
      el('div', { className: 'editor-section-title', text: 'Global Electorate Shifts' }),
      turnoutInput,
      shiftDetails
    );

    // 3. Scenario Metadata
    const meta = this.#ensureMeta();
    const descInput = el('textarea', {
      className: 'editor-textarea',
      rows: '2',
      maxlength: '280',
      placeholder: 'Optional note about what you changed (e.g. "+0.15 Green swing, 5% nationwide threshold")',
      'aria-label': 'Scenario description'
    });
    descInput.value = meta.description || '';
    descInput.addEventListener('input', (e) => {
      meta.description = e.target.value;
    });

    const hideCheck = el('input', { type: 'checkbox' });
    hideCheck.checked = Boolean(meta.hide_coalitions);
    hideCheck.addEventListener('change', (e) => {
      meta.hide_coalitions = e.target.checked;
    });

    const poles = [
      meta.axis2_poles?.[0] ?? '',
      meta.axis2_poles?.[1] ?? '',
      meta.axis2_poles?.[2] ?? ''
    ];
    const setPole = (idx, val) => {
      if (!Array.isArray(meta.axis2_poles)) meta.axis2_poles = ['', '', ''];
      meta.axis2_poles[idx] = val;
    };

    const geoDetails = el('details', { className: 'editor-details' },
      el('summary', { text: 'Map & Geometry Paths (Advanced)' }),
      el('div', { className: 'editor-hint', text: 'Paths must start with /geo/ and end with .json (server validation enforced).' }),
      el('div', { className: 'editor-grid-3' },
        el('label', { className: 'editor-field' },
          el('span', { text: 'Regions URL' }),
          makeTextInput(meta.regions_url || '', 'Regions TopoJSON URL', (v) => { meta.regions_url = v; })
        ),
        el('label', { className: 'editor-field' },
          el('span', { text: 'Units URL' }),
          makeTextInput(meta.units_url || '', 'Units TopoJSON URL', (v) => { meta.units_url = v; })
        ),
        el('label', { className: 'editor-field' },
          el('span', { text: 'ViewBox' }),
          makeTextInput(meta.view_box || '', 'SVG ViewBox', (v) => { meta.view_box = v; })
        )
      )
    );

    const metaSec = el('section', { className: 'editor-section' },
      el('div', { className: 'editor-section-title', text: 'Scenario Metadata & Clock' }),
      el('label', { className: 'editor-field' },
        el('span', { text: 'Description / Notes' }),
        descInput,
        el('span', { className: 'editor-help', text: 'Shown under the header and kept in share links and exported JSON.' })
      ),
      el('div', { className: 'editor-grid-3' },
        el('label', { className: 'editor-field' },
          el('span', { text: 'Parliament / Event Title' }),
          makeTextInput(meta.title || '', 'Scenario title', (v) => { meta.title = v; })
        ),
        el('label', { className: 'editor-field' },
          el('span', { text: 'Chart Type' }),
          makeSelect(CHART_OPTIONS, meta.chart_type || 'hemicycle', 'Chart type', (v) => { meta.chart_type = v; })
        ),
        el('label', { className: 'editor-field' },
          el('span', { text: 'Seat Label' }),
          makeTextInput(meta.seat_label || '', 'Seat label', (v) => { meta.seat_label = v; }, { placeholder: 'seats' })
        ),
        el('label', { className: 'editor-field' },
          el('span', { text: 'Region Label' }),
          makeTextInput(meta.region_label || '', 'Region label', (v) => { meta.region_label = v; }, { placeholder: 'Member State' })
        ),
        el('label', { className: 'editor-field' },
          el('span', { text: 'Unit Label' }),
          makeTextInput(meta.unit_label || '', 'Unit label', (v) => { meta.unit_label = v; }, { placeholder: 'County' })
        ),
        el('label', { className: 'editor-field' },
          el('span', { text: 'Overview Pill Label' }),
          makeTextInput(meta.overview_label || '', 'Overview label', (v) => { meta.overview_label = v; }, { placeholder: 'Europe' })
        ),
        el('label', { className: 'editor-field' },
          el('span', { text: 'Timezone' }),
          makeTextInput(meta.timezone || '', 'Timezone label', (v) => { meta.timezone = v; }, { placeholder: 'CET' })
        ),
        el('label', { className: 'editor-field' },
          el('span', { text: 'Clock Start Hour' }),
          makeNumInput(meta.clock_start_hour ?? 18, 'Clock start hour', {
            min: 0,
            max: 30,
            step: 0.5,
            onInput: (raw) => {
              const n = parseFloat(raw);
              if (Number.isFinite(n)) meta.clock_start_hour = clamp(n, 0, 30);
            }
          })
        ),
        el('label', { className: 'editor-field' },
          el('span', { text: 'Clock End Hour' }),
          makeNumInput(meta.clock_end_hour ?? 25.5, 'Clock end hour', {
            min: 0,
            max: 30,
            step: 0.5,
            onInput: (raw) => {
              const n = parseFloat(raw);
              if (Number.isFinite(n)) meta.clock_end_hour = clamp(n, 0, 30);
            }
          })
        ),
        el('label', { className: 'editor-field' },
          el('span', { text: 'Axis 2 Label' }),
          makeTextInput(meta.axis2_label || '', 'Axis 2 label', (v) => { meta.axis2_label = v; }, { placeholder: 'Pro-EU' })
        ),
        el('label', { className: 'editor-field' },
          el('span', { text: 'Wealth Label' }),
          makeTextInput(meta.wealth_label || '', 'Wealth label', (v) => { meta.wealth_label = v; })
        ),
        el('label', { className: 'editor-field' },
          el('span', { text: 'Age Label' }),
          makeTextInput(meta.age_label || '', 'Age label', (v) => { meta.age_label = v; })
        )
      ),
      el('div', { className: 'editor-grid-3' },
        el('label', { className: 'editor-field' },
          el('span', { text: 'Axis 2 Pole (Low)' }),
          makeTextInput(poles[0], 'Axis 2 low pole', (v) => setPole(0, v))
        ),
        el('label', { className: 'editor-field' },
          el('span', { text: 'Axis 2 Pole (Mid)' }),
          makeTextInput(poles[1], 'Axis 2 middle pole', (v) => setPole(1, v))
        ),
        el('label', { className: 'editor-field' },
          el('span', { text: 'Axis 2 Pole (High)' }),
          makeTextInput(poles[2], 'Axis 2 high pole', (v) => setPole(2, v))
        )
      ),
      el('div', { className: 'editor-inline-row' },
        el('label', { className: 'editor-field editor-inline-grow' },
          el('span', { text: 'Attribution' }),
          makeTextInput(meta.attribution || '', 'Map attribution', (v) => { meta.attribution = v; })
        ),
        el('label', { className: 'editor-check-label' }, hideCheck, el('span', { text: 'Hide Coalitions Tab' }))
      ),
      geoDetails
    );

    // 4. Previous Seats Baseline
    const prevIds = this.#collectBaselineEntries();
    const prevGrid = el('div', { className: 'editor-grid-3' });
    for (const entry of prevIds) {
      const curSeats = this.draft.prev_seats?.[entry.id] ?? 0;
      prevGrid.appendChild(el('label', { className: 'editor-field' },
        el('span', { text: `${entry.shortName} (${entry.id})` }),
        makeNumInput(curSeats, `Previous seats for ${entry.shortName}`, {
          min: 0,
          max: 2000,
          step: '1',
          onInput: (raw) => {
            const val = Math.max(0, parseInt(raw, 10) || 0);
            if (val === 0) {
              if (this.draft.prev_seats) {
                delete this.draft.prev_seats[entry.id];
                if (Object.keys(this.draft.prev_seats).length === 0) delete this.draft.prev_seats;
              }
            } else {
              if (!this.draft.prev_seats) this.draft.prev_seats = {};
              this.draft.prev_seats[entry.id] = val;
            }
          }
        })
      ));
    }
    const prevDetails = el('details', { className: 'editor-details' },
      el('summary', { text: 'Previous Election Seat Baseline (prev_seats)' }),
      el('div', { className: 'editor-help', text: 'Last election seat totals used to compute +/- gains in the parliament ledger.' }),
      prevGrid
    );

    this.bodyEl.appendChild(modSec);
    this.bodyEl.appendChild(electorateSec);
    this.bodyEl.appendChild(metaSec);
    this.bodyEl.appendChild(prevDetails);
  }

  #collectBaselineEntries() {
    if (this.draft.groups?.length) {
      return this.draft.groups.map((g) => ({ id: g.id, shortName: g.short_name || g.id }));
    }
    const seen = new Set();
    const out = [];
    for (const p of (this.draft.parties || [])) {
      if (p.id && !seen.has(p.id)) {
        seen.add(p.id);
        out.push({ id: p.id, shortName: p.short_name || p.id });
      }
    }
    for (const r of (this.draft.regions || [])) {
      for (const p of (r.parties || [])) {
        if (p.id && !seen.has(p.id)) {
          seen.add(p.id);
          out.push({ id: p.id, shortName: p.short_name || p.id });
        }
      }
    }
    return out;
  }

  #renderPartiesTab() {
    const groups = this.draft.groups || [];
    const regions = this.draft.regions || [];
    const hasTopParties = Boolean(this.draft.parties?.length);

    const options = [];
    if (groups.length > 0) {
      options.push({ value: 'groups', label: 'EU Political Groups (All Regions)' });
    }
    if (hasTopParties) {
      options.push({ value: 'top', label: 'National / Scenario Parties' });
    }
    for (const r of regions) {
      if (r.parties?.length || hasTopParties) {
        options.push({ value: r.id, label: `${r.name} (${r.id.toUpperCase()})` });
      }
    }

    const validValues = new Set(options.map((o) => o.value));
    if (!validValues.has(this.partyScope)) {
      this.partyScope = options[0]?.value || 'top';
    }

    const scopeSel = makeSelect(options, this.partyScope, 'Party editing scope', (val) => {
      this.partyScope = val;
      this.#renderPartiesTabBody();
    });

    this.partiesWrapEl = el('div', { className: 'editor-list' });
    this.bodyEl.appendChild(el('div', { className: 'editor-toolbar' },
      el('label', { className: 'editor-scope-label', text: 'Edit Scope' }, scopeSel)
    ));
    this.bodyEl.appendChild(this.partiesWrapEl);
    this.#renderPartiesTabBody();
  }

  #renderPartiesTabBody() {
    clearEl(this.partiesWrapEl);

    if (this.partyScope === 'groups' && this.draft.groups?.length) {
      this.partiesWrapEl.appendChild(el('div', {
        className: 'editor-hint',
        text: 'Edit EU political group appearance, policy centroid, and bulk appeal shift across all affiliated member parties.'
      }));

      for (const g of this.draft.groups) {
        const hex = toHex6(g.color);
        const sw = swatch(hex);
        const colorInput = el('input', {
          type: 'color',
          className: 'editor-color-input',
          title: `Color for ${g.short_name}`,
          'aria-label': `Color for ${g.short_name}`
        });
        colorInput.value = hex;
        colorInput.addEventListener('input', (e) => {
          g.color = e.target.value;
          sw.style.background = safeColor(g.color);
        });

        const curShift = this.groupOffsets[g.id] || 0;
        const posDetails = el('details', { className: 'editor-details' },
          el('summary', { text: 'Policy Position (6 Axes)' }),
          this.#buildAxesGrid(g.position, {
            min: -1,
            max: 1,
            step: 0.01,
            decimals: 2,
            prefix: g.short_name,
            onUpdate: (ax, val) => {
              const pos = normalizeVec6(g.position, 0);
              pos[ax] = val;
              g.position = pos;
            }
          })
        );

        this.partiesWrapEl.appendChild(el('div', { className: 'editor-card editor-party-row' },
          el('div', { className: 'editor-item-head' },
            colorInput,
            sw,
            el('span', { className: 'mono editor-code', text: g.id.toUpperCase() }),
            el('div', { className: 'editor-inline-row editor-inline-grow' },
              makeTextInput(g.short_name, `${g.id} short name`, (v) => { g.short_name = v; }, { placeholder: 'Short' }),
              makeTextInput(g.name, `${g.id} full name`, (v) => { g.name = v; }, { placeholder: 'Full name', className: 'editor-inline-grow' })
            )
          ),
          makeSliderNumRow({
            label: 'Appeal Shift',
            help: 'Adds or subtracts popularity from every party in this group.',
            min: -1,
            max: 1,
            step: 0.01,
            decimals: 2,
            value: curShift,
            ariaLabel: `${g.short_name} appeal shift`,
            onChange: (next) => {
              const prev = this.groupOffsets[g.id] || 0;
              const delta = next - prev;
              this.groupOffsets[g.id] = next;
              this.#applyGroupDelta(g.id, delta);
            }
          }),
          posDetails
        ));
      }
      return;
    }

    const region = this.partyScope === 'top'
      ? null
      : (this.draft.regions || []).find((r) => r.id === this.partyScope);
    const usesRegionParties = Boolean(region?.parties?.length);
    const parties = usesRegionParties ? region.parties : (this.draft.parties || []);

    const toolbar = el('div', { className: 'editor-toolbar' },
      el('span', {
        className: 'editor-hint',
        text: region && !usesRegionParties
          ? 'Parties are shared nationally across regions; Region Bonus adjusts only this region.'
          : 'Edit party color, names, group link, appeal, and 6-axis policy position.'
      }),
      el('button', {
        type: 'button',
        className: 'btn editor-btn-sm',
        text: '+ Add Party',
        onClick: () => {
          this.#addPartyToList(parties);
        }
      })
    );
    this.partiesWrapEl.appendChild(toolbar);

    if (!parties.length) {
      this.partiesWrapEl.appendChild(el('div', { className: 'editor-hint', text: 'No editable parties in this scope.' }));
      return;
    }

    const groupOpts = (this.draft.groups || []).map((g) => ({ value: g.id, label: `${g.short_name} (${g.id})` }));
    const deleteDisabled = parties.length <= 1;

    for (const p of parties) {
      const hex = toHex6(p.color);
      const sw = swatch(hex);
      const colorInput = el('input', {
        type: 'color',
        className: 'editor-color-input',
        title: `Color for ${p.short_name}`,
        'aria-label': `Color for ${p.short_name}`
      });
      colorInput.value = hex;
      colorInput.addEventListener('input', (e) => {
        p.color = e.target.value;
        sw.style.background = safeColor(p.color);
      });

      const delBtn = el('button', {
        type: 'button',
        className: 'btn editor-btn-sm editor-btn-danger',
        text: 'Delete',
        onClick: () => {
          if (parties.length <= 1) return;
          this.#deletePartyFromList(parties, p.id, usesRegionParties ? region : null);
        }
      });
      delBtn.disabled = deleteDisabled;

      const metaInputs = [
        makeTextInput(p.short_name, `${p.id} short name`, (v) => { p.short_name = v; }, { placeholder: 'Short' }),
        makeTextInput(p.name, `${p.id} name`, (v) => { p.name = v; }, { placeholder: 'Party name', className: 'editor-inline-grow' })
      ];
      if (groupOpts.length > 0) {
        const curGroup = p.group_id || (groupOpts.some((o) => o.value === 'ni') ? 'ni' : groupOpts[0].value);
        metaInputs.push(makeSelect(groupOpts, curGroup, `${p.short_name} EU group`, (v) => {
          p.group_id = v;
        }));
      }

      const controls = [
        makeSliderNumRow({
          label: 'Appeal',
          help: 'Extra popularity bonus or penalty for this party, separate from its policy positions.',
          min: -2,
          max: 2,
          step: 0.01,
          decimals: 2,
          value: p.appeal || 0,
          ariaLabel: `${p.short_name} appeal`,
          onChange: (val) => { p.appeal = val; }
        })
      ];

      if (region) {
        controls.push(makeSliderNumRow({
          label: 'Region Bonus',
          help: 'Local boost or penalty in this region only.',
          min: -2,
          max: 2,
          step: 0.01,
          decimals: 2,
          value: region.party_bonus?.[p.id] || 0,
          ariaLabel: `${p.short_name} bonus in ${region.name}`,
          onChange: (val) => {
            if (Math.abs(val) < 1e-6) {
              if (region.party_bonus) {
                delete region.party_bonus[p.id];
                if (Object.keys(region.party_bonus).length === 0) delete region.party_bonus;
              }
            } else {
              if (!region.party_bonus) region.party_bonus = {};
              region.party_bonus[p.id] = val;
            }
          }
        }));
      }

      const posDetails = el('details', { className: 'editor-details' },
        el('summary', { text: 'Policy Position (6 Axes)' }),
        this.#buildAxesGrid(p.position, {
          min: -1,
          max: 1,
          step: 0.01,
          decimals: 2,
          prefix: p.short_name || p.id,
          onUpdate: (ax, val) => {
            const pos = normalizeVec6(p.position, 0);
            pos[ax] = val;
            p.position = pos;
          }
        })
      );

      this.partiesWrapEl.appendChild(el('div', { className: 'editor-card editor-party-row' },
        el('div', { className: 'editor-item-head' },
          colorInput,
          sw,
          el('span', { className: 'mono editor-code', text: p.id }),
          el('div', { className: 'editor-inline-row editor-inline-grow' }, ...metaInputs),
          delBtn
        ),
        el('div', { className: 'editor-party-controls' }, ...controls),
        posDetails
      ));
    }
  }

  #addPartyToList(parties) {
    if (parties.length >= 64) {
      this.#setError('Maximum of 64 parties per region/list reached.');
      return;
    }
    this.#clearError();
    const existing = new Set(parties.map((p) => p.id));
    for (const p of (this.draft.parties || [])) existing.add(p.id);
    let idx = parties.length + 1;
    let id = `party_${idx}`;
    while (existing.has(id)) {
      idx++;
      id = `party_${idx}`;
    }
    const newParty = {
      id,
      name: `Custom Party ${idx}`,
      short_name: `P${idx}`,
      color: '#6366f1',
      position: [0, 0, 0, 0, 0, 0],
      appeal: 0
    };
    if (this.draft.groups?.length) {
      newParty.group_id = this.draft.groups.some((g) => g.id === 'ni') ? 'ni' : this.draft.groups[0].id;
    }
    parties.push(newParty);
    this.#updateStatsBar();
    this.#renderPartiesTabBody();
  }

  #deletePartyFromList(parties, partyId, scopedRegion) {
    const idx = parties.findIndex((p) => p.id === partyId);
    if (idx === -1 || parties.length <= 1) return;
    parties.splice(idx, 1);

    const regionsToClean = scopedRegion ? [scopedRegion] : (this.draft.regions || []);
    for (const r of regionsToClean) {
      if (r.party_bonus && partyId in r.party_bonus) {
        delete r.party_bonus[partyId];
        if (Object.keys(r.party_bonus).length === 0) delete r.party_bonus;
      }
    }
    if (!this.draft.groups?.length && this.draft.prev_seats && partyId in this.draft.prev_seats) {
      delete this.draft.prev_seats[partyId];
      if (Object.keys(this.draft.prev_seats).length === 0) delete this.draft.prev_seats;
    }
    this.#updateStatsBar();
    this.#renderPartiesTabBody();
  }

  #applyGroupDelta(groupId, delta) {
    if (!delta) return;
    for (const p of (this.draft.parties || [])) {
      if ((p.group_id || 'ni') === groupId) {
        p.appeal = round4((Number(p.appeal) || 0) + delta);
      }
    }
    for (const r of (this.draft.regions || [])) {
      for (const p of (r.parties || [])) {
        if ((p.group_id || 'ni') === groupId) {
          p.appeal = round4((Number(p.appeal) || 0) + delta);
        }
      }
    }
  }

  #renderRegionsTab() {
    const filterInput = el('input', {
      type: 'search',
      className: 'editor-search-input',
      placeholder: 'Filter regions by name or code…',
      'aria-label': 'Filter regions'
    });
    filterInput.value = this.regionFilter;
    filterInput.addEventListener('input', (e) => {
      this.regionFilter = e.target.value;
      this.#renderRegionsList();
    });

    this.regionsListEl = el('div', { className: 'editor-list' });
    this.bodyEl.appendChild(el('div', { className: 'editor-toolbar' }, filterInput));
    this.bodyEl.appendChild(el('div', {
      className: 'editor-help',
      text: 'Threshold is ignored under FPTP/STV. Poll Close Hour staggers when regions begin counting on the night clock. District Seats reserve seats 1-per-district before allocating the rest.'
    }));
    this.bodyEl.appendChild(this.regionsListEl);
    this.#renderRegionsList();
  }

  #renderRegionsList() {
    clearEl(this.regionsListEl);
    const q = this.regionFilter.trim().toLowerCase();
    const regions = (this.draft.regions || []).filter((r) => (
      !q || r.name.toLowerCase().includes(q) || r.id.toLowerCase().includes(q)
    ));

    if (!regions.length) {
      this.regionsListEl.appendChild(el('div', { className: 'editor-hint', text: 'No regions match that filter.' }));
      return;
    }

    for (const r of regions) {
      const geoOpts = [...GEOPACK_OPTIONS];
      if (r.geo_pack && !geoOpts.some((o) => o.value === r.geo_pack)) {
        geoOpts.push({ value: r.geo_pack, label: r.geo_pack });
      }

      const nutsCheck = el('input', { type: 'checkbox' });
      nutsCheck.checked = Boolean(r.use_nuts3);
      nutsCheck.addEventListener('change', (e) => { r.use_nuts3 = e.target.checked; });

      const turnoutInp = makeNumInput(this.turnoutScales[r.id] ?? 100, `${r.name} turnout scale percentage`, {
        min: 25,
        max: 200,
        step: 5,
        onInput: (raw) => {
          const pct = parseFloat(raw);
          if (Number.isNaN(pct)) return;
          const clampedPct = clamp(pct, 25, 200);
          this.turnoutScales[r.id] = clampedPct;
          const factor = clampedPct / 100;
          const baseArr = this.baseTurnoutByRegion[r.id] || [];
          (r.groups || []).forEach((g, idx) => {
            const baseTurnout = idx < baseArr.length ? baseArr[idx] : Number(g.turnout || 0);
            g.turnout = clamp(round4(baseTurnout * factor), 0, 1);
          });
        }
      });
      if (!(r.groups?.length)) turnoutInp.disabled = true;

      const localParties = r.parties?.length ? r.parties : (this.draft.parties || []);

      const tiltsDetails = this.#buildDistrictTiltsSection(r);

      this.regionsListEl.appendChild(el('div', { className: 'editor-card editor-region-card' },
        el('div', { className: 'editor-item-head' },
          el('span', { className: 'mono editor-code', text: r.id.toUpperCase() }),
          makeTextInput(r.name, `${r.id} region name`, (v) => { r.name = v; }, { className: 'editor-inline-grow' }),
          el('button', {
            type: 'button',
            className: 'btn editor-btn-sm',
            text: `Voters (${r.groups?.length || 0}) →`,
            onClick: () => {
              this.voterRegionId = r.id;
              this.#switchTab('voters');
            }
          }),
          el('button', {
            type: 'button',
            className: 'btn editor-btn-sm',
            text: `Parties (${localParties.length}) →`,
            onClick: () => {
              this.partyScope = r.id;
              this.#switchTab('parties');
            }
          })
        ),
        el('div', { className: 'editor-region-grid' },
          el('label', { className: 'editor-field' },
            el('span', { text: 'Seats' }),
            makeNumInput(r.seats, `${r.name} seats`, {
              min: 1,
              max: 2000,
              onInput: (raw) => {
                const val = parseInt(raw, 10);
                if (!Number.isNaN(val)) {
                  r.seats = clamp(val, 1, 2000);
                  this.#updateStatsBar();
                }
              }
            })
          ),
          el('label', { className: 'editor-field' },
            el('span', { text: 'Method' }),
            makeSelect(METHOD_OPTIONS, r.method || '', `${r.name} allocation method`, (v) => { r.method = v; })
          ),
          el('label', { className: 'editor-field' },
            el('span', { text: 'Threshold %' }),
            makeNumInput(round4((Number(r.threshold) || 0) * 100), `${r.name} threshold %`, {
              min: 0,
              max: 100,
              step: 0.5,
              onInput: (raw) => {
                const pct = parseFloat(raw);
                if (!Number.isNaN(pct)) r.threshold = clamp(round4(pct / 100), 0, 1);
              }
            })
          ),
          el('label', { className: 'editor-field' },
            el('span', { text: 'Poll Close Hour' }),
            makeNumInput(r.poll_close_hour ?? 0, `${r.name} poll close hour`, {
              min: 0,
              max: 30,
              step: 0.5,
              onInput: (raw) => {
                const h = parseFloat(raw);
                if (Number.isFinite(h)) r.poll_close_hour = clamp(h, 0, 30);
              }
            })
          ),
          el('label', { className: 'editor-field' },
            el('span', { text: 'Precincts' }),
            makeNumInput(r.precincts ?? 1, `${r.name} precincts`, {
              min: 1,
              max: 5000,
              onInput: (raw) => {
                const n = parseInt(raw, 10);
                if (!Number.isNaN(n)) r.precincts = clamp(n, 1, 5000);
              }
            })
          ),
          el('label', { className: 'editor-field' },
            el('span', { text: 'District Seats' }),
            makeNumInput(r.district_seats ?? 0, `${r.name} district seats`, {
              min: 0,
              max: 500,
              onInput: (raw) => {
                const n = parseInt(raw, 10);
                if (!Number.isNaN(n)) r.district_seats = clamp(n, 0, 500);
              }
            })
          ),
          el('label', { className: 'editor-field' },
            el('span', { text: 'GeoPack' }),
            makeSelect(geoOpts, r.geo_pack || '', `${r.name} geopack`, (v) => { r.geo_pack = v; })
          ),
          el('label', { className: 'editor-field' },
            el('span', { text: 'Turnout Scale %' }),
            turnoutInp
          )
        ),
        el('div', { className: 'editor-inline-row' },
          el('label', { className: 'editor-check-label' }, nutsCheck, el('span', { text: 'Legacy Use NUTS-3 Flag' }))
        ),
        tiltsDetails
      ));
    }
  }

  #buildDistrictTiltsSection(r) {
    const details = el('details', { className: 'editor-details' });
    const countEntries = () => Object.values(r.district_tilts || {}).reduce((acc, m) => acc + Object.keys(m).length, 0);
    const summaryEl = el('summary', { text: `District Tilts (${countEntries()})` });
    const contentEl = el('div');

    const renderTiltsContent = () => {
      clearEl(contentEl);
      summaryEl.textContent = `District Tilts (${countEntries()})`;

      const tiltList = el('div', { className: 'editor-tilt-list' });
      const entries = Object.entries(r.district_tilts || {});
      if (entries.length === 0) {
        tiltList.appendChild(el('div', { className: 'editor-hint', text: 'No district/sub-region demographic tilts configured.' }));
      }
      for (const [distId, vgMap] of entries) {
        for (const [vgId, mult] of Object.entries(vgMap || {})) {
          tiltList.appendChild(el('div', { className: 'editor-tilt-row' },
            el('span', { className: 'mono editor-code', text: distId }),
            el('span', { className: 'mono', text: vgId }),
            makeNumInput(mult, `Multiplier for ${distId} / ${vgId}`, {
              min: 0.05,
              max: 10,
              step: 0.05,
              className: 'editor-num-input',
              onInput: (raw) => {
                const v = parseFloat(raw);
                if (Number.isFinite(v) && v > 0) {
                  r.district_tilts[distId][vgId] = clamp(round4(v), 0.05, 10);
                }
              }
            }),
            el('button', {
              type: 'button',
              className: 'btn editor-btn-sm editor-btn-danger',
              text: '×',
              title: 'Remove tilt',
              onClick: () => {
                delete r.district_tilts[distId][vgId];
                if (Object.keys(r.district_tilts[distId]).length === 0) delete r.district_tilts[distId];
                if (Object.keys(r.district_tilts).length === 0) delete r.district_tilts;
                renderTiltsContent();
              }
            })
          ));
        }
      }
      contentEl.appendChild(tiltList);

      const vgOptions = (r.groups || []).filter((g) => Boolean(g.id)).map((g) => ({
        value: g.id,
        label: `${g.name || g.id} (${g.id})`
      }));

      if (vgOptions.length === 0) {
        contentEl.appendChild(el('div', {
          className: 'editor-hint',
          text: 'Voter groups need a non-empty ID (editable in the Voters tab) before adding district tilts.'
        }));
        return;
      }

      let newDist = '';
      let newVg = vgOptions[0].value;
      let newMult = 1.1;

      const distInput = makeTextInput('', 'District or county prefix ID', (v) => { newDist = v.trim(); }, {
        placeholder: 'District / Prefix ID'
      });
      const vgSelect = makeSelect(vgOptions, newVg, 'Voter group ID', (v) => { newVg = v; });
      const multInput = makeNumInput(1.1, 'Tilt multiplier', {
        min: 0.05,
        max: 10,
        step: 0.05,
        className: 'editor-num-input',
        onInput: (raw) => {
          const v = parseFloat(raw);
          if (Number.isFinite(v) && v > 0) newMult = clamp(round4(v), 0.05, 10);
        }
      });

      contentEl.appendChild(el('div', { className: 'editor-inline-row' },
        Object.assign(distInput, { className: 'editor-inline-grow' }),
        vgSelect,
        multInput,
        el('button', {
          type: 'button',
          className: 'btn editor-btn-sm',
          text: '+ Add Tilt',
          onClick: () => {
            if (!newDist) {
              this.#setError('District ID is required to add a district tilt.');
              return;
            }
            this.#clearError();
            if (!r.district_tilts) r.district_tilts = {};
            if (!r.district_tilts[newDist]) r.district_tilts[newDist] = {};
            r.district_tilts[newDist][newVg] = newMult;
            renderTiltsContent();
          }
        })
      ));
    };

    renderTiltsContent();
    details.appendChild(summaryEl);
    details.appendChild(contentEl);
    return details;
  }

  #renderVotersTab() {
    const regions = this.draft.regions || [];
    if (!regions.length) {
      this.bodyEl.appendChild(el('div', { className: 'editor-hint', text: 'No regions in scenario.' }));
      return;
    }

    const regOpts = regions.map((r) => ({ value: r.id, label: `${r.name} (${r.id.toUpperCase()})` }));
    if (!regions.some((r) => r.id === this.voterRegionId)) {
      this.voterRegionId = regions[0].id;
    }

    const regSel = makeSelect(regOpts, this.voterRegionId, 'Region for voter groups', (val) => {
      this.voterRegionId = val;
      this.#renderVotersList();
    });

    const addBtn = el('button', {
      type: 'button',
      className: 'btn editor-btn-sm',
      text: '+ Add Voter Group',
      onClick: () => this.#addVoterGroup()
    });

    this.votersWrapEl = el('div', { className: 'editor-list' });
    this.bodyEl.appendChild(el('div', { className: 'editor-toolbar' },
      el('label', { className: 'editor-scope-label editor-inline-grow', text: 'Region' }, regSel),
      addBtn
    ));
    this.bodyEl.appendChild(this.votersWrapEl);
    this.#renderVotersList();
  }

  #renderVotersList() {
    clearEl(this.votersWrapEl);
    const region = (this.draft.regions || []).find((r) => r.id === this.voterRegionId);
    if (!region) return;
    const groups = region.groups || [];
    if (!groups.length) {
      this.votersWrapEl.appendChild(el('div', { className: 'editor-hint', text: 'No voter groups in this region.' }));
      return;
    }

    const deleteDisabled = groups.length <= 1;

    groups.forEach((vg, idx) => {
      const idInput = el('input', {
        type: 'text',
        className: 'mono',
        placeholder: 'group_id (optional)',
        'aria-label': `${vg.name || 'Voter group'} ID`
      });
      idInput.value = vg.id || '';
      idInput.addEventListener('change', (e) => {
        const nextId = e.target.value.trim();
        const prevId = vg.id || '';
        if (nextId === prevId) {
          idInput.value = prevId;
          return;
        }
        if (nextId && groups.some((other) => other !== vg && other.id === nextId)) {
          this.#setError(`Duplicate voter group ID "${nextId}" in region ${region.name}.`);
          idInput.value = prevId;
          return;
        }
        this.#clearError();
        vg.id = nextId;
        if (prevId && region.district_tilts) {
          for (const [dKey, tiltMap] of Object.entries(region.district_tilts)) {
            if (prevId in tiltMap) {
              const mult = tiltMap[prevId];
              delete tiltMap[prevId];
              if (nextId) tiltMap[nextId] = mult;
            }
            if (Object.keys(tiltMap).length === 0) delete region.district_tilts[dKey];
          }
          if (Object.keys(region.district_tilts).length === 0) delete region.district_tilts;
        }
      });

      const delBtn = el('button', {
        type: 'button',
        className: 'btn editor-btn-sm editor-btn-danger',
        text: 'Delete',
        onClick: () => {
          if (groups.length <= 1) return;
          const oldId = vg.id;
          groups.splice(idx, 1);
          if (oldId && region.district_tilts) {
            for (const [dKey, tiltMap] of Object.entries(region.district_tilts)) {
              delete tiltMap[oldId];
              if (Object.keys(tiltMap).length === 0) delete region.district_tilts[dKey];
            }
            if (Object.keys(region.district_tilts).length === 0) delete region.district_tilts;
          }
          this.#renderVotersList();
        }
      });
      delBtn.disabled = deleteDisabled;

      const policyDetails = el('details', { className: 'editor-details' });
      policyDetails.open = idx === 0;
      policyDetails.appendChild(el('summary', { text: 'Ideal Policy Positions & Issue Weights (6 Axes)' }));
      policyDetails.appendChild(el('div', { className: 'editor-section-title', text: 'Ideal Policy Positions [-1 .. +1]' }));
      policyDetails.appendChild(el('div', { className: 'editor-help', text: 'What this group wants on each issue (-1 left/open, +1 right/strict).' }));
      policyDetails.appendChild(this.#buildAxesGrid(vg.ideal, {
        min: -1,
        max: 1,
        step: 0.01,
        decimals: 2,
        prefix: `${vg.name || vg.id || 'Group'} ideal`,
        onUpdate: (ax, val) => {
          const ideal = normalizeVec6(vg.ideal, 0);
          ideal[ax] = val;
          vg.ideal = ideal;
        }
      }));
      policyDetails.appendChild(el('div', { className: 'editor-section-title', text: 'Issue Salience Weights [0 .. 3]' }));
      policyDetails.appendChild(el('div', { className: 'editor-help', text: 'How much each issue matters when picking a party (0 = ignored).' }));
      policyDetails.appendChild(this.#buildAxesGrid(vg.weights, {
        min: 0,
        max: 3,
        step: 0.05,
        decimals: 2,
        prefix: `${vg.name || vg.id || 'Group'} weight`,
        onUpdate: (ax, val) => {
          const weights = normalizeVec6(vg.weights, 1);
          weights[ax] = val;
          vg.weights = weights;
        }
      }));

      this.votersWrapEl.appendChild(el('div', { className: 'editor-card' },
        el('div', { className: 'editor-item-head' },
          makeTextInput(vg.name, 'Voter group name', (v) => { vg.name = v; }, {
            placeholder: 'Voter group name',
            className: 'editor-inline-grow'
          }),
          idInput,
          delBtn
        ),
        el('div', { className: 'editor-grid-2' },
          el('label', { className: 'editor-field' },
            el('span', { text: 'Eligible Population' }),
            makeNumInput(vg.population ?? 0, `${vg.name} population`, {
              min: 0,
              step: '1000',
              onInput: (raw) => {
                const n = parseInt(raw, 10);
                if (!Number.isNaN(n)) vg.population = Math.max(0, n);
              }
            })
          ),
          el('div', { className: 'editor-field' },
            el('span', { text: 'Turnout %' }),
            makeSliderNumRow({
              label: 'Rate %',
              min: 0,
              max: 100,
              step: 0.5,
              decimals: 1,
              value: round4((Number(vg.turnout) || 0) * 100),
              ariaLabel: `${vg.name} turnout %`,
              onChange: (pct) => {
                vg.turnout = clamp(round4(pct / 100), 0, 1);
              }
            })
          )
        ),
        policyDetails
      ));
    });
  }

  #addVoterGroup() {
    const region = (this.draft.regions || []).find((r) => r.id === this.voterRegionId);
    if (!region) return;
    if (!region.groups) region.groups = [];
    if (region.groups.length >= 32) {
      this.#setError(`Region ${region.name} already has the maximum of 32 voter groups.`);
      return;
    }
    this.#clearError();
    const existingIds = new Set(region.groups.map((g) => g.id).filter(Boolean));
    let n = region.groups.length + 1;
    let id = `vg_${n}`;
    while (existingIds.has(id)) {
      n++;
      id = `vg_${n}`;
    }
    region.groups.push({
      id,
      name: `Voter Group ${n}`,
      population: 100000,
      turnout: 0.55,
      ideal: [0, 0, 0, 0, 0, 0],
      weights: [1, 1, 1, 1, 1, 1]
    });
    if (!this.baseTurnoutByRegion[region.id]) this.baseTurnoutByRegion[region.id] = [];
    this.baseTurnoutByRegion[region.id].push(0.55);
    this.#renderVotersList();
  }

  #renderJsonTab() {
    this.fileInputEl = el('input', {
      type: 'file',
      accept: '.json,application/json',
      className: 'hidden',
      onChange: async (e) => {
        const file = e.target.files?.[0];
        if (!file) return;
        try {
          const text = await file.text();
          const parsed = JSON.parse(text);
          if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
            throw new Error('Scenario JSON must be an object');
          }
          this.#loadDraftFrom(parsed);
          this.#updateStatsBar();
          this.#clearError();
          this.#renderJsonTab();
        } catch (err) {
          this.#setError(`Import failed: ${err.message}`);
        } finally {
          e.target.value = '';
        }
      }
    });

    const formatted = JSON.stringify(this.draft, null, 2);
    this.jsonAreaEl = el('textarea', {
      className: 'editor-json-textarea mono',
      spellcheck: 'false',
      'aria-label': 'Scenario JSON source',
      onInput: () => {
        this.jsonDirty = true;
      }
    });
    this.jsonAreaEl.value = formatted;

    const actions = el('div', { className: 'editor-toolbar' },
      el('div', { className: 'editor-json-actions' },
        el('button', {
          type: 'button',
          className: 'btn',
          text: 'Format & Sync',
          onClick: () => {
            this.jsonDirty = true;
            if (this.#syncJsonIfDirty()) {
              this.jsonAreaEl.value = JSON.stringify(this.draft, null, 2);
            }
          }
        }),
        el('button', {
          type: 'button',
          className: 'btn',
          text: 'Download JSON',
          onClick: () => this.#downloadJson()
        }),
        el('button', {
          type: 'button',
          className: 'btn',
          text: 'Import JSON…',
          onClick: () => this.fileInputEl.click()
        })
      ),
      this.fileInputEl
    );

    clearEl(this.bodyEl);
    this.bodyEl.appendChild(actions);
    this.bodyEl.appendChild(this.jsonAreaEl);
  }

  #downloadJson() {
    if (this.jsonDirty && !this.#syncJsonIfDirty()) return;
    const slug = (this.draft?.name || 'scenario')
      .toLowerCase()
      .replace(/[^a-z0-9]+/g, '-')
      .replace(/^-|-$/g, '') || 'scenario';
    const blob = new Blob([JSON.stringify(this.draft, null, 2)], { type: 'application/json' });
    const url = URL.createObjectURL(blob);
    const link = el('a', { href: url, download: `${slug}.json`, className: 'hidden' });
    document.body.appendChild(link);
    link.click();
    link.remove();
    setTimeout(() => URL.revokeObjectURL(url), 1000);
  }

  async #copyShareLink() {
    if (this.isSubmitting || !this.draft || !this.baseScenario) return;
    if (this.activeTab === 'json' && !this.#syncJsonIfDirty()) return;
    this.#clearError();

    const patch = computeScenarioPatch(this.baseScenario, this.draft);
    const payload = {
      v: 1,
      base: this.baseScenarioId || '',
      patch,
      mods: structuredClone(this.modifiers)
    };

    try {
      const encoded = await encodeSharePayload(payload);
      if (encoded.length > 8000) {
        this.#setError(`Edits are too large for a URL (${encoded.length} chars). Use JSON -> Download JSON instead.`);
        return;
      }
      const shareUrl = `${window.location.origin}${window.location.pathname}#share=${encoded}`;
      if (navigator.clipboard?.writeText) {
        await navigator.clipboard.writeText(shareUrl);
      } else {
        const tmp = el('textarea', { className: 'hidden' });
        tmp.value = shareUrl;
        document.body.appendChild(tmp);
        tmp.select();
        document.execCommand('copy');
        tmp.remove();
      }
      const prevText = this.shareBtn.textContent;
      this.shareBtn.textContent = 'Link Copied!';
      setTimeout(() => {
        if (this.shareBtn) this.shareBtn.textContent = prevText;
      }, 1800);
    } catch (err) {
      this.#setError(`Could not build share link: ${err.message}`);
    }
  }

  async #submit(autoStart) {
    if (this.isSubmitting || !this.draft) return;
    if (this.activeTab === 'json' && !this.#syncJsonIfDirty()) return;

    this.#clearError();
    if (!this.draft.name || !this.draft.name.trim()) {
      this.#setError('Scenario name is required.');
      return;
    }

    this.isSubmitting = true;
    this.applyReadyBtn.disabled = true;
    this.applyStartBtn.disabled = true;
    const prevReadyText = this.applyReadyBtn.textContent;
    const prevStartText = this.applyStartBtn.textContent;
    if (autoStart) this.applyStartBtn.textContent = 'Applying…';
    else this.applyReadyBtn.textContent = 'Applying…';

    try {
      const res = await fetch('/api/runs', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(this.draft)
      });
      if (!res.ok) {
        const msg = (await res.text()).trim();
        throw new Error(msg || `HTTP ${res.status}`);
      }
      const data = await res.json();
      if (!data.run_id) throw new Error('Server did not return a run_id');
      this.isSubmitting = false;
      this.close();
      if (this.onApplied) await this.onApplied(data.run_id, autoStart, structuredClone(this.modifiers));
    } catch (err) {
      this.#setError(err.message);
    } finally {
      this.isSubmitting = false;
      this.applyReadyBtn.disabled = false;
      this.applyStartBtn.disabled = false;
      this.applyReadyBtn.textContent = prevReadyText;
      this.applyStartBtn.textContent = prevStartText;
    }
  }
}
