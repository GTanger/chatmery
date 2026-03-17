(function () {
	// 與 head 內 inline script 同步更新可視高度（鍵盤、旋轉時）
	(function () {
		function setVH() {
			var vv = window.visualViewport;
			var h = (vv && vv.height) ? vv.height : window.innerHeight;
			var t = (vv && vv.offsetTop !== undefined) ? vv.offsetTop : 0;
			document.documentElement.style.setProperty('--app-height', h + 'px');
			document.documentElement.style.setProperty('--vv-top', t + 'px');
		}
		setVH();
		window.addEventListener('resize', setVH);
		if (window.visualViewport) {
			window.visualViewport.addEventListener('resize', setVH);
			window.visualViewport.addEventListener('scroll', setVH);
		}
	})();

	const messagesEl = document.getElementById('messages');
	const form = document.getElementById('chat-form');
	const input = document.getElementById('chat-input');
	const sendBtn = document.getElementById('chat-send');
	const fileInput = document.getElementById('chat-file');
	const fileNameEl = document.getElementById('chat-file-name');
	const webBtn = document.getElementById('chat-web-btn');

	var serverWasDown = false;
	var webSearchOn = false;
	var currentModel = '';

	fetch('api/model').then(function (res) {
		if (!res.ok) return;
		return res.json();
	}).then(function (data) {
		if (data && data.model) currentModel = data.model;
	}).catch(function () {});

	if (webBtn) {
		webBtn.addEventListener('click', function () {
			webSearchOn = !webSearchOn;
			webBtn.setAttribute('aria-pressed', webSearchOn ? 'true' : 'false');
			webBtn.classList.toggle('is-on', webSearchOn);
		});
	}

	function showRestartNotification() {
		if (typeof Notification !== 'undefined' && Notification.permission === 'granted') {
			try {
				new Notification('Chatmery', { body: '系統已重新啟動', tag: 'chatmery-restart' });
			} catch (_) {}
		}
		var toast = document.createElement('div');
		toast.className = 'chat-toast';
		toast.setAttribute('role', 'status');
		toast.textContent = '系統已重新啟動';
		document.body.appendChild(toast);
		setTimeout(function () { toast.classList.add('chat-toast-show'); }, 10);
		setTimeout(function () {
			toast.classList.remove('chat-toast-show');
			setTimeout(function () { toast.remove(); }, 300);
		}, 3000);
	}

	if (fileInput) {
		fileInput.addEventListener('change', function () {
			if (fileNameEl) {
				fileNameEl.textContent = fileInput.files && fileInput.files[0] ? fileInput.files[0].name : '';
			}
		});
	}

	// 僅用於 bot：將 Markdown 轉成安全 HTML（**粗體**、*斜體*、`程式碼`、換行）
	function renderMarkdown(text) {
		if (text == null || text === '') return '';
		var div = document.createElement('div');
		div.textContent = text;
		var s = div.innerHTML;
		s = s.replace(/\*\*(.+?)\*\*/g, '<strong>$1</strong>');
		s = s.replace(/\*(.+?)\*/g, '<em>$1</em>');
		s = s.replace(/__(.+?)__/g, '<strong>$1</strong>');
		s = s.replace(/_(.+?)_/g, '<em>$1</em>');
		s = s.replace(/`([^`\n]+)`/g, '<code>$1</code>');
		s = s.replace(/\n/g, '<br>');
		return s;
	}

	function appendMessage(role, text, isStreaming) {
		const wrap = document.createElement('div');
		wrap.className = 'chat-msg ' + role;
		const meta = document.createElement('div');
		meta.className = 'msg-meta';
		meta.textContent = role === 'user' ? '你' : (currentModel || '阿卅');
		wrap.appendChild(meta);
		const body = document.createElement('div');
		body.className = 'msg-body' + (role === 'bot' ? ' msg-body-md' : '');
		if (role === 'bot') {
			body.innerHTML = renderMarkdown(text);
		} else {
			body.textContent = text;
		}
		if (isStreaming) body.setAttribute('aria-busy', 'true');
		wrap.appendChild(body);
		messagesEl.appendChild(wrap);
		messagesEl.scrollTop = messagesEl.scrollHeight;
		return body;
	}

	function updateStreamBody(el, text, isMarkdown) {
		if (!el) return;
		if (isMarkdown) {
			el.innerHTML = renderMarkdown(text);
		} else {
			el.textContent = text;
		}
		el.removeAttribute('aria-busy');
		messagesEl.scrollTop = messagesEl.scrollHeight;
	}

	input.addEventListener('keydown', function (e) {
		if (e.key !== 'Enter') return;
		if (e.ctrlKey || e.metaKey) {
			e.preventDefault();
			form.requestSubmit();
		}
	});

	form.addEventListener('submit', function (e) {
		e.preventDefault();
		const text = (input.value || '').trim();
		const fileToSend = fileInput && fileInput.files && fileInput.files[0];
		const hasFile = !!fileToSend;
		if (!text && !hasFile) return;

		var displayText = text || (hasFile ? '（附檔：' + fileToSend.name + '）' : '');
		input.value = '';
		if (fileInput) fileInput.value = '';
		if (fileNameEl) fileNameEl.textContent = '';
		sendBtn.disabled = true;
		appendMessage('user', displayText, false);

		const streamBody = appendMessage('bot', '\u2026', true);

		var opts = { method: 'POST' };
		if (hasFile) {
			var fd = new FormData();
			fd.append('text', text);
			fd.append('file', fileToSend);
			fd.append('web_search', webSearchOn ? 'true' : 'false');
			opts.body = fd;
		} else {
			opts.headers = { 'Content-Type': 'application/json' };
			opts.body = JSON.stringify({ text: text, web_search: webSearchOn });
		}
		if (typeof Notification !== 'undefined' && Notification.permission === 'default') {
			Notification.requestPermission();
		}
		fetch('api/chat', opts).then(function (res) {
			if (!res.ok) {
				serverWasDown = true;
				return res.text().then(function (t) {
					updateStreamBody(streamBody, '錯誤：' + (t || res.status), false);
				});
			}
			if (!res.body) {
				updateStreamBody(streamBody, '無法取得串流', false);
				return;
			}
			const reader = res.body.getReader();
			const decoder = new TextDecoder();
			let full = '';
			function read() {
				reader.read().then(function (r) {
					if (r.done) {
						updateStreamBody(streamBody, full || '(無回覆)', true);
						if (serverWasDown) {
							showRestartNotification();
							serverWasDown = false;
						}
						return;
					}
					const chunk = decoder.decode(r.value, { stream: true });
					const lines = chunk.split('\n');
					for (let i = 0; i < lines.length; i++) {
						const line = lines[i];
						if (line.startsWith('data: ')) {
							const data = line.slice(6).trim();
							if (data === '[DONE]') continue;
							try {
								const j = JSON.parse(data);
								if (j.text != null) {
									full += j.text;
									updateStreamBody(streamBody, full + '\u2026', true);
								}
							} catch (_) {}
						}
					}
					read();
				}).catch(function (err) {
					serverWasDown = true;
					updateStreamBody(streamBody, full + '\n\n錯誤：' + err.message, true);
				});
			}
			read();
		}).catch(function (err) {
			serverWasDown = true;
			updateStreamBody(streamBody, '錯誤：' + err.message, false);
		}).finally(function () {
			sendBtn.disabled = false;
		});
	});
})();
