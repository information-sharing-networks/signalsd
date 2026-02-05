
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