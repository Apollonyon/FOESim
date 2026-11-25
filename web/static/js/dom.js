// Safe DOM creation and CSS color sanitization helpers (prevents raw innerHTML injection).

const COLOR_RE = /^#(?:[0-9a-fA-F]{3}|[0-9a-fA-F]{6})$|^rgb\(\s*\d{1,3}\s*,\s*\d{1,3}\s*,\s*\d{1,3}\s*\)$/;

export function safeColor(color, fallback = '#64748b') {
  if (typeof color === 'string' && COLOR_RE.test(color.trim())) {
    return color.trim();
  }
  return fallback;
}

export function clearEl(node) {
  if (node) node.replaceChildren();
}

export function el(tag, props = null, ...children) {
  const node = document.createElement(tag);
  if (props) {
    for (const [k, v] of Object.entries(props)) {
      if (v === null || v === undefined) continue;
      if (k === 'className') node.className = v;
      else if (k === 'text') node.textContent = String(v);
      else if (k === 'style' && typeof v === 'object') Object.assign(node.style, v);
      else if (k.startsWith('on') && typeof v === 'function') node.addEventListener(k.slice(2).toLowerCase(), v);
      else node.setAttribute(k, String(v));
    }
  }
  for (const child of children) {
    if (child === null || child === undefined || child === false) continue;
    node.appendChild(typeof child === 'object' ? child : document.createTextNode(String(child)));
  }
  return node;
}

export function swatch(color) {
  return el('span', { className: 'swatch', style: { background: safeColor(color) } });
}
