// Keep API and user-provided values out of HTML parsing sinks.
window.maildDOM = {
  el(tag, text, className) {
    const node = document.createElement(tag);
    if (text !== undefined && text !== null) node.textContent = String(text);
    if (className) node.className = className;
    return node;
  },
  localLink(href, text, className) {
    try {
      const url = new URL(href, window.location.href);
      if (url.origin === window.location.origin && /^https?:$/.test(url.protocol)) {
        const link = this.el('a', text, className);
        link.href = url.href;
        return link;
      }
    } catch (_) { /* Invalid actions render as text. */ }
    return this.el('span', text, className);
  }
};
