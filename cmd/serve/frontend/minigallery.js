/*!
  MiniGallery v1.0.0 — single-file, no-deps image grid + lightbox
  Usage:
    const gallery = MiniGallery.mount('#app', [
      'img1.jpg',
      { src: 'img2_large.jpg', thumb: 'img2_thumb.jpg', alt: 'Alt', caption: 'Caption' }
    ], { gap: 8, minThumb: 140 });
*/
(function (global) {
  const STYLE_ID = 'mg-styles';
  function injectStyles() {
    if (document.getElementById(STYLE_ID)) return;
    const css = `
.mg-grid{display:grid;grid-template-columns:repeat(auto-fill,minmax(var(--mg-min,140px),1fr));gap:var(--mg-gap,8px)}
.mg-grid button{all:unset;display:block;cursor:pointer}
.mg-thumb{width:100%;height:100%;aspect-ratio:1/1;object-fit:cover;border-radius:6px;background:#eee;transition:transform .15s ease}
.mg-thumb:hover{transform:translateY(-1px)}
.mg-lightbox{position:fixed;inset:0;background:rgba(0,0,0,.92);display:none;align-items:center;justify-content:center;z-index:9999}
.mg-lightbox.open{display:flex}
.mg-figure{position:relative;max-width:92vw;max-height:92vh;display:flex;flex-direction:column;gap:10px;align-items:center}
.mg-image{max-width:92vw;max-height:78vh;object-fit:contain;border-radius:6px;background:#111}
.mg-caption{color:#ddd;font:14px/1.4 system-ui,Segoe UI,Roboto,Helvetica,Arial,sans-serif;text-align:center;max-width:90vw}
.mg-btn{position:absolute;background:rgba(255,255,255,.12);color:#fff;border:none;border-radius:999px;backdrop-filter:saturate(120%) blur(2px);width:42px;height:42px;display:grid;place-items:center;cursor:pointer}
.mg-btn:hover{background:rgba(255,255,255,.2)}
.mg-close{top:16px;right:16px}
.mg-prev,.mg-next{top:50%;transform:translateY(-50%)}
.mg-prev{left:16px}
.mg-next{right:16px}
.mg-visually-hidden{position:absolute!important;height:1px;width:1px;overflow:hidden;clip:rect(1px,1px,1px,1px);white-space:nowrap}
    `.trim();
    const style = document.createElement('style');
    style.id = STYLE_ID;
    style.textContent = css;
    document.head.appendChild(style);
  }

  function normalizeItems(items) {
    return (items || []).map(i => {
      if (typeof i === 'string') return { src: i, thumb: i, alt: '', caption: '' };
      return {
        src: i.src,
        thumb: i.thumb || i.src,
        alt: i.alt || '',
        caption: i.caption || ''
      };
    }).filter(i => !!i.src);
  }

  function createLightbox() {
    const overlay = document.createElement('div');
    overlay.className = 'mg-lightbox';
    overlay.setAttribute('role', 'dialog');
    overlay.setAttribute('aria-modal', 'true');
    overlay.innerHTML = `
      <div class="mg-figure">
        <img class="mg-image" alt="">
        <div class="mg-caption"></div>
        <button class="mg-btn mg-close" aria-label="Close (Esc)">&times;</button>
        <button class="mg-btn mg-prev" aria-label="Previous">&#10094;</button>
        <button class="mg-btn mg-next" aria-label="Next">&#10095;</button>
        <span class="mg-visually-hidden" aria-live="polite"></span>
      </div>
    `;
    document.body.appendChild(overlay);
    return {
      el: overlay,
      img: overlay.querySelector('.mg-image'),
      cap: overlay.querySelector('.mg-caption'),
      closeBtn: overlay.querySelector('.mg-close'),
      prevBtn: overlay.querySelector('.mg-prev'),
      nextBtn: overlay.querySelector('.mg-next'),
      live: overlay.querySelector('[aria-live]'),
    };
  }

  function MiniGallery(container, items, opts) {
    injectStyles();
    const options = Object.assign({ gap: 8, minThumb: 140 }, opts || {});
    const root = typeof container === 'string' ? document.querySelector(container) : container;
    if (!root) throw new Error('MiniGallery: container not found');
    const data = normalizeItems(items);
    root.style.setProperty('--mg-gap', options.gap + 'px');
    root.style.setProperty('--mg-min', options.minThumb + 'px');

    const grid = document.createElement('div');
    grid.className = 'mg-grid';
    root.innerHTML = '';
    root.appendChild(grid);

    // Build thumbnails
    data.forEach((item, idx) => {
      const btn = document.createElement('button');
      const img = document.createElement('img');
      img.className = 'mg-thumb';
      img.loading = 'lazy';
      img.src = item.thumb;
      img.alt = item.alt || '';
      btn.appendChild(img);
      btn.addEventListener('click', () => open(idx));
      grid.appendChild(btn);
    });

    // Lightbox
    const lb = createLightbox();
    let index = 0;
    let lastFocus = null;
    let openState = false;

    function render(i) {
      const it = data[i];
      lb.img.src = it.src;
      lb.img.alt = it.alt || '';
      lb.cap.textContent = it.caption || '';
      lb.live.textContent = (it.caption || it.alt || it.src).toString();
    }

    function open(i = 0) {
      if (!data.length) return;
      index = ((i % data.length) + data.length) % data.length;
      render(index);
      lb.el.classList.add('open');
      document.body.style.overflow = 'hidden';
      lastFocus = document.activeElement;
      lb.closeBtn.focus();
      openState = true;
      window.addEventListener('keydown', onKey, { passive: true });
      lb.el.addEventListener('click', onBackdrop);
    }

    function close() {
      lb.el.classList.remove('open');
      document.body.style.overflow = '';
      openState = false;
      window.removeEventListener('keydown', onKey);
      lb.el.removeEventListener('click', onBackdrop);
      if (lastFocus && lastFocus.focus) lastFocus.focus();
    }

    function prev() { go(-1); }
    function next() { go(1); }
    function go(delta) {
      if (!openState) return;
      index = (index + delta + data.length) % data.length;
      render(index);
    }

    function onKey(e) {
      if (!openState) return;
      switch (e.key) {
        case 'Escape': close(); break;
        case 'ArrowLeft': prev(); break;
        case 'ArrowRight': next(); break;
      }
    }
    function onBackdrop(e) {
      // Close if clicking black backdrop area
      if (e.target === lb.el) close();
    }

    lb.closeBtn.addEventListener('click', close);
    lb.prevBtn.addEventListener('click', prev);
    lb.nextBtn.addEventListener('click', next);

    return {
      open,
      close,
      next,
      prev,
      destroy() {
        close();
        root.innerHTML = '';
        lb.el.remove();
      }
    };
  }

  MiniGallery.mount = MiniGallery;
  // UMD-lite
  if (typeof module !== 'undefined' && module.exports) module.exports = MiniGallery;
  else global.MiniGallery = MiniGallery;
})(this);
