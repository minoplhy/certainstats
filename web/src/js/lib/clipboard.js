// copyText puts text on the clipboard and says so in a toast. Pages in any
// bundle may import it.
export async function copyText(text, label) {
  try {
    await navigator.clipboard.writeText(text);
    window.CertainStatsTelemetry?.showToast((label || 'Value') + ' copied', true);
    return true;
  } catch (e) {
    window.CertainStatsTelemetry?.showToast('Copy failed; select the text and copy it by hand', false);
    return false;
  }
}

// copyIcon is a small labelled copy button, styled by .cs-icon-btn.
export function copyIcon(doc, label, onClick) {
  const node = doc.createElement('button');
  node.type = 'button';
  node.className = 'cs-icon-btn';
  node.setAttribute('aria-label', label);
  node.title = label;
  const svg = doc.createElementNS('http://www.w3.org/2000/svg', 'svg');
  svg.setAttribute('viewBox', '0 0 24 24');
  svg.setAttribute('aria-hidden', 'true');
  const path = doc.createElementNS('http://www.w3.org/2000/svg', 'path');
  path.setAttribute('d', 'M9 9h10v10H9zM5 15V5h10');
  path.setAttribute('fill', 'none');
  path.setAttribute('stroke', 'currentColor');
  path.setAttribute('stroke-width', '2');
  path.setAttribute('stroke-linejoin', 'round');
  svg.append(path);
  node.append(svg);
  node.addEventListener('click', onClick);
  return node;
}
