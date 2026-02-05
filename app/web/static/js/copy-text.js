// copy to clipboard functionality

function copyText(text, btn) {
	navigator.clipboard.writeText(text).then(function() {
		const originalText = btn.textContent;
		btn.textContent = 'Copied!';
		btn.classList.add('copied');
		setTimeout(function() {
			btn.textContent = originalText;
			btn.classList.remove('copied');
		}, 1500);
	}).catch(function(err) {
		console.error('Failed to copy text: ', err);
		btn.textContent = 'Failed';
		btn.classList.add('error');
		setTimeout(function() {
			btn.textContent = originalText;
			btn.classList.remove('error');
		}, 1500);
	});
}

// Function to attach event listeners to copy buttons
function attachCopyButtonListeners() {
	document.querySelectorAll('.btn-copy').forEach(function(btn) {
		// Only attach if not already attached (check for data attribute)
		if (!btn.dataset.listenerAttached) {
			btn.addEventListener('click', function() {
				const text = this.dataset.text;
				copyText(text, this);
			});
			btn.dataset.listenerAttached = 'true';
		}
	});
}

// Attach on initial page load
document.addEventListener('DOMContentLoaded', attachCopyButtonListeners);

// Attach when HTMX loads new content
document.addEventListener('htmx:afterSwap', attachCopyButtonListeners);

// Auto-redirect to login after 5 seconds (for home page)
document.addEventListener('DOMContentLoaded', function() {
	const homeRedirectElement = document.querySelector('[data-auto-redirect-to-login]');
	if (homeRedirectElement) {
		setTimeout(function() {
			if (typeof htmx !== 'undefined') {
				htmx.ajax('GET', '/login', {target: 'body', swap: 'outerHTML'});
			}
		}, 5000);
	}
});

