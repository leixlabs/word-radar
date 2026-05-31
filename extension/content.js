(function () {
  'use strict';

  // ====== 配置 ======
  const DEFAULT_SERVER = 'http://localhost:8787';
  let settings = {
    trigger: 'click',
    server: DEFAULT_SERVER,
    autoSave: true,
    ttsLang: 'en-US',
    ttsRate: 1.0,
    hideDelay: 800,
    studyMode: true,
    theme: 'system',
  };

  let systemDarkQuery = null;
  let isPinned = false;

  function resolveTheme() {
    const theme = settings.theme || 'system';
    if (theme === 'dark') return 'dark';
    if (theme === 'light') return 'light';
    return window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light';
  }

  function applyPopupTheme() {
    if (popupEl) {
      popupEl.setAttribute('data-theme', resolveTheme());
    }
  }

  function setupSystemThemeListener() {
    if (systemDarkQuery) {
      systemDarkQuery.removeEventListener('change', applyPopupTheme);
      systemDarkQuery = null;
    }
    if (settings.theme === 'system') {
      systemDarkQuery = window.matchMedia('(prefers-color-scheme: dark)');
      systemDarkQuery.addEventListener('change', applyPopupTheme);
    }
  }

  let popupSize = { width: 420, height: 320 };

  // 加载设置
  chrome.storage.sync.get(
    ['trigger', 'server', 'autoSave', 'ttsLang', 'ttsRate', 'hideDelay', 'studyMode', 'theme', 'popupWidth', 'popupHeight'],
    (items) => {
      settings = { ...settings, ...items };
      if (items.popupWidth) popupSize.width = items.popupWidth;
      if (items.popupHeight) popupSize.height = items.popupHeight;
      setupSystemThemeListener();
    }
  );

  // 监听设置变化
  chrome.storage.onChanged.addListener((changes) => {
    for (const key in changes) {
      settings[key] = changes[key].newValue;
    }
    if (changes.theme) {
      setupSystemThemeListener();
      applyPopupTheme();
    }
  });

  // ====== 状态 ======
  let popupEl = null;
  let isHoveringPopup = false;
  let hideTimer = null;
  let dragStart = { x: 0, y: 0, left: 0, top: 0 };
  let isDragging = false;
  let isResizing = false;
  let resizeStart = { x: 0, y: 0, width: 0, height: 0 };
  let ignoreNextOutsideClick = false;
  let activeWordRange = null;   // 当前高亮的 Range
  let activeWordNode = null;    // 用于高亮包裹的临时元素
  let activeAbortController = null; // 用于取消正在进行的请求

  // ====== 取词逻辑 ======
  function getWordAtPoint(event) {
    const selection = window.getSelection();
    selection.removeAllRanges();

    let range;
    if (document.caretPositionFromPoint) {
      const pos = document.caretPositionFromPoint(event.clientX, event.clientY);
      if (!pos) return null;
      range = document.createRange();
      range.setStart(pos.offsetNode, pos.offset);
      range.setEnd(pos.offsetNode, pos.offset);
    } else if (document.caretRangeFromPoint) {
      range = document.caretRangeFromPoint(event.clientX, event.clientY);
    } else {
      return null;
    }

    // 扩展选区到整词
    const node = range.startContainer;
    if (node.nodeType !== Node.TEXT_NODE) return null;

    const text = node.textContent;
    let start = range.startOffset;
    let end = range.startOffset;

    // 快速排除：点击空白处时浏览器可能把光标"吸附"到附近文本。
    // 检查 offset 位置或紧邻位置是否有字母字符。
    const isLetter = (idx) => idx >= 0 && idx < text.length && /[a-zA-Z'-]/.test(text[idx]);
    if (!isLetter(start) && !isLetter(start - 1)) {
      selection.removeAllRanges();
      return null;
    }

    // 向左扩展
    while (start > 0 && /[a-zA-Z'-]/.test(text[start - 1])) start--;
    // 向右扩展
    while (end < text.length && /[a-zA-Z'-]/.test(text[end])) end++;

    if (start === end) return null;

    const word = text.slice(start, end);
    if (!/^[a-zA-Z]+$/.test(word) || word.length < 2) return null;

    // 重新创建精确覆盖该单词的 range
    const wordRange = document.createRange();
    wordRange.setStart(node, start);
    wordRange.setEnd(node, end);

    // 关键检查：验证点击坐标是否实际落在取到的单词区域内
    // 防止点击空白处时光标被浏览器吸附到附近文本而误取词
    const rect = wordRange.getBoundingClientRect();
    const margin = 4;
    const hitX = event.clientX >= rect.left - margin && event.clientX <= rect.right + margin;
    const hitY = event.clientY >= rect.top - margin && event.clientY <= rect.bottom + margin;
    if (!hitX || !hitY) {
      selection.removeAllRanges();
      return null;
    }

    // 获取所在句子上下文
    const sentence = getSentence(text, start, end);

    selection.removeAllRanges();
    return { word, sentence, range: wordRange };
  }

  function getSentence(fullText, wordStart, wordEnd) {
    let sentStart = wordStart;
    let sentEnd = wordEnd;

    const terminators = /[.!?。！？]/;
    while (sentStart > 0 && !terminators.test(fullText[sentStart - 1])) sentStart--;
    while (sentEnd < fullText.length && !terminators.test(fullText[sentEnd])) sentEnd++;

    let sentence = fullText.slice(sentStart, sentEnd + 1).trim();
    sentence = sentence.replace(/\s+/g, ' ');
    return sentence;
  }

  function getSelectedWord() {
    const selection = window.getSelection();
    const text = selection.toString().trim();
    if (!text) return null;

    const word = text.split(/[^a-zA-Z'-]/)[0];
    if (!word || word.length < 2) return null;

    const range = selection.getRangeAt(0);
    const node = range.startContainer;
    const sentence = node.nodeType === TEXT_NODE ? getSentence(node.textContent, range.startOffset, range.endOffset) : '';

    return { word, sentence, range };
  }

  // ====== 高亮 ======
  function highlightRange(range) {
    clearHighlight();
    if (!range) return;

    try {
      // 用 span 包裹实现自定义高亮样式
      const span = document.createElement('span');
      span.className = 'word-radar-highlight';
      if (resolveTheme() === 'dark') {
        span.classList.add('word-radar-highlight-dark');
      }
      activeWordNode = span;
      activeWordRange = range.cloneRange();
      range.surroundContents(span);
    } catch (e) {
      // 如果 range 跨节点，fallback 到原生 selection
      const sel = window.getSelection();
      sel.removeAllRanges();
      sel.addRange(range);
      activeWordRange = range;
    }
  }

  function clearHighlight() {
    // 移除 span 包裹的高亮
    if (activeWordNode && activeWordNode.parentNode) {
      const parent = activeWordNode.parentNode;
      while (activeWordNode.firstChild) {
        parent.insertBefore(activeWordNode.firstChild, activeWordNode);
      }
      parent.removeChild(activeWordNode);
      activeWordNode = null;
    }
    activeWordRange = null;

    // 同时清除原生 selection
    const sel = window.getSelection();
    sel.removeAllRanges();
  }

  // ====== 弹窗逻辑 ======
  function createPopup() {
    if (popupEl) return;

    const el = document.createElement('div');
    el.id = 'word-radar-popup';
    el.className = 'word-radar-popup';
    el.innerHTML = `
      <div class="word-radar-header">
        <span class="word-radar-word"></span>
        <span class="word-radar-phonetic"></span>
        <button class="word-radar-btn word-radar-speak" title="发音">🔊</button>
        <a class="word-radar-youglish" target="_blank" rel="noopener" title="在 YouGlish 中看视频发音">▶️ YouGlish</a>
        <button class="word-radar-btn word-radar-pin" title="固定">📌</button>
        <button class="word-radar-btn word-radar-close" title="关闭">✕</button>
      </div>
      <div class="word-radar-body">
        <div class="word-radar-meanings"></div>
        <div class="word-radar-examples"></div>
        <div class="word-radar-etymology"></div>
      </div>

      <!-- 底部进度条（LLM 单词卡加载时显示） -->
      <div class="word-radar-etymology-progress">
        <div class="word-radar-etymology-bar"></div>
        <span class="word-radar-etymology-progress-text">加载单词卡...</span>
      </div>

      <div class="word-radar-resize" title="拖动调整大小">⋮⋮</div>
    `;

    // 悬浮不关闭
    el.addEventListener('mouseenter', () => {
      isHoveringPopup = true;
      clearTimeout(hideTimer);
    });
    el.addEventListener('mouseleave', () => {
      isHoveringPopup = false;
      scheduleHide();
    });

    // Pin 按钮
    const pinBtn = el.querySelector('.word-radar-pin');
    pinBtn.addEventListener('click', () => {
      isPinned = !isPinned;
      pinBtn.classList.toggle('active', isPinned);
      pinBtn.title = isPinned ? '取消固定' : '固定';
      if (isPinned) {
        clearTimeout(hideTimer);
      }
    });

    // 关闭按钮
    el.querySelector('.word-radar-close').addEventListener('click', () => {
      hidePopup();
    });

    // 发音
    el.querySelector('.word-radar-speak').addEventListener('click', () => {
      const word = el.dataset.word;
      if (word) speak(word);
    });

    // 拖拽
    const header = el.querySelector('.word-radar-header');
    header.addEventListener('mousedown', (e) => {
      if (e.target.closest('.word-radar-btn, .word-radar-youglish')) return;
      isDragging = true;
      dragStart.x = e.pageX;
      dragStart.y = e.pageY;
      dragStart.left = parseFloat(el.style.left) || 0;
      dragStart.top = parseFloat(el.style.top) || 0;
      header.style.cursor = 'grabbing';
      e.preventDefault();
    });

    document.addEventListener('mousemove', (e) => {
      if (!isDragging) return;
      el.style.left = `${dragStart.left + e.pageX - dragStart.x}px`;
      el.style.top = `${dragStart.top + e.pageY - dragStart.y}px`;
    });

    // Resize
    const resizeHandle = el.querySelector('.word-radar-resize');
    resizeHandle.addEventListener('mousedown', (e) => {
      isResizing = true;
      resizeStart.x = e.pageX;
      resizeStart.y = e.pageY;
      resizeStart.width = el.offsetWidth;
      resizeStart.height = el.offsetHeight;
      e.preventDefault();
      e.stopPropagation();
    });

    document.addEventListener('mouseup', () => {
      if (isDragging) {
        isDragging = false;
        header.style.cursor = 'move';
      }
      if (isResizing) {
        isResizing = false;
        // 持久化尺寸
        popupSize.width = el.offsetWidth;
        popupSize.height = el.offsetHeight;
        chrome.storage.sync.set({
          popupWidth: popupSize.width,
          popupHeight: popupSize.height,
        });
      }
    });

    document.addEventListener('mousemove', (e) => {
      if (isResizing) {
        const newW = resizeStart.width + e.pageX - resizeStart.x;
        const newH = resizeStart.height + e.pageY - resizeStart.y;
        if (newW > 280) el.style.width = `${newW}px`;
        if (newH > 180) el.style.height = `${newH}px`;
      }
    });

    document.body.appendChild(el);
    popupEl = el;
  }

  function showPopup(wordRange, data) {
    createPopup();
    clearTimeout(hideTimer);

    // 应用保存的尺寸
    popupEl.style.width = `${popupSize.width}px`;
    popupEl.style.height = `${popupSize.height}px`;

    popupEl.dataset.word = data.word;
    popupEl.querySelector('.word-radar-word').textContent = data.word;
    popupEl.querySelector('.word-radar-phonetic').textContent = data.phonetic || '';

    // YouGlish 链接
    const youglishEl = popupEl.querySelector('.word-radar-youglish');
    if (youglishEl) {
      if (data.youglish_url) {
        youglishEl.href = data.youglish_url;
        youglishEl.style.display = 'inline';
      } else {
        youglishEl.style.display = 'none';
      }
    }

    // 释义
    const meaningsEl = popupEl.querySelector('.word-radar-meanings');
    meaningsEl.innerHTML = '';
    if (data.meanings && data.meanings.length > 0) {
      data.meanings.forEach((m) => {
        const pos = m.partOfSpeech ? `<b>${m.partOfSpeech}</b>. ` : '';
        const defs = m.definitions.slice(0, 3).join('; ');
        const div = document.createElement('div');
        div.className = 'word-radar-meaning';
        div.innerHTML = pos + defs;
        meaningsEl.appendChild(div);
      });
    } else {
      meaningsEl.innerHTML = '<div class="word-radar-meaning">暂无释义</div>';
    }

    // 例句
    const examplesEl = popupEl.querySelector('.word-radar-examples');
    examplesEl.innerHTML = '';
    if (data.examples && data.examples.length > 0) {
      examplesEl.innerHTML = '<div class="word-radar-section-title">📖 例句</div>';
      data.examples.forEach((ex) => {
        const div = document.createElement('div');
        div.className = 'word-radar-example';
        div.textContent = `"${ex}"`;
        examplesEl.appendChild(div);
      });
    }

    // 记忆增强卡片区域（单词卡 LLM 数据异步注入）
    const etyEl = popupEl.querySelector('.word-radar-etymology');
    etyEl.innerHTML = '';

    // ====== 位置：基于单词 rect 定位 ======
    const rect = wordRange.getBoundingClientRect();
    const popupWidth = 360;
    const popupHeight = 200; // 估算，渲染后会更精确

    // 默认显示在单词下方
    let left = rect.left + window.scrollX;
    let top = rect.bottom + window.scrollY + 8;

    // 水平居中于单词
    left = left + rect.width / 2 - popupWidth / 2;

    // 智能避让
    if (left < 10) left = 10;
    if (left + popupWidth > window.innerWidth + window.scrollX - 10) {
      left = window.innerWidth + window.scrollX - popupWidth - 10;
    }
    if (top + popupHeight > window.innerHeight + window.scrollY - 10) {
      // 单词上方
      top = rect.top + window.scrollY - popupHeight - 8;
    }
    if (top < 0) top = rect.bottom + window.scrollY + 8;

    popupEl.style.left = `${left}px`;
    popupEl.style.top = `${top}px`;
    popupEl.style.display = 'flex';
    applyPopupTheme();

    // 新单词默认不 pin
    isPinned = false;
    const pinBtn = popupEl.querySelector('.word-radar-pin');
    if (pinBtn) {
      pinBtn.classList.remove('active');
      pinBtn.title = '固定';
    }

    ignoreNextOutsideClick = true;
    setTimeout(() => { ignoreNextOutsideClick = false; }, 100);


  }

  function hidePopup() {
    if (popupEl) {
      popupEl.style.display = 'none';
    }
    clearHighlight();
    resetEtymologyProgress();
  }

  // 渲染单词卡（LLM 记忆增强数据）到指定容器
  function renderWordCard(container, card) {
    container.innerHTML = '<div class="word-radar-section-title">🧠 记忆增强</div>';
    if (card.scene) {
      const div = document.createElement('div');
      div.className = 'word-radar-ety-item';
      div.innerHTML = `<span class="word-radar-ety-label">🎨 场景</span><span class="word-radar-ety-value">${card.scene}</span>`;
      container.appendChild(div);
    }
    if (card.etymology) {
      const div = document.createElement('div');
      div.className = 'word-radar-ety-item';
      div.innerHTML = `<span class="word-radar-ety-label">🧩 词根</span><span class="word-radar-ety-value">${card.etymology}</span>`;
      container.appendChild(div);
    }
    if (card.cn_core) {
      const div = document.createElement('div');
      div.className = 'word-radar-ety-item';
      div.innerHTML = `<span class="word-radar-ety-label">💡 核心</span><span class="word-radar-ety-value">${card.cn_core}</span>`;
      container.appendChild(div);
    }
    if (card.example) {
      const div = document.createElement('div');
      div.className = 'word-radar-ety-item';
      div.innerHTML = `<span class="word-radar-ety-label">📖 例句</span><span class="word-radar-ety-value">${card.example}</span>`;
      container.appendChild(div);
    }
    if (card.contrast) {
      const div = document.createElement('div');
      div.className = 'word-radar-ety-item';
      div.innerHTML = `<span class="word-radar-ety-label">⚡ 对比</span><span class="word-radar-ety-value">${card.contrast}</span>`;
      container.appendChild(div);
    }
    if (card.word_family && card.word_family.length > 0) {
      const div = document.createElement('div');
      div.className = 'word-radar-ety-item';
      div.innerHTML = `<span class="word-radar-ety-label">👨‍👩‍👧‍👦 同根</span><span class="word-radar-ety-value">${card.word_family.join(', ')}</span>`;
      container.appendChild(div);
    }
    if (card.pronunciation_trap) {
      const div = document.createElement('div');
      div.className = 'word-radar-ety-item';
      div.innerHTML = `<span class="word-radar-ety-label">🎯 发音</span><span class="word-radar-ety-value">${card.pronunciation_trap}</span>`;
      container.appendChild(div);
    }
    if (card.memory_hook) {
      const div = document.createElement('div');
      div.className = 'word-radar-ety-item';
      div.innerHTML = `<span class="word-radar-ety-label">🔗 记忆</span><span class="word-radar-ety-value">${card.memory_hook}</span>`;
      container.appendChild(div);
    }
    if (card.register) {
      const div = document.createElement('div');
      div.className = 'word-radar-ety-item';
      div.innerHTML = `<span class="word-radar-ety-label">🏷️ 语域</span><span class="word-radar-ety-value">${card.register}</span>`;
      container.appendChild(div);
    }
  }

  // ====== 底部进度条（单词卡 LLM 加载状态） ======
  function showEtymologyProgress(text) {
    if (!popupEl) return;
    const el = popupEl.querySelector('.word-radar-etymology-progress');
    const bar = popupEl.querySelector('.word-radar-etymology-bar');
    const txt = popupEl.querySelector('.word-radar-etymology-progress-text');
    if (!el) return;
    el.style.display = 'flex';
    el.className = 'word-radar-etymology-progress loading';
    bar.className = 'word-radar-etymology-bar';
    if (text) txt.textContent = text;
  }

  function setEtymologyProgressSuccess(text) {
    const el = popupEl && popupEl.querySelector('.word-radar-etymology-progress');
    const bar = popupEl && popupEl.querySelector('.word-radar-etymology-bar');
    const txt = popupEl && popupEl.querySelector('.word-radar-etymology-progress-text');
    if (!el) return;
    el.className = 'word-radar-etymology-progress success';
    bar.className = 'word-radar-etymology-bar success';
    if (text) txt.textContent = text;
    // 成功后渐隐
    clearTimeout(el._hideTimer);
    el._hideTimer = setTimeout(() => {
      el.style.display = 'none';
      el.className = 'word-radar-etymology-progress';
      bar.className = 'word-radar-etymology-bar';
    }, 1500);
  }

  function setEtymologyProgressError(text) {
    const el = popupEl && popupEl.querySelector('.word-radar-etymology-progress');
    const bar = popupEl && popupEl.querySelector('.word-radar-etymology-bar');
    const txt = popupEl && popupEl.querySelector('.word-radar-etymology-progress-text');
    if (!el) return;
    el.className = 'word-radar-etymology-progress error';
    bar.className = 'word-radar-etymology-bar error';
    if (text) txt.textContent = text;
    // 错误保持更久
    clearTimeout(el._hideTimer);
    el._hideTimer = setTimeout(() => {
      el.style.display = 'none';
      el.className = 'word-radar-etymology-progress';
      bar.className = 'word-radar-etymology-bar';
    }, 4000);
  }

  function resetEtymologyProgress() {
    const el = popupEl && popupEl.querySelector('.word-radar-etymology-progress');
    const bar = popupEl && popupEl.querySelector('.word-radar-etymology-bar');
    const txt = popupEl && popupEl.querySelector('.word-radar-etymology-progress-text');
    if (!el) return;
    clearTimeout(el._hideTimer);
    el.style.display = 'none';
    el.className = 'word-radar-etymology-progress';
    bar.className = 'word-radar-etymology-bar';
    if (txt) txt.textContent = '加载单词卡...';
  }

  function scheduleHide() {
    if (isPinned) return;
    clearTimeout(hideTimer);
    const delay = parseInt(settings.hideDelay, 10) || 800;
    hideTimer = setTimeout(() => {
      if (!isHoveringPopup) hidePopup();
    }, delay);
  }

  // ====== TTS ======
  function speak(word) {
    if (!window.speechSynthesis) return;
    const utter = new SpeechSynthesisUtterance(word);
    utter.lang = settings.ttsLang || 'en-US';
    utter.rate = settings.ttsRate || 1.0;
    window.speechSynthesis.cancel();
    window.speechSynthesis.speak(utter);
  }

  // ====== 后端通信 ======
  // 查词（字典信息），快速响应
  // 如果 autoSave 关闭，则不传 context，后端不会保存
  async function lookupWord(word, context, signal) {
    try {
      const params = new URLSearchParams({ q: word });
      if (settings.autoSave && context) {
        params.set('context', context);
      }
      const resp = await fetch(`${settings.server}/api/lookup?${params}`, { signal });
      if (!resp.ok) throw new Error('lookup failed');
      return await resp.json();
    } catch (e) {
      if (e.name === 'AbortError') throw e;
      console.error('Word Radar lookup error:', e);
      return null;
    }
  }

  // 单词卡（LLM 记忆增强），独立接口，异步加载
  async function fetchWordCard(word, signal) {
    try {
      const params = new URLSearchParams({ q: word });
      const resp = await fetch(`${settings.server}/api/wordcard?${params}`, { signal });
      if (!resp.ok) return null;
      return await resp.json();
    } catch (e) {
      if (e.name === 'AbortError') throw e;
      console.error('Word Radar wordcard error:', e);
      return null;
    }
  }

  // 手动保存（无视 autoSave 设置）
  async function saveWord(word, context) {
    try {
      const params = new URLSearchParams({
        q: word,
        context: context || '',
      });
      const resp = await fetch(`${settings.server}/api/lookup?${params}`);
      if (!resp.ok) throw new Error('save failed');
      return await resp.json();
    } catch (e) {
      console.error('Word Radar save error:', e);
    }
  }

  // ====== 防抖 ======
  function debounce(fn, wait) {
    let timer = null;
    return function (...args) {
      clearTimeout(timer);
      timer = setTimeout(() => fn.apply(this, args), wait);
    };
  }

  // 统一处理查词：先快速渲染字典信息，再异步加载单词卡
  async function processWordResult(result) {
    // 取消之前的请求
    if (activeAbortController) {
      activeAbortController.abort();
    }
    activeAbortController = new AbortController();
    const signal = activeAbortController.signal;

    // 1. 查字典，快速渲染弹窗
    const data = await lookupWord(result.word, result.sentence, signal);
    if (data) {
      data.context = result.sentence;
      speak(data.word);
      showPopup(result.range, data);
    } else {
      speak(result.word);
      showPopup(result.range, {
        word: result.word,
        context: result.sentence,
        meanings: [],
        examples: [],
      });
    }

    // 2. 异步加载单词卡（LLM 可能较慢，不阻塞弹窗）
    //    底部进度条提供加载反馈
    resetEtymologyProgress();
    showEtymologyProgress('正在加载单词卡...');
    try {
      const cardData = await fetchWordCard(result.word, signal);
      if (signal.aborted) return;
      if (cardData && popupEl && popupEl.dataset.word.toLowerCase() === result.word.toLowerCase()) {
        renderWordCard(popupEl.querySelector('.word-radar-etymology'), cardData);
        setEtymologyProgressSuccess('单词卡加载完成 ✓');
      } else if (!cardData && popupEl && popupEl.dataset.word.toLowerCase() === result.word.toLowerCase()) {
        setEtymologyProgressSuccess('暂无数据');
      } else if (!signal.aborted) {
        setEtymologyProgressError('单词卡加载失败 ❌');
      }
    } catch (e) {
      if (e.name !== 'AbortError') {
        console.error('Word Radar wordcard error:', e);
        setEtymologyProgressError('单词卡解析出错 ❌');
      }
    }
  }

  const debouncedProcessWord = debounce(processWordResult, 120);

  // ====== 事件监听 ======
  // 单击取词
  document.addEventListener('click', (e) => {
    // 学习模式关闭时，插件不生效
    if (settings.studyMode === false) return;
    // 非单击模式直接返回，避免调用 getWordAtPoint 清除用户滑动选中的文本
    if (settings.trigger !== 'click') return;
    // 如果点击在弹窗内，不处理
    if (popupEl && popupEl.contains(e.target)) return;

    // 如果用户通过拖动选中了文本，保留用户选择，不做取词处理
    const userSelection = window.getSelection().toString().trim();
    if (userSelection.length > 0) return;

    const result = getWordAtPoint(e);
    if (!result) return;

    // 阻止默认行为，防止取词时触发链接跳转
    e.preventDefault();
    e.stopPropagation();

    // 高亮单词
    highlightRange(result.range);

    debouncedProcessWord(result);
  });

  // 双击取词
  document.addEventListener('dblclick', (e) => {
    // 学习模式关闭时，插件不生效
    if (settings.studyMode === false) return;
    // 非双击模式直接返回
    if (settings.trigger !== 'dblclick') return;
    if (popupEl && popupEl.contains(e.target)) return;

    const result = getWordAtPoint(e);
    if (!result) return;

    // 阻止默认行为
    e.preventDefault();
    e.stopPropagation();

    highlightRange(result.range);

    debouncedProcessWord(result);
  });

  // ESC 关闭
  document.addEventListener('keydown', (e) => {
    if (e.key === 'Escape') hidePopup();
  });

  // 点击外部关闭
  document.addEventListener('click', (e) => {
    if (ignoreNextOutsideClick) return;
    if (isPinned) return;
    if (popupEl && !popupEl.contains(e.target)) {
      scheduleHide();
    }
  });
})();
