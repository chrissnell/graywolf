// Graywolf Handbook — shared JS

// Theme toggle
(function () {
  var key = 'handbook-theme';
  var toggle = document.querySelector('.theme-toggle');
  if (!toggle) return;

  function apply(theme) {
    document.documentElement.setAttribute('data-theme', theme);
    toggle.textContent = theme === 'dark' ? '[ light ]' : '[ dark ]';
    localStorage.setItem(key, theme);
  }

  var stored = localStorage.getItem(key);
  if (stored) {
    apply(stored);
  } else if (window.matchMedia('(prefers-color-scheme: dark)').matches) {
    apply('dark');
  }

  toggle.addEventListener('click', function () {
    var current = document.documentElement.getAttribute('data-theme');
    apply(current === 'dark' ? 'light' : 'dark');
  });
})();

// Tab switcher
document.querySelectorAll('[data-tabs]').forEach(function (group) {
  var tabs = group.querySelectorAll('.tab');
  var panels = group.querySelectorAll('.tab-panel');

  tabs.forEach(function (tab) {
    tab.addEventListener('click', function () {
      tabs.forEach(function (t) { t.classList.remove('active'); });
      panels.forEach(function (p) { p.classList.remove('active'); });
      tab.classList.add('active');
      var target = group.querySelector('#' + tab.getAttribute('data-target'));
      if (target) target.classList.add('active');
    });
  });
});

// Lightbox
(function () {
  var lb = document.createElement('div');
  lb.className = 'lightbox';
  lb.innerHTML = '<button class="lightbox-close" aria-label="Close">&#x2715;</button><img src="" alt="" />';
  document.body.appendChild(lb);

  var lbImg = lb.querySelector('img');

  function open(src, alt) {
    lbImg.src = src;
    lbImg.alt = alt || '';
    lb.classList.add('open');
    document.body.style.overflow = 'hidden';
  }

  function close() {
    lb.classList.remove('open');
    document.body.style.overflow = '';
    lbImg.src = '';
  }

  document.querySelectorAll('.screenshot img').forEach(function (img) {
    img.addEventListener('click', function () {
      open(img.src, img.alt);
    });
  });

  lb.addEventListener('click', function (e) {
    if (e.target === lb || e.target.classList.contains('lightbox-close')) {
      close();
    }
  });

  document.addEventListener('keydown', function (e) {
    if (e.key === 'Escape' && lb.classList.contains('open')) {
      close();
    }
  });
})();

// Mark active sidebar link
(function () {
  var current = location.pathname.split('/').pop() || 'index.html';
  document.querySelectorAll('.sidebar a').forEach(function (a) {
    var href = a.getAttribute('href');
    if (href === current || (current === 'index.html' && href === 'index.html')) {
      a.classList.add('active');
    }
  });
})();
