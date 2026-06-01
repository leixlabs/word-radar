chrome.runtime.onInstalled.addListener(() => {
  console.log('Word Radar installed');
});

// 监听 content script 的消息
chrome.runtime.onMessage.addListener((request, sender, sendResponse) => {
  if (request.type === 'LOOKUP') {
    // 代理查词请求（处理 CSP 严格网站）
    chrome.storage.sync.get(['server'], async (items) => {
      const server = items.server || 'http://localhost:8787';
      try {
        const params = new URLSearchParams({
          q: request.word,
          context: request.context || '',
        });
        const resp = await fetch(`${server}/api/lookup?${params}`);
        const data = await resp.json();
        sendResponse({ success: resp.ok, data });
      } catch (e) {
        sendResponse({ success: false, error: e.message });
      }
    });
    return true; // async
  }
});
