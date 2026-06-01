document.addEventListener('DOMContentLoaded', () => {
  const themeEl = document.getElementById('theme');
  const triggerEl = document.getElementById('trigger');
  const serverEl = document.getElementById('server');
  const hideDelayEl = document.getElementById('hideDelay');
  const ttsLangEl = document.getElementById('ttsLang');
  const studyModeEl = document.getElementById('studyMode');
  const autoCopyEl = document.getElementById('autoCopy');
  const saveBtn = document.getElementById('saveBtn');
  const statusEl = document.getElementById('status');

  function applyPopupTheme(theme) {
    const html = document.documentElement;
    if (theme === 'dark') {
      html.setAttribute('data-theme', 'dark');
    } else if (theme === 'light') {
      html.setAttribute('data-theme', 'light');
    } else {
      const prefersDark = window.matchMedia('(prefers-color-scheme: dark)').matches;
      html.setAttribute('data-theme', prefersDark ? 'dark' : 'light');
    }
  }

  function loadSettings() {
    chrome.storage.sync.get(
      ['theme', 'trigger', 'server', 'hideDelay', 'ttsLang', 'ttsRate', 'studyMode', 'autoCopy'],
      (items) => {
        themeEl.value = items.theme || 'system';
        triggerEl.value = items.trigger || 'click';
        serverEl.value = items.server || 'http://localhost:8787';
        hideDelayEl.value = String(items.hideDelay || 800);
        ttsLangEl.value = items.ttsLang || 'en-US';
        studyModeEl.checked = items.studyMode !== false;
        autoCopyEl.checked = items.autoCopy === true;
        applyPopupTheme(themeEl.value);
      }
    );
  }

  loadSettings();

  function showStatus(msg, type) {
    statusEl.textContent = msg;
    statusEl.className = 'popup-status ' + (type || '');
    setTimeout(() => {
      statusEl.textContent = '';
      statusEl.className = 'popup-status';
    }, 2000);
  }

  function saveSettings() {
    const settings = {
      theme: themeEl.value,
      trigger: triggerEl.value,
      server: serverEl.value.trim(),
      hideDelay: parseInt(hideDelayEl.value, 10),
      ttsLang: ttsLangEl.value,
      studyMode: studyModeEl.checked,
      autoCopy: autoCopyEl.checked,
    };
    applyPopupTheme(themeEl.value);
    chrome.storage.sync.set(settings, () => {
      showStatus('设置已保存 ✅', 'success');
    });
  }

  themeEl.addEventListener('change', saveSettings);
  triggerEl.addEventListener('change', saveSettings);
  hideDelayEl.addEventListener('change', saveSettings);
  ttsLangEl.addEventListener('change', saveSettings);
  studyModeEl.addEventListener('change', saveSettings);
  autoCopyEl.addEventListener('change', saveSettings);

  saveBtn.addEventListener('click', saveSettings);
});
